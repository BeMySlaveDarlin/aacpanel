package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/schema"
	"aacpanel/internal/session"
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
	s.prepareSession(w, r, action.SessionRestart, target, params)
}

// Close falls through to its ordinary executor path for conversations that
// cannot answer readiness, and for an already saved, quiet conversation.
func (s *Server) prepareSession(w http.ResponseWriter, r *http.Request, kind action.Kind, target string, params map[string]any) bool {
	if s.db == nil || s.host == nil || s.exec == nil {
		http.Error(w, "the preparation cycle is unavailable", http.StatusServiceUnavailable)
		return true
	}
	plan, err := s.restartPlan(r.Context(), target, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	live, ok := s.host.LiveSession(plan.Name)
	eligible := ok && !live.Outside && live.Agent != schema.AgentCodex && live.SessionID != "" && plan.Project != nil
	if eligible {
		values, valid := launchOf(plan.Project.Launch)
		eligible = valid && values["panelTools"] != false
	}
	if !eligible {
		if kind == action.SessionClose {
			return false
		}
		http.Error(w, "preparation needs a live Claude session in a project of the map with panel tools", http.StatusConflict)
		return true
	}
	finish := store.PrepareRestart
	if kind == action.SessionClose {
		finish = store.PrepareClose
	}
	if kind == action.SessionFinalize {
		finish = store.PrepareFinalize
	}
	if kind == action.SessionClose {
		raw, err := s.host.JSON()
		var snap restartCensus
		if err == nil {
			err = json.Unmarshal(raw, &snap)
		}
		var marks map[string]time.Time
		if err == nil {
			marks, err = s.finalizations(r.Context(), snap)
		}
		if err != nil {
			historyError(w, err)
			return true
		}
		for _, row := range snap.Sessions {
			at, saved := marks[live.SessionID]
			busy, _ := session.BackgroundWork(row.Work)
			if row.SessionID == live.SessionID && saved && row.idle() && !busy && time.Unix(snap.At, 0).After(at) && time.Since(time.Unix(snap.At, 0)) <= restartSnapshotAge {
				params["conversation"] = live.SessionID
				return false
			}
		}
	}
	id, err := s.logActionAttempt(r, string(kind), plan.Name, map[string]any{"prepare": true, "conversation": live.SessionID})
	if err != nil {
		historyError(w, err)
		return true
	}
	err = s.db.BeginPreparation(r.Context(), live.SessionID, plan.Name, finish, time.Now())
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: "Preparation queued; the panel waits for the turn to finish."}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	}
	if logErr := s.db.LogResult(r.Context(), id, outcome); logErr != nil {
		log.Printf("manual restart journal: %v", logErr)
	}
	if err != nil {
		historyError(w, err)
		return true
	}
	writeJSON(w, map[string]any{"ok": true, "logged": true, "detail": outcome.Detail})
	return true
}

// A preparation cycle can outlast the browser's short action wait.
func (s *Server) sessionsRestarting(ctx context.Context, payload []byte, list []store.Profile) []byte {
	if s.db == nil {
		return payload
	}
	cycles, err := s.db.RestartCycles(ctx)
	if err != nil {
		log.Printf("restart status: %v", err)
		return payload
	}
	pending := map[string]bool{}
	phases := map[string]*restartPhase{}
	for _, c := range cycles {
		if c.Manual && !c.Done && (c.Finish == store.PrepareRestart || c.Finish == "") {
			pending[c.Conversation] = true
		}
		if phase := restartPhaseOf(c); phase != nil {
			phases[c.Conversation] = phase
		}
	}
	var census restartCensus
	if err := json.Unmarshal(payload, &census); err != nil {
		return payload
	}
	marks, err := s.finalizations(ctx, census)
	if err != nil {
		log.Printf("finalization status: %v", err)
		return payload
	}
	var snapshot struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return payload
	}
	for _, row := range snapshot.Sessions {
		id := textField(row, "sessionId")
		if at, ok := marks[id]; ok {
			row["finalizedAt"], _ = json.Marshal(at)
		}
		var live host.LiveSession
		raw, _ := json.Marshal(row)
		if json.Unmarshal(raw, &live) == nil && !live.Outside && live.Agent != schema.AgentCodex && id != "" {
			if found, ok := projectAt(list, live.CWD, s.worktrees(), s.db.ProjectRoots()); ok {
				launch, err := store.EffectiveLaunch(found.profile.Launch, found.project.Launch)
				values, valid := launchOf(launch)
				if err == nil && valid && values["panelTools"] != false && values["agent"] != schema.AgentCodex {
					row["prepareAvailable"] = json.RawMessage("true")
				}
			}
		}
		if pending[id] {
			row["restarting"] = json.RawMessage("true")
		}
		if phase := phases[id]; phase != nil {
			row["restartPhase"], _ = json.Marshal(phase)
		}
	}
	out, err := spliceField(payload, "sessions", snapshot.Sessions)
	if err != nil {
		return payload
	}
	return out
}

func (s *Server) finalizations(ctx context.Context, snap restartCensus) (map[string]time.Time, error) {
	turns := map[string]time.Time{}
	for _, row := range snap.Sessions {
		if at, err := time.Parse(time.RFC3339Nano, row.LastTurnAt); err == nil && row.SessionID != "" {
			turns[row.SessionID] = at
		}
	}
	return s.db.Finalizations(ctx, turns)
}
