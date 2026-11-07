package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/schema"
	"aacpanel/internal/session"
	"aacpanel/internal/store"
)

type sleepActionKey struct{}
type sleepResumeKey struct{}

func (s *Server) sessionPauses(ctx context.Context) ([]store.SessionPause, error) {
	if s.db == nil {
		return nil, nil
	}
	id, err := s.db.HostID(ctx, s.hostName)
	if err != nil {
		return nil, err
	}
	return s.db.SessionPauses(ctx, id)
}

func pauseConversation(rows []store.SessionPause, conversation string) (store.SessionPause, bool) {
	for _, row := range rows {
		if row.Conversation == conversation {
			return row, true
		}
	}
	return store.SessionPause{}, false
}

func (s *Server) rememberPause(ctx context.Context, row pauseSession, until, commanded time.Time) error {
	profiles, err := s.db.Profiles(ctx)
	if err != nil {
		return err
	}
	saved := store.LivingSession{Conversation: row.SessionID, Session: row.Name, CWD: row.CWD,
		Contour: row.Profile, ConfigDir: row.ConfigDir, Transport: row.Transport, Agent: schema.AgentClaude}
	if found, ok := projectAt(profiles, row.CWD, s.worktrees(), s.db.ProjectRoots()); ok {
		saved.ProjectID, saved.Project = found.project.ID, found.project.Name
		saved.Contour, saved.ConfigDir = found.profile.Name, found.profile.ConfigDir
	}
	saved.Parent, saved.ParentConversation, err = s.pauseParent(ctx, row.LiveSession)
	if err != nil {
		return err
	}
	p := store.SessionPause{LivingSession: saved, Snapshot: row.Snapshot, Sleeping: row.Sleeping, CommandAt: commanded}
	if !until.IsZero() {
		p.Until = &until
	}
	id, err := s.db.HostID(ctx, s.hostName)
	if err != nil {
		return err
	}
	return s.db.PutSessionPause(ctx, id, p)
}

func (s *Server) pauseParent(ctx context.Context, row host.LiveSession) (string, string, error) {
	if row.Parent == nil {
		return "", "", nil
	}
	name := row.Parent.Session
	if live, ok := s.host.LiveSession(name); ok {
		return name, live.SessionID, nil
	}
	pauses, err := s.sessionPauses(ctx)
	if err != nil {
		return "", "", err
	}
	for _, parent := range pauses {
		if parent.Session == name {
			return name, parent.Conversation, nil
		}
	}
	return name, "", nil
}

func panelPauseAlarm(p store.SessionPause) pauseAlarm {
	alarm := pauseAlarm{ID: "panel:" + p.Conversation, Name: action.PauseNoEndAlarm}
	if p.Until != nil {
		alarm.Name, alarm.Next = action.PauseAlarm, p.Until.Format(time.RFC3339)
	}
	return alarm
}

// Sleeping cards are service data. They never enter the collector's live
// census, which would make a reboot revive conversations still on pause.
func (s *Server) sessionsPaused(ctx context.Context, payload []byte) ([]byte, error) {
	pauses, err := s.sessionPauses(ctx)
	if err != nil || len(pauses) == 0 {
		return payload, err
	}
	var snap map[string]json.RawMessage
	if err := json.Unmarshal(payload, &snap); err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(snap["sessions"], &rows); err != nil {
		return nil, err
	}
	for _, p := range pauses {
		var row map[string]any
		for _, live := range rows {
			if live["sessionId"] == p.Conversation {
				row = live
				break
			}
		}
		if row == nil && p.Sleeping {
			if err := json.Unmarshal(p.Snapshot, &row); err != nil {
				return nil, err
			}
			row["status"], row["sleeping"] = "idle", true
			delete(row, "ask")
			delete(row, "permission")
			delete(row, "waitingFor")
			delete(row, "work")
			delete(row, "nextAlarm")
			if p.Parent != "" {
				row["parent"] = host.SessionRef{Session: p.Parent}
			} else {
				delete(row, "parent")
			}
			rows = append(rows, row)
		}
		if row != nil {
			row["pauseAlarm"] = panelPauseAlarm(p)
		}
	}
	snap["sessions"], err = json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	return json.Marshal(snap)
}

func (s *Server) sleepReviver() *reviver {
	if s.sleeper == nil {
		s.sleeper = &reviver{srv: s, config: s.sleepConfig, read: s.livingSnapshot, measure: s.exec.Headroom, sleep: true}
	}
	return s.sleeper
}

func (s *Server) wakePaused(ctx context.Context, conversation string) error {
	pauses, err := s.sessionPauses(ctx)
	if err != nil {
		return err
	}
	p, ok := pauseConversation(pauses, conversation)
	if !ok || !p.Sleeping {
		return nil
	}
	return s.wakePauses(ctx, []store.SessionPause{p}, nil)
}

