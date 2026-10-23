package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/guards"
	"aacpanel/internal/host"
	"aacpanel/internal/schema"
	"aacpanel/internal/session"
	"aacpanel/internal/store"
)

const (
	restartEvery       = 5 * time.Second
	restartGrace       = 5 * time.Minute
	restartKeep        = 7 * 24 * time.Hour
	restartSnapshotAge = 30 * time.Second
)

const restartCommand = "This session has reached its project's context cap. The panel will restart it with an empty context. " +
	"Start no new work, background included. Update the task registry, save a handoff using the project's finalize skill if it has one, " +
	"and commit completed work. Cleanly stop your background commands, agents, workflows and panel observers. " +
	"Then call session_ready (mcp__aacpanel__session_ready), optionally with intent for the fresh session's first message, " +
	"and end the turn. Do not call session_restart. Do this silently: do not tell the person about the context, the cap or the restart."

const restartReminder = "The panel is waiting for session_ready. Finish saving the task registry and handoff, commit completed work, " +
	"and stop your background commands, agents, workflows and panel observers. Call session_ready, optionally with intent, and end the turn. " +
	"The panel will restart this session even if preparation is unfinished five minutes after this reminder. " +
	"Do not call session_restart or tell the person about the context, the cap or the restart."

func restartWorkReminder(work string) string {
	return "Readiness was accepted, but this session still has " + work + " at work. Cleanly stop that work, then end the turn and wait. " +
		"The panel will restart this session even if that work is unfinished five minutes after this reminder. " +
		"Do not call session_restart or tell the person about the context, the cap or the restart."
}

func restartMessage(text string) string {
	return action.PanelMessage(action.PanelContextRestart, text)
}

type restartSession struct {
	host.LiveSession
	TurnOver      bool            `json:"turnOver"`
	LastRequestAt string          `json:"lastRequestAt"`
	Status        string          `json:"status"`
	StatusAt      int64           `json:"statusUpdatedAt"`
	Waiting       string          `json:"waitingFor"`
	Ask           json.RawMessage `json:"ask"`
	Permission    json.RawMessage `json:"permission"`
	Pct           *float64        `json:"pct"`
	Limit         float64         `json:"limit"`
	LimitKnown    *bool           `json:"limitKnown"`
	Work          json.RawMessage `json:"work"`
}

func (s restartSession) idle() bool {
	return (s.Status == "idle" || (s.Status == "busy" && s.TurnOver)) && s.Waiting == "" &&
		(len(s.Ask) == 0 || string(s.Ask) == "null") &&
		(len(s.Permission) == 0 || string(s.Permission) == "null")
}

type restartStep struct {
	Kind   action.Kind
	Text   string
	Anyway bool
}

// A snapshot from before readiness cannot prove the tool's turn has ended.
// For a missing reply, the status timestamp proves an intervening turn even
// when the collector did not catch its running state.
func nextRestart(c store.RestartCycle, s restartSession, snapshotAt, now time.Time) restartStep {
	if c.Done || (c.AttemptedAt != nil && now.Before(c.AttemptedAt.Add(restartGrace))) {
		return restartStep{}
	}
	if !c.Commanded {
		if s.idle() {
			return restartStep{Kind: action.SessionSend, Text: restartMessage(restartCommand)}
		}
		return restartStep{}
	}
	busy, work := session.BackgroundWork(s.Work)
	readyIdle := c.ReadyAt != nil && snapshotAt.After(*c.ReadyAt) && s.idle()
	if readyIdle && !busy {
		return restartStep{Kind: action.SessionRestart, Text: c.Intent}
	}
	if c.RemindedAt != nil {
		if !now.Before(c.RemindedAt.Add(restartGrace)) {
			return restartStep{Kind: action.SessionRestart, Text: c.Intent, Anyway: true}
		}
		return restartStep{}
	}
	if readyIdle && busy {
		return restartStep{Kind: action.SessionSend, Text: restartMessage(restartWorkReminder(work))}
	}
	requestAt, _ := time.Parse(time.RFC3339Nano, s.LastRequestAt)
	if c.ReadyAt == nil && s.idle() && (s.StatusAt > c.CommandAt.UnixMilli() || requestAt.After(c.CommandAt)) && snapshotAt.After(c.CommandAt) {
		return restartStep{Kind: action.SessionSend, Text: restartMessage(restartReminder)}
	}
	return restartStep{}
}

