package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

func (s *Server) prepareRestart(w http.ResponseWriter, r *http.Request, target string, params map[string]any) {
	if s.db == nil || s.host == nil || s.exec == nil {
		http.Error(w, "the restart cycle is unavailable", http.StatusServiceUnavailable)
		return
	}
	if params["prepare"] != true || params["clean"] == true || params["resume"] == true || params["intent"] != nil {
		http.Error(w, "a prepared restart cannot also be clean, continue a conversation or supply its first message", http.StatusBadRequest)
		return
	}
	plan, err := s.restartPlan(r.Context(), target, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	live, ok := s.host.LiveSession(plan.Name)
	if !ok || live.Outside || live.Agent == schema.AgentCodex || live.SessionID == "" || plan.Project == nil {
		http.Error(w, "preparing a restart needs a live Claude session in a project of the map", http.StatusConflict)
		return
	}
	id, err := s.logActionAttempt(r, string(action.SessionRestart), plan.Name, map[string]any{"prepare": true, "conversation": live.SessionID})
	if err != nil {
		historyError(w, err)
		return
	}
	err = s.db.BeginManualRestart(r.Context(), live.SessionID, plan.Name, time.Now())
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: "Restart preparation queued; the panel waits for the turn to finish."}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	}
	if logErr := s.db.LogResult(r.Context(), id, outcome); logErr != nil {
		log.Printf("manual restart journal: %v", logErr)
	}
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "logged": true, "detail": outcome.Detail})
}

// A preparation cycle can outlast the browser's short action wait.
func (s *Server) sessionsRestarting(ctx context.Context, payload []byte) []byte {
	if s.db == nil {
		return payload
	}
	cycles, err := s.db.RestartCycles(ctx)
	if err != nil {
		log.Printf("restart status: %v", err)
		return payload
	}
	pending := map[string]bool{}
	for _, c := range cycles {
		if c.Manual && !c.Done {
			pending[c.Conversation] = true
		}
	}
	if len(pending) == 0 {
		return payload
	}
	var snapshot struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return payload
	}
	for _, row := range snapshot.Sessions {
		if pending[textField(row, "sessionId")] {
			row["restarting"] = json.RawMessage("true")
		}
	}
	out, err := spliceField(payload, "sessions", snapshot.Sessions)
	if err != nil {
		return payload
	}
	return out
}
