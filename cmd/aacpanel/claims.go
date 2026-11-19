package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/notify"
	personrequest "aacpanel/internal/request"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

type claimMail struct{ ID, Conversation, MessageID string }

func claimParams(params map[string]any) bool {
	for _, key := range []string{"expects", "due", "subject", "re", "outcome"} {
		if _, ok := params[key]; ok {
			return true
		}
	}
	return false
}

func readClaimInput(params map[string]any) (act.ClaimInput, error) {
	fields := map[string]any{}
	for _, key := range []string{"expects", "due", "subject", "re", "outcome"} {
		if value, ok := params[key]; ok {
			fields[key] = value
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return act.ClaimInput{}, err
	}
	var input act.ClaimInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, err
	}
	if input.Expects == "" && input.Re == "" {
		return input, errors.New("a claim letter needs expects or re")
	}
	return input, input.Validate()
}

type claimSession struct {
	host.LiveSession
	ConfigDir string `json:"configDir"`
	Profile   string `json:"profile"`
}

func (s claimSession) notification() notify.Session {
	return notify.Session{ID: s.SessionID, Name: s.Name, CWD: s.CWD, ConfigDir: s.ConfigDir, Profile: s.Profile}
}

func (s *Server) claimSessions(ctx context.Context) ([]claimSession, error) {
	if s.host == nil {
		return nil, errors.New("claims need the host snapshot")
	}
	raw, err := s.host.JSON()
	if err != nil {
		return nil, err
	}
	raw, err = s.sessionsPaused(ctx, raw)
	if err != nil {
		return nil, err
	}
	var snapshot struct {
		Sessions []claimSession `json:"sessions"`
	}
	err = json.Unmarshal(raw, &snapshot)
	return snapshot.Sessions, err
}

func claimRecipient(rows []claimSession, name string) (claimSession, error) {
	var found claimSession
	for _, row := range rows {
		if row.Name != name {
			continue
		}
		if found.SessionID != "" {
			return claimSession{}, fmt.Errorf("session %s is ambiguous", name)
		}
		found = row
	}
	if found.SessionID == "" {
		return found, fmt.Errorf("session %s is not live or sleeping", name)
	}
	if found.Outside && found.Agent == schema.AgentCodex {
		return found, fmt.Errorf("session %s is outside the panel", name)
	}
	return found, nil
}