func (s *Server) runRestarts(ctx context.Context) {
	tick := time.NewTicker(restartEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.restartOnce(ctx, time.Now()); err != nil {
				log.Printf("context restart: %v", err)
			}
		}
	}
}

func (s *Server) restartOnce(ctx context.Context, now time.Time) error {
	if s.host == nil || s.db == nil || s.exec == nil {
		return nil
	}
	raw, err := s.host.JSON()
	if err != nil {
		return err
	}
	var snap struct {
		At       int64            `json:"sessionsAt"`
		Sessions []restartSession `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	at := time.Unix(snap.At, 0)
	if snap.At == 0 || now.Sub(at) > restartSnapshotAge {
		return nil
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return err
	}
	cycles, err := s.db.RestartCycles(ctx)
	if err != nil {
		return err
	}
	live := make([]string, 0, len(snap.Sessions))
	for _, row := range snap.Sessions {
		live = append(live, row.SessionID)
	}
	if err := s.db.PruneRestartCycles(ctx, live, now.Add(-restartKeep)); err != nil {
		return err
	}
	byID := make(map[string]store.RestartCycle, len(cycles))
	for _, c := range cycles {
		byID[c.Conversation] = c
	}
	for _, row := range snap.Sessions {
		if row.Agent == schema.AgentCodex || row.Outside || row.SessionID == "" {
			continue
		}
		found, ok := projectAt(list, row.CWD, s.worktreesFor(ctx, list, row.CWD, s.worktrees()), s.db.ProjectRoots())
		if !ok {
			continue
		}
		launch, err := store.EffectiveLaunch(found.profile.Launch, found.project.Launch)
		if err != nil {
			return err
		}
		values, ok := launchOf(launch)
		if !ok || values["agent"] == schema.AgentCodex {
			continue
		}
		guard := guardOf(found.project.Path, schema.Effective(nil, values, nil))
		if !guard.Restart {
			continue
		}
		c, exists := byID[row.SessionID]
		if !exists {
			if !row.idle() || row.Pct == nil || row.Limit <= 0 || (row.LimitKnown != nil && !*row.LimitKnown) ||
				*row.Pct < float64(guard.Cap)-guards.RestartMargin {
				continue
			}
			c = store.RestartCycle{Conversation: row.SessionID, Session: row.Name, CommandAt: now}
		}
		c.Session = row.Name
		step := nextRestart(c, row, at, now)
		if step.Kind == "" {
			continue
		}
		// Persist before delivery: readiness may arrive immediately, and a
		// service failure must not make a failed restart retry without a delay.
		c.AttemptedAt = &now
		if err := s.db.KeepRestartCycle(ctx, c); err != nil {
			return err
		}
		deliveryBegan := time.Now()
		err = s.restartAction(ctx, c, step)
		if err != nil {
			log.Printf("context restart of %s: %v", c.Session, err)
			return nil
		}
		c.AttemptedAt = nil
		if step.Kind == action.SessionRestart {
			c.Done = true
		} else if !c.Commanded {
			c.Commanded = true
			c.CommandAt = now
		} else {
			deliveredAt := now.Add(time.Since(deliveryBegan))
			c.RemindedAt = &deliveredAt
		}
		if err := s.db.KeepRestartCycle(ctx, c); err != nil {
			return err
		}
		// Delivery can wait for a process to exit. Other sessions must be
		// considered from the next snapshot, not from the one it began with.
		return nil
	}
	return nil
}

// The cycle uses the same executor protocol and append-only action journal
// as a message or restart from the phone. No host access is added to the service.
func (s *Server) restartAction(ctx context.Context, c store.RestartCycle, step restartStep) error {
	ctx, cancel := context.WithTimeout(ctx, action.Timeout(step.Kind, execTimeout))
	defer cancel()
	params := map[string]any{"conversation": c.Conversation, "automatic": true, "chars": len([]rune(step.Text))}
	if step.Anyway {
		params["anyway"] = true
	}
	id, err := s.db.LogAttempt(ctx, store.Action{Kind: string(step.Kind), Target: c.Session,
		DeviceName: action.PanelContextRestart, Params: params})
	if err != nil {
		return err
	}
	req := action.Request{ID: requestID(id), Kind: step.Kind, Target: c.Session, Text: step.Text, Device: action.PanelContextRestart}
	if step.Kind == action.SessionRestart {
		plan, planErr := s.restartPlan(ctx, c.Session, map[string]any{"conversation": c.Conversation})
		err = planErr
		if err == nil && plan.Project == nil {
			err = errors.New("the automatic restart no longer has a project")
		}
		req.Project, req.Conversation, req.AutoRestart, req.Anyway = plan.Project, c.Conversation, true, step.Anyway
	}
	if err == nil {
		err = req.Validate()
	}
	var resp action.Response
	if err == nil {
		resp, err = s.exec.Do(ctx, req)
	}
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: resp.Detail, Duration: time.Duration(resp.DurationMs) * time.Millisecond}
	if err == nil && !resp.OK {
		err = errors.New(resp.Error)
	}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
		if errors.Is(err, action.ErrUnavailable) {
			outcome.Result = store.ActionRejected
		}
	} else if s.watch != nil {
		s.watch.Expect(req.Kind, req.Target)
	}
	if logErr := s.db.LogResult(ctx, id, outcome); logErr != nil {
		log.Printf("context restart journal: %v", logErr)
	}
	log.Printf("context restart: %s %s — %s%s", req.Kind, req.Target, outcome.Result, note(outcome.Error))
	return err
}

func (s *Server) sessionReady(w http.ResponseWriter, r *http.Request, target string, params map[string]any) {
	if local, _ := r.Context().Value(localKey{}).(bool); !local {
		http.Error(w, "readiness is accepted only on the local listener", http.StatusForbidden)
		return
	}
	if s.db == nil || s.host == nil {
		http.Error(w, "the restart cycle is unavailable", http.StatusServiceUnavailable)
		return
	}
	var ready action.Ready
	raw, err := json.Marshal(params)
	if err == nil {
		err = json.Unmarshal(raw, &ready)
	}
	if err == nil {
		err = ready.Validate()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	live, ok := s.host.LiveSessionOf(ready.Conversation)
	if !ok || live.Agent == schema.AgentCodex || (target != "" && target != live.Name) {
		http.Error(w, "readiness needs the live Claude conversation the panel asked", http.StatusConflict)
		return
	}
	accepted, err := s.db.ReadyRestartCycle(r.Context(), ready.Conversation, ready.Intent, time.Now())
	if err != nil {
		historyError(w, err)
		return
	}
	if accepted {
		id, err := s.logActionAttempt(r, string(action.SessionReady), live.Name,
			map[string]any{"conversation": ready.Conversation, "chars": len([]rune(ready.Intent))})
		logged := err == nil
		if err == nil {
			err = s.db.LogResult(r.Context(), id, store.ActionOutcome{Result: store.ActionOK, Detail: "Readiness accepted; end the turn and wait."})
		}
		if err != nil {
			log.Printf("readiness journal: %v", err)
		}
		writeJSON(w, map[string]any{"ok": true, "detail": "Readiness accepted; end the turn and wait.", "logged": logged})
		return
	}
	http.Error(w, fmt.Sprintf("the panel has not asked conversation %s to prepare; use session_restart for a restart the person asked for", ready.Conversation), http.StatusConflict)
}
