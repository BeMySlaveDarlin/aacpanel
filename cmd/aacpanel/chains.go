package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/notify"
	personrequest "aacpanel/internal/request"
	"aacpanel/internal/store"
)

func chainStep(params map[string]any) int {
	n, ok := params["step"].(float64)
	if !ok || n < 1 || n > personrequest.StepsMax || n != float64(int(n)) {
		return 0
	}
	return int(n)
}

// The command and directory come from the stored act and its live conversation,
// never from the browser. The executor still checks the real directory.
func (s *Server) runChainCommand(w http.ResponseWriter, r *http.Request, kind action.Kind, target string, params map[string]any, term bool) {
	if !term {
		http.Error(w, "chain commands are not served on this listener", http.StatusNotFound)
		return
	}
	if s.exec == nil {
		http.Error(w, "the executor is unavailable", http.StatusServiceUnavailable)
		return
	}
	reply, err := s.prepareActAnswer(r, target, params)
	if err != nil {
		actError(w, err)
		return
	}
	defer reply.close()
	row := &reply.lease.Record
	step := chainStep(params)
	spec, err := row.Current(step, "you")
	if err != nil || spec.Command == "" {
		http.Error(w, "this is not the current human command step", http.StatusConflict)
		return
	}
	out := row.Outputs[step-1]
	if kind == action.ActRun {
		if out.Running() {
			writeJSON(w, map[string]any{"ok": true, "logged": true})
			return
		}
		if row.Notice != nil {
			http.Error(w, "the preceding summary still waits for delivery", http.StatusConflict)
			return
		}

		now := time.Now().UTC()
		next := &personrequest.Output{State: "pending", Term: newTermID(), Started: &now}
		if out != nil {
			next.Tail, next.Log = out.Tail, out.Log
		}
		row.Outputs[step-1] = next
		id := row.ID
		if err := reply.lease.SaveChain(reply.ctx); err != nil {
			actError(w, err)
			return
		}
		if err := s.startChainStep(reply.ctx, id); err != nil {
			actError(w, err)
			return
		}
	} else {
		if !out.Running() {
			http.Error(w, "the step is not running", http.StatusConflict)
			return
		}
		if out.State == "running" {
			// Capture before killing the pane; stopping must retain what the person saw.
			if err := s.peekChainStep(reply.ctx, row); err != nil {
				actError(w, err)
				return
			}
		}
		if err := s.closeChainTerms(reply.ctx, row); err != nil {
			actError(w, err)
			return
		}
		now := time.Now().UTC()
		out.State, out.Ended, out.Exit = "stopped", &now, nil
		if err := reply.lease.SaveChain(reply.ctx); err != nil {
			actError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true, "logged": true})
}

// Every host operation goes through the append-only journal. Peek's output
// and command text stay in the act and its private log, outside that journal.
func (s *Server) chainExec(ctx context.Context, req action.Request, params map[string]any) (action.Response, error) {
	if s.exec == nil {
		return action.Response{}, errors.New("the executor is unavailable")
	}
	r := automaticRequest(context.WithValue(ctx, automaticActionKey{}, "request chain"))
	id, err := s.logActionAttempt(r, string(req.Kind), req.Target, params)
	if err != nil {
		return action.Response{}, err
	}
	if req.ID == "" {
		req.ID = requestID(id)
	}
	req.Device = "panel"
	resp, err := s.doAction(ctx, req)
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: resp.Detail, Duration: time.Duration(resp.DurationMs) * time.Millisecond}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	} else if !resp.OK {
		outcome.Result, outcome.Error = store.ActionFailed, resp.Error
		err = errors.New(resp.Error)
	}
	if save := s.db.LogResult(ctx, id, outcome); save != nil {
		return resp, errors.Join(err, save)
	}
	return resp, err
}

func (s *Server) startChainStep(ctx context.Context, id string) error {
	lease, err := s.db.LockChain(ctx, id)
	if err != nil {
		return err
	}
	defer lease.Close(ctx)
	row := &lease.Record
	if !row.Chain() || row.State != "open" {
		return nil
	}
	out := row.Outputs[*row.Step-1]
	if out == nil || out.State != "pending" || out.Term == "" {
		return nil
	}
	live, ok := s.host.LiveSessionOf(row.Conversation)
	if !ok || live.Outside {
		return errors.New("the chain's conversation has no live session")
	}
	_, err = s.chainExec(ctx, action.Request{ID: out.Term + "-start", Kind: action.TermStart, Target: out.Term, Place: live.CWD, TermCommand: row.Body.Chain[*row.Step-1].Command}, map[string]any{"act": row.ID, "step": *row.Step, "place": live.CWD})
	if err != nil {
		return err
	}
	out.State = "running"
	return lease.SaveChain(ctx)
}