func (s *Server) sendClaim(w http.ResponseWriter, r *http.Request, target string, params map[string]any, term bool) {
	input, err := readClaimInput(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.db == nil || s.exec == nil || s.host == nil {
		http.Error(w, "claims need the database, executor and host snapshot", http.StatusServiceUnavailable)
		return
	}
	from, _ := params["from"].(string)
	text, _ := params["text"].(string)
	form := action.Request{ID: "claim", Kind: action.SessionLetter, Target: target, From: from, Text: text}
	if form.Target == "" && input.Re != "" {
		form.Target = "claim"
	}
	if sender, ok := params["fromCodex"].(map[string]any); ok {
		home, _ := sender["home"].(string)
		dir, _ := sender["dir"].(string)
		form.FromCodex = &action.CodexSender{Home: home, Dir: dir}
	}
	if err := form.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sender, ok := s.host.LiveSessionOf(from)
	if !ok || sender.Name == "" || (sender.Outside && sender.Agent == schema.AgentCodex) {
		http.Error(w, "the claim has no live sender", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout+5*time.Second)
	defer cancel()
	r = r.Clone(ctx)
	var row act.Record
	if input.Re == "" {
		sessions, err := s.claimSessions(ctx)
		if err != nil {
			actError(w, err)
			return
		}
		peer, err := claimRecipient(sessions, target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if peer.SessionID == from {
			http.Error(w, "a session sends no claim to itself", http.StatusBadRequest)
			return
		}
		if input.Due == nil {
			input.Due, err = s.claimDue(ctx, peer.notification())
			if err != nil {
				actError(w, err)
				return
			}
		}
		row, err = s.db.CreateClaim(ctx, from, sender.Name, peer.Name, peer.SessionID, input.Title(text), input)
		if err != nil {
			actError(w, err)
			return
		}
		delivery := s.deliverClaimLetter(r, target, params, row.ID, row.ClaimLetter(text, time.Now()), term)
		if delivery.status != http.StatusOK {
			if err := s.db.DeleteClaim(ctx, row.ID); err != nil {
				log.Printf("claim %s: rejected delivery cleanup: %v", row.ID, err)
			}
			copyClaimReply(w, delivery)
			return
		}
		claimReply(w, delivery, row)
		return
	}
	lease, err := s.db.LockClaim(ctx, input.Re)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	defer lease.Close(ctx)
	target, err = lease.Record.ClaimTarget(from, sender.Name, target, input.Result())
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	delivery := s.deliverClaimLetter(r, target, params, lease.Record.ID, lease.Record.Reply(input.Result(), text), term)
	if delivery.status != http.StatusOK {
		copyClaimReply(w, delivery)
		return
	}
	if err := lease.FinishClaim(ctx, input.Result(), sender.Name); err != nil {
		http.Error(w, "the claim reply was delivered but its mark was not saved; do not resend it blindly", http.StatusServiceUnavailable)
		return
	}
	claimReply(w, delivery, lease.Record)
}

func (s *Server) deliverClaimLetter(r *http.Request, target string, params map[string]any, id, text string, term bool) *actionReply {
	clean := map[string]any{"text": text}
	for _, key := range []string{"from", "fromCodex"} {
		if value, ok := params[key]; ok {
			clean[key] = value
		}
	}
	raw, _ := json.Marshal(map[string]any{"kind": action.SessionLetter, "target": target, "params": clean})
	inner := r.Clone(context.WithValue(r.Context(), claimMail{}, claimMail{ID: id}))
	inner.Body = io.NopCloser(bytes.NewReader(raw))
	reply := &actionReply{header: make(http.Header)}
	s.runAction(reply, inner, term)
	return reply
}

func copyClaimReply(w http.ResponseWriter, reply *actionReply) {
	for key, values := range reply.header {
		w.Header()[key] = values
	}
	w.WriteHeader(reply.status)
	_, _ = w.Write(reply.Bytes())
}

func claimReply(w http.ResponseWriter, reply *actionReply, row act.Record) {
	var result map[string]any
	if err := json.Unmarshal(reply.Bytes(), &result); err != nil {
		historyError(w, err)
		return
	}
	result["claim"] = row
	writeJSON(w, result)
}

func (s *Server) claimDue(ctx context.Context, peer notify.Session) (*int, error) {
	values, err := s.sessionPolicy(ctx, peer)
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		if value.Key != "claimDue" || value.Value == nil {
			continue
		}
		var n int
		switch v := value.Value.(type) {
		case int:
			n = v
		case float64:
			n = int(v)
		}
		if n >= 1 && n <= 1440 {
			return &n, nil
		}
	}
	return nil, nil
}

func (s *Server) withdrawClaim(w http.ResponseWriter, r *http.Request, target string, params map[string]any) {
	id, _ := params["act"].(string)
	if !personrequest.ValidID(id) || target == "" || len(params) != 1 {
		http.Error(w, "a withdrawal needs its sender session and act id", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "claims need the database", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout+5*time.Second)
	defer cancel()
	lease, err := s.db.LockClaim(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	defer lease.Close(ctx)
	row := lease.Record
	if row.Session != target || !row.Open() {
		http.Error(w, "the claim is closed or belongs to another sender", http.StatusConflict)
		return
	}
	r = r.Clone(ctx)
	journal, err := s.logActionAttempt(r, string(action.ActWithdraw), target, map[string]any{"act": id})
	if err != nil {
		actError(w, err)
		return
	}
	err = lease.FinishClaim(ctx, "withdraw", "person")
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: "the claim is withdrawn"}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	}
	if saved := s.db.LogResult(ctx, journal, outcome); saved != nil {
		log.Printf("claim %s: withdrawal journal: %v", id, saved)
	}
	if err != nil {
		actError(w, err)
		return
	}
	s.claimPanelLetter(ctx, row, row.Peer, fmt.Sprintf("The person withdrew claim %s from %s.", id, row.Session), "withdraw-peer", 0)
	s.claimPanelLetter(ctx, row, row.Session, fmt.Sprintf("The person withdrew your claim %s to %s.", id, row.Peer), "withdraw-sender", 0)
	writeJSON(w, map[string]any{"ok": true, "logged": true, "detail": "the claim is withdrawn"})
}

func (s *Server) claimPanelLetter(ctx context.Context, row act.Record, target, text, side string, rung int) {
	sessions, err := s.claimSessions(ctx)
	if err != nil {
		log.Printf("claim %s: rung %d: %v", row.ID, rung, err)
		return
	}
	peer, err := claimRecipient(sessions, target)
	if err != nil {
		log.Printf("claim %s: rung %d: %v", row.ID, rung, err)
		return
	}
	mail := claimMail{ID: row.ID, Conversation: peer.SessionID, MessageID: row.ClaimMessageID(rung, side)}
	ctx = context.WithValue(ctx, claimMail{}, mail)
	ctx = context.WithValue(ctx, automaticActionKey{}, "panel claims")
	if err := s.automaticAction(ctx, string(action.SessionLetter), target, map[string]any{"text": text}); err != nil {
		log.Printf("claim %s: rung %d: %v", row.ID, rung, err)
	}
}

func (s *Server) runClaims(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if err := s.pollClaims(ctx, now); err != nil {
				log.Printf("claims: %v", err)
			}
		}
	}
}