func (s *Server) wakePauses(ctx context.Context, pauses []store.SessionPause, after func(store.SessionPause) error) error {
	r := s.sleepReviver()
	snap, err := r.read(time.Now())
	if err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, r.config.Alive)
	defer cancel()
	for {
		fresh := true
		for _, p := range pauses {
			if p.Sleeping && !time.Unix(snap.At, 0).After(p.CommandAt) {
				fresh = false
				break
			}
		}
		if fresh {
			break
		}
		if err := r.wait(wait); err != nil {
			return fmt.Errorf("a fresh census after the sleep is not available")
		}
		snap, err = r.read(time.Now())
		if err != nil {
			return err
		}
	}
	r.boot = snap.BootID
	pending := []store.LivingSession{}
	for _, p := range pauses {
		if _, alive := liveConversation(snap, p.Conversation); alive {
			if after != nil {
				if err := after(p); err != nil {
					return err
				}
			}
			continue
		}
		pending = append(pending, p.LivingSession)
	}
	_, _, failed := r.raise(ctx, pending, len(pending), func(_ context.Context, row store.LivingSession, _ livingRow) error {
		if after == nil {
			return nil
		}
		for _, p := range pauses {
			if p.Conversation == row.Conversation {
				return after(p)
			}
		}
		return nil
	})
	if len(failed) > 0 {
		return fmt.Errorf("%s", failed[0])
	}
	return nil
}