func (s *Server) peekChainStep(ctx context.Context, row *act.Record) error {
	out := row.Outputs[*row.Step-1]
	path := filepath.Join(s.home, ".local", "state", "aacpanel", "acts", row.ID, strconv.Itoa(*row.Step)+".log")
	resp, err := s.chainExec(ctx, action.Request{Kind: action.TermPeek, Target: out.Term, Log: path}, map[string]any{"act": row.ID, "step": *row.Step})
	if err != nil {
		if resp.Code == action.TermNotFound {
			code := -1
			out.Exit = &code
			out.Note = "The terminal was closed before its exit status was read."
			return nil
		}
		return err
	}
	if resp.TermPeek == nil {
		return errors.New("the executor returned no terminal capture")
	}
	peek := resp.TermPeek
	out.Tail, out.Log = personrequest.Tail(peek.Tail), path
	if peek.Dead {
		code := peek.Status
		out.Exit = &code
	}
	return nil
}

func (s *Server) closeChainTerms(ctx context.Context, row *act.Record) error {
	for _, out := range row.Outputs {
		if out == nil || out.Term == "" {
			continue
		}
		if resp, err := s.chainExec(ctx, action.Request{Kind: action.TermClose, Target: out.Term}, map[string]any{"act": row.ID}); err != nil && resp.Code != action.TermNotFound {
			return err
		}
		out.Term = ""
	}
	return nil
}

func (s *Server) answerChain(w http.ResponseWriter, r *http.Request, reply *actReply, params map[string]any) {
	row := &reply.lease.Record
	step := chainStep(params)
	spec, err := row.Current(step, "you")
	if err != nil || (reply.answer == "done" && spec.Command != "") {
		http.Error(w, "Done applies only to the current human step without a command", http.StatusConflict)
		return
	}
	if out := row.Outputs[step-1]; reply.answer == "cant" && out.Running() {
		http.Error(w, "stop the command before declining its step", http.StatusConflict)
		return
	}
	if err := personrequest.ValidateAnswer(reply.answer, reply.note); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if row.Notice != nil {
		http.Error(w, "the preceding summary still waits for delivery", http.StatusConflict)
		return
	}
	row.Advance(map[string]string{"done": "done", "cant": "cant"}[reply.answer], strings.TrimSpace(reply.note), time.Now().UTC())
	row.AnsweredBy = "person"
	s.chainAdvanced(reply.lease)
	if !row.Open() {
		if err := s.closeChainTerms(reply.ctx, row); err != nil {
			actError(w, err)
			return
		}
	}
	id := row.ID
	if err := reply.lease.SaveChain(reply.ctx); err != nil {
		actError(w, err)
		return
	}
	if err := s.deliverChainNotice(reply.ctx, id); err != nil {
		log.Printf("chain notice: %v", err)
	}
	writeJSON(w, map[string]any{"ok": true, "logged": true})
}

func (s *Server) chainAdvanced(lease *store.ActAnswer) {
	row := &lease.Record
	if row.WakesAgent() {
		lease.QueueNotice()
		return
	}
	if s.push != nil && s.host != nil {
		if live, ok := s.host.LiveSessionOf(row.Conversation); ok {
			s.push.Send(notify.Requested(notify.Session{ID: live.SessionID, Name: live.Name, CWD: live.CWD}, row.ID, row.Title))
		}
	}
}

func (s *Server) deliverChainNotice(ctx context.Context, id string) error {
	lease, err := s.db.LockChain(ctx, id)
	if err != nil {
		return err
	}
	defer lease.Close(ctx)
	row := &lease.Record
	if row.Notice == nil {
		return nil
	}
	live, ok := s.host.LiveSessionOf(row.Conversation)
	if !ok || live.Outside {
		return errors.New("the chain's recipient is not live")
	}
	if err := s.automaticAction(context.WithValue(ctx, automaticActionKey{}, "request chain"), string(action.SessionSend), live.Name, map[string]any{"text": row.Notice.Text, "messageId": row.Notice.ID}); err != nil {
		return err
	}
	row.Notice = nil
	return lease.SaveChain(ctx)
}

func (s *Server) runChains(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := s.pollChains(ctx); err != nil {
			log.Printf("request chains: %v", err)
		}
	}
}

func (s *Server) pollChains(ctx context.Context) error {
	rows, err := s.db.ActiveChains(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.pollChain(ctx, row.ID); err != nil {
			log.Printf("chain %s: %v", row.ID, err)
		}
	}
	return nil
}