func (s *Server) pollClaims(ctx context.Context, now time.Time) error {
	rows, err := s.db.DueClaims(ctx, now)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.pollClaim(ctx, row.ID, now); err != nil {
			log.Printf("claim %s: %v", row.ID, err)
		}
	}
	return nil
}

func (s *Server) claimPeer(ctx context.Context, row act.Record) (claimSession, error) {
	sessions, err := s.claimSessions(ctx)
	if err != nil {
		return claimSession{}, err
	}
	if peer, err := claimRecipient(sessions, row.Peer); err == nil {
		return peer, nil
	}
	hostID, err := s.db.HostID(ctx, s.hostName)
	if err != nil {
		return claimSession{}, err
	}
	saved, err := s.db.LivingConversation(ctx, hostID, row.PeerConversation)
	if err != nil {
		return claimSession{}, err
	}
	peer := claimSession{LiveSession: host.LiveSession{Name: row.Peer, SessionID: row.PeerConversation}}
	if saved != nil {
		peer.CWD, peer.ConfigDir, peer.Profile = saved.CWD, saved.ConfigDir, saved.Contour
	}
	return peer, nil
}

func (s *Server) pollClaim(ctx context.Context, id string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	for {
		lease, err := s.db.LockClaim(ctx, id)
		if err != nil {
			return err
		}
		row := lease.Record
		if !row.Open() || row.Due == nil || row.Rung == 3 || now.Before(*row.Due) {
			lease.Close(ctx)
			return nil
		}
		peer, err := s.claimPeer(ctx, row)
		if err != nil {
			lease.Close(ctx)
			return err
		}
		on, wait := s.parentQuestionPolicy(ctx, peer.notification())
		next := row.Rung + 1
		at := row.Due.Add(time.Duration(row.Rung) * wait)
		if now.Before(at) {
			lease.Close(ctx)
			return nil
		}
		var opening *action.Opening
		if next == 2 && on && s.exec != nil {
			opening, err = s.exec.Opening(ctx, peer.Name, peer.SessionID)
			if err != nil {
				log.Printf("claim %s: rung 2: opening: %v", id, err)
			}
			if opening != nil && !opening.Letters {
				opening = nil
			}
		}
		if next == 2 && opening == nil {
			next = 3
		}
		if err := lease.AdvanceClaim(ctx, next, now); err != nil {
			lease.Close(ctx)
			return err
		}
		lease.Close(ctx)
		// Closure wins over any delivery still waiting after the persisted advance.
		delivery, err := s.db.LockClaim(ctx, id)
		if err != nil {
			return err
		}
		if !delivery.Record.Open() {
			delivery.Close(ctx)
			return nil
		}
		if row.Rung == 0 {
			s.claimPanelLetter(ctx, row, row.Peer, fmt.Sprintf("The panel: claim %s from %s is due — expects %s. Answer with send_to_session re=%s.", id, row.Session, row.Expectation(), id), "peer", 1)
		} else if row.Rung == 1 {
			parent, told := "", "now"
			overdue := act.ClaimElapsed(now.Sub(*row.Due))
			if opening != nil {
				parent = "The parent " + opening.Name + " was told. "
				if remaining := row.Due.Add(2 * wait).Sub(now); remaining > 0 {
					told = fmt.Sprintf("in %d min", act.ClaimMinutes(remaining))
				}
				s.claimPanelLetter(ctx, row, opening.Name, fmt.Sprintf("The panel: your session %s has not answered claim %s from %s (%s), due %d min ago. Nudge it, or answer %s yourself.", row.Peer, id, row.Session, row.Title, overdue, row.Session), "parent", 2)
			}
			s.claimPanelLetter(ctx, row, row.Session, fmt.Sprintf("The panel: claim %s to %s is overdue for %d min; no answer yet. %sThe person is told %s.", id, row.Peer, overdue, parent, told), "sender", 2)
		}
		if next == 3 && s.push != nil {
			s.push.Send(notify.Message{Kind: notify.KindClaim, Tag: "claim:" + id, Severity: notify.Warning,
				Title: row.Peer + " owes a claim", Body: fmt.Sprintf("%s has not answered %s's claim for %d min: %s", row.Peer, row.Session, act.ClaimElapsed(now.Sub(*row.Due)), row.Title), Session: row.Peer})
		}
		delivery.Close(ctx)
	}
}