func (s *Server) runPauses(ctx context.Context) {
	tick := time.NewTicker(livingEvery)
	defer tick.Stop()
	for {
		if err := s.pauseOnce(ctx, time.Now()); err != nil {
			log.Printf("session sleep: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) pauseOnce(ctx context.Context, now time.Time) error {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	return s.advancePauses(ctx, now)
}

func (s *Server) advancePauses(ctx context.Context, now time.Time) error {
	if s.exec == nil || s.host == nil {
		return nil
	}
	pauses, err := s.sessionPauses(ctx)
	if err != nil || len(pauses) == 0 {
		return err
	}
	snap, err := s.livingSnapshot(now)
	if err != nil {
		return err
	}
	raw, err := s.host.JSON()
	if err != nil {
		return err
	}
	var states struct {
		Sessions []restartSession `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &states); err != nil {
		return err
	}
	hostID, err := s.db.HostID(ctx, s.hostName)
	if err != nil {
		return err
	}
	due := []store.SessionPause{}
	for _, p := range pauses {
		if p.Until != nil && !now.Before(*p.Until) {
			due = append(due, p)
			continue
		}
		for _, row := range states.Sessions {
			if row.SessionID != p.Conversation {
				continue
			}
			busy, _ := session.BackgroundWork(row.Work)
			requested, _ := time.Parse(time.RFC3339Nano, row.LastRequestAt)
			// The pre-message idle census must not close a process before it
			// reads the pause, or before it answers the person's one turn.
			finished := p.Sleeping || row.StatusAt > p.CommandAt.UnixMilli() || requested.After(p.CommandAt)
			if !row.idle() || busy || !finished || !time.Unix(snap.At, 0).After(p.CommandAt) {
				break
			}
			before := p
			p.Parent, p.ParentConversation, err = s.pauseParent(ctx, row.LiveSession)
			if err != nil {
				return err
			}
			opening, err := s.exec.Opening(ctx, p.Session, p.Conversation)
			if err != nil {
				return err
			}
			if opening != nil {
				p.OpenedBy = opening.Conversation
			}
			p.Sleeping, p.CommandAt = true, now
			if err := s.db.PutSessionPause(ctx, hostID, p); err != nil {
				return err
			}
			sleepCtx := context.WithValue(ctx, sleepActionKey{}, true)
			sleepCtx = context.WithValue(sleepCtx, automaticActionKey{}, "session sleep")
			err = s.automaticAction(sleepCtx, string(action.SessionClose), p.Session, map[string]any{"conversation": p.Conversation})
			if err != nil {
				if saveErr := s.db.PutSessionPause(ctx, hostID, before); saveErr != nil {
					return saveErr
				}
				return err
			}
			break
		}
	}
	if len(due) == 0 {
		return nil
	}
	return s.wakePauses(ctx, due, func(p store.SessionPause) error {
		wakeCtx := context.WithValue(ctx, sleepActionKey{}, true)
		wakeCtx = context.WithValue(wakeCtx, automaticActionKey{}, "pause timer")
		if err := s.automaticAction(wakeCtx, string(action.SessionSend), p.Session, map[string]any{"text": action.PanelMessage("pause over", "The person has lifted the pause. Continue your work.")}); err != nil {
			return err
		}
		return s.db.DropSessionPause(ctx, hostID, p.Conversation)
	})
}

// A letter lifts a pause, while a person's message borrows one turn. Holding
// the same lock as the sleeper prevents closing between wake and delivery.
func (s *Server) pausedAction(w http.ResponseWriter, r *http.Request, term bool, kind, target string, params map[string]any) bool {
	if internal, _ := r.Context().Value(sleepActionKey{}).(bool); internal || s.db == nil || s.exec == nil {
		return false
	}
	if kind != string(action.SessionSend) && kind != string(action.SessionLetter) && kind != string(action.SessionClose) {
		return false
	}
	pauses, err := s.sessionPauses(r.Context())
	if err != nil {
		historyError(w, err)
		return true
	}
	var p store.SessionPause
	found := false
	for _, row := range pauses {
		if row.Session == target || (target == "" && params["conversation"] == row.Conversation) {
			p, found = row, true
			break
		}
	}
	if !found {
		return false
	}
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	pauses, err = s.sessionPauses(r.Context())
	if err != nil {
		historyError(w, err)
		return true
	}
	if p, found = pauseConversation(pauses, p.Conversation); !found {
		return false
	}
	// A tool caller may stop waiting before admission and the fresh census
	// finish. Once accepted, delivery follows the ordinary action's lifetime.
	c := s.sleepReviver().config
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout+c.Wait+2*c.Alive)
	defer cancel()
	r = r.Clone(ctx)
	if conversation, _ := params["conversation"].(string); conversation != "" && conversation != p.Conversation {
		http.Error(w, "the paused conversation does not match", http.StatusConflict)
		return true
	}
	if kind == string(action.SessionClose) {
		if params == nil {
			params = map[string]any{}
		}
		params["conversation"] = p.Conversation
		if caller, present := params["caller"]; present && p.Sleeping {
			name, ok := caller.(string)
			if !ok || name == "" || name != p.OpenedBy {
				http.Error(w, "the caller did not open this session", http.StatusForbidden)
				return true
			}
			if _, live := s.host.LiveSessionOf(name); !live {
				http.Error(w, "the calling conversation is not live", http.StatusForbidden)
				return true
			}
		}
	} else if p.Sleeping {
		if text, _ := params["text"].(string); text == "" {
			http.Error(w, "a message needs text", http.StatusBadRequest)
			return true
		}
		if err := s.wakePaused(r.Context(), p.Conversation); err != nil {
			historyError(w, err)
			return true
		}
	}
	hostID, err := s.db.HostID(r.Context(), s.hostName)
	if err != nil {
		historyError(w, err)
		return true
	}
	at := time.Now()
	if kind == string(action.SessionClose) {
		if _, alive := s.host.LiveSessionOf(p.Conversation); !alive && p.Sleeping {
			id, err := s.logActionAttempt(r, kind, p.Session, map[string]any{"conversation": p.Conversation})
			if err != nil {
				historyError(w, err)
				return true
			}
			err = s.db.DropSessionPause(r.Context(), hostID, p.Conversation)
			outcome := store.ActionOutcome{Result: store.ActionOK, Detail: "the sleeping session was closed"}
			if err != nil {
				outcome.Result, outcome.Error = store.ActionFailed, err.Error()
			}
			if logErr := s.db.LogResult(r.Context(), id, outcome); logErr != nil {
				log.Printf("sleep close journal: %v", logErr)
			}
			if err != nil {
				historyError(w, err)
			} else {
				writeJSON(w, map[string]any{"ok": true, "logged": true, "detail": outcome.Detail})
			}
			return true
		}
	}
	raw, err := json.Marshal(map[string]any{"kind": kind, "target": p.Session, "params": params})
	if err != nil {
		historyError(w, err)
		return true
	}
	inner := r.Clone(context.WithValue(r.Context(), sleepActionKey{}, true))
	inner.Body = io.NopCloser(bytes.NewReader(raw))
	reply := &actionReply{header: make(http.Header)}
	s.runAction(reply, inner, term)
	if reply.status == http.StatusOK {
		if kind == string(action.SessionSend) {
			p.Sleeping, p.CommandAt = false, at
			err = s.db.PutSessionPause(context.WithoutCancel(r.Context()), hostID, p)
		} else {
			err = s.db.DropSessionPause(context.WithoutCancel(r.Context()), hostID, p.Conversation)
		}
		if err != nil {
			historyError(w, err)
			return true
		}
	}
	for key, values := range reply.header {
		w.Header()[key] = values
	}
	w.WriteHeader(reply.status)
	w.Write(reply.Bytes())
	return true
}
