package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

type pauseAlarm struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Next string `json:"next"`
}

type pauseSession struct {
	host.LiveSession
	Pause *pauseAlarm `json:"pauseAlarm"`
}

// The map owns scope: the phone picks a project and whether to include its
// group, or one session of the project by its name; worktrees belong to the
// same project as on the sessions screen. A session named that runs in
// another project is refused rather than paused: the screen it was pressed on
// put it in the wrong place. The pause goes one step down from the scope: a
// session the panel opened for a session of it works for that session and
// waits with it, wherever it runs, and is lifted with it; the sessions that
// one opened in turn are not its parent's to stop.
func (s *Server) pauseTargets(ctx context.Context, p action.Pause) ([]pauseSession, string, error) {
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return nil, "", err
	}
	anchor, ok := projectByID(list, p.Project)
	if !ok {
		return nil, "", fmt.Errorf("the project is no longer on the map")
	}
	label := anchor.project.Name
	if p.Group {
		label = anchor.group
	}
	if p.Session != "" {
		label = p.Session
	}
	raw, err := s.host.JSON()
	if err != nil {
		return nil, "", err
	}
	var snapshot struct {
		Sessions []pauseSession `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, "", err
	}
	reached := func(row pauseSession) bool { return !row.Outside && row.Name != "" && row.SessionID != "" }
	trees := s.worktrees()
	scope := make(map[string]bool)
	for _, row := range snapshot.Sessions {
		if !reached(row) || (p.Session != "" && row.Name != p.Session) {
			continue
		}
		found, ok := projectAt(list, row.CWD, trees, s.db.ProjectRoots())
		inProject := ok && found.project.ID == p.Project
		if p.Session != "" && !inProject {
			return nil, "", fmt.Errorf("session %s does not run in project %s", p.Session, anchor.project.Name)
		}
		if ok && (inProject || (p.Group && found.project.GroupID == anchor.project.GroupID)) {
			scope[row.Name] = true
		}
	}
	targets := make([]pauseSession, 0)
	for _, row := range snapshot.Sessions {
		if !reached(row) || !(scope[row.Name] || scope[row.OpenedBy()]) || (p.Resume && row.Pause == nil) {
			continue
		}
		targets = append(targets, row)
	}
	return targets, label, nil
}

func (s *Server) pauseProject(w http.ResponseWriter, r *http.Request, params map[string]any) {
	if s.db == nil || s.exec == nil || s.host == nil {
		http.Error(w, "the project pause needs the map and a connected host", http.StatusServiceUnavailable)
		return
	}
	var p action.Pause
	raw, err := json.Marshal(params)
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	var until time.Time
	if err == nil {
		until, err = p.Validate(time.Now())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	targets, label, err := s.pauseTargets(r.Context(), p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(targets) == 0 {
		http.Error(w, "there are no live sessions in this scope to pause or resume", http.StatusConflict)
		return
	}
	result, err := s.applyPause(r, p, targets, label, params, until)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "logged": true, "detail": result.Detail, "delivered": result.Delivered, "failed": result.Failed})
}

type pauseDelivery struct {
	Detail    string
	Delivered int
	Failed    []map[string]string
}

// Both a tap and reboot recovery use this action, including its journal and
// cancellation-before-message rule. A recovered process has no old alarm to
// cancel when its pause expired while the machine was down.
func (s *Server) applyPause(r *http.Request, p action.Pause, targets []pauseSession, label string, params map[string]any, until time.Time) (pauseDelivery, error) {
	id, err := s.logActionAttempt(r, string(action.ProjectPause), label, params)
	if err != nil {
		return pauseDelivery{}, err
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout)
	defer cancel()
	delivered := 0
	failed := make([]map[string]string, 0)
	calls := 0
	call := func(req action.Request) error {
		calls++
		req.ID, req.Device = fmt.Sprintf("%s-%d", requestID(id), calls), s.deviceName(r)
		if err := req.Validate(); err != nil {
			return err
		}
		resp, err := s.exec.Do(ctx, req)
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}
		return nil
	}
	for _, row := range targets {
		err = nil
		text := action.PauseMessage(until)
		if p.Resume {
			if row.Pause != nil {
				err = call(action.Request{Kind: action.AlarmCancel, Target: row.Name, Work: &action.Work{ID: row.Pause.ID}})
			}
			text = action.PanelMessage("pause over", "The person has lifted the pause. Continue your work.")
		} else {
			err = nil
		}
		if err == nil {
			err = call(action.Request{Kind: action.SessionSend, Target: row.Name, Text: text})
		}
		if err != nil {
			failed = append(failed, map[string]string{"session": row.Name, "error": err.Error()})
			continue
		}
		delivered++
	}
	detail := fmt.Sprintf("%d sessions received the message", delivered)
	var refusals []string
	for _, f := range failed {
		refusals = append(refusals, f["session"]+": "+f["error"])
	}
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: detail, Duration: time.Since(started)}
	if len(failed) > 0 {
		outcome.Result, outcome.Error = store.ActionFailed, strings.Join(refusals, "; ")
	}
	if err := s.db.LogResult(context.WithoutCancel(ctx), id, outcome); err != nil {
		log.Printf("project pause journal: %v", err)
	}
	return pauseDelivery{Detail: detail, Delivered: delivered, Failed: failed}, nil
}