func (s *Server) pollChain(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	if err := s.startChainStep(ctx, id); err != nil {
		return err
	}
	lease, err := s.db.LockChain(ctx, id)
	if err != nil {
		return err
	}
	defer lease.Close(ctx)
	row := &lease.Record
	if row.State == "open" {
		out := row.Outputs[*row.Step-1]
		if out != nil && out.State == "running" {
			if err := s.peekChainStep(ctx, row); err != nil {
				return err
			}
			if out.Exit != nil {
				outcome := "done"
				if *out.Exit != 0 {
					outcome = "failed"
				}
				row.Advance(outcome, "", time.Now().UTC())
				s.chainAdvanced(lease)
			}
		}
	}
	if !row.Open() {
		if err := s.closeChainTerms(ctx, row); err != nil {
			return err
		}
	}
	if err := lease.SaveChain(ctx); err != nil {
		return err
	}
	return s.deliverChainNotice(ctx, id)
}

func (s *Server) operateChain(w http.ResponseWriter, r *http.Request, conversation, session string, input act.Input) {
	lease, err := s.db.LockAct(r.Context(), input.ID)
	if err != nil {
		actError(w, err)
		return
	}
	defer lease.Close(r.Context())
	row := &lease.Record
	if row.Kind == "claim" {
		http.Error(w, "withdraw a claim with send_to_session and outcome withdraw", http.StatusConflict)
		return
	}
	if row.Conversation != conversation {
		http.Error(w, "the act belongs to another conversation", http.StatusConflict)
		return
	}
	if !row.Chain() {
		if !input.Withdraw {
			http.Error(w, "this act is not a chain", http.StatusConflict)
			return
		}
		lease.Close(r.Context())
		if err := s.db.WithdrawAct(r.Context(), conversation, session, input.ID); err != nil {
			actError(w, err)
			return
		}
		writeJSON(w, map[string]string{"id": input.ID, "message": "Withdrawn request " + input.ID + ". End the turn if no independent work remains."})
		return
	}
	now := time.Now().UTC()
	closed := *row.Step
	switch {
	case input.Withdraw:
		if err := s.closeChainTerms(r.Context(), row); err != nil {
			actError(w, err)
			return
		}
		lease.WithdrawChain(now, session)
	case input.Reopen != 0:
		if row.State != "failed" || *row.Step != input.Reopen || row.Body.Chain[*row.Step-1].Who != "you" {
			http.Error(w, "only the failed human step can be reopened", http.StatusConflict)
			return
		}
		if err := s.closeChainTerms(r.Context(), row); err != nil {
			actError(w, err)
			return
		}
		row.State, row.Notice = "open", nil
		row.Outputs[*row.Step-1].State = "pending"
		s.chainAdvanced(lease)
	default:
		if _, err := row.Current(input.Step, "agent"); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		row.Advance(input.Outcome, strings.TrimSpace(input.Note), now)
		row.AnsweredBy = session
		s.chainAdvanced(lease)
		if !row.Open() {
			if err := s.closeChainTerms(r.Context(), row); err != nil {
				actError(w, err)
				return
			}
		}
	}
	message := row.NextMessage(closed)
	if input.Withdraw {
		message = "Withdrawn request " + input.ID + ". End the turn if no independent work remains."
	}
	if input.Reopen != 0 {
		message = fmt.Sprintf("Step %d is the person's — end the turn.", input.Reopen)
	}
	if err := lease.SaveChain(r.Context()); err != nil {
		actError(w, err)
		return
	}
	if err := s.deliverChainNotice(r.Context(), input.ID); err != nil {
		log.Printf("chain notice: %v", err)
	}
	writeJSON(w, map[string]string{"id": input.ID, "message": message})
}

func (s *Server) runChainAction(w http.ResponseWriter, r *http.Request, kind action.Kind, target string, params map[string]any, term bool) {
	if !term {
		http.Error(w, "chain commands are not served on this listener", http.StatusNotFound)
		return
	}
	s.chainHumanAction(w, r, kind, target, params, func(out http.ResponseWriter) { s.runChainCommand(out, r, kind, target, params, term) })
}

func (s *Server) chainHumanAction(w http.ResponseWriter, r *http.Request, kind action.Kind, target string, params map[string]any, run func(http.ResponseWriter)) {
	clean := map[string]any{"act": params["act"], "step": params["step"]}
	if answer, ok := params["answer"].(string); ok {
		clean["answer"] = answer
	}
	id, err := s.logActionAttempt(r, string(kind), target, clean)
	if err != nil {
		actError(w, err)
		return
	}
	out := &actionReply{header: make(http.Header)}
	run(out)
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: "the chain action was accepted"}
	if out.status != http.StatusOK {
		outcome.Result, outcome.Error = store.ActionFailed, strings.TrimSpace(out.String())
	}
	if err := s.db.LogResult(context.WithoutCancel(r.Context()), id, outcome); err != nil {
		log.Printf("chain action journal: %v", err)
	}
	for k, v := range out.header {
		w.Header()[k] = v
	}
	w.WriteHeader(out.status)
	w.Write(out.Bytes())
}
