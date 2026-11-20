package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/checklist"
	"aacpanel/internal/host"
	"aacpanel/internal/schema"
	"aacpanel/internal/session"
)

const idleChecklistKeep = 10 * time.Minute

type idleChecklistRow struct {
	host.LiveSession
	OpenedKey string          `json:"openedKey"`
	Status    string          `json:"status"`
	TurnOver  bool            `json:"turnOver"`
	Work      json.RawMessage `json:"work"`
	Processes int             `json:"processes"`
	Pause     json.RawMessage `json:"pauseAlarm"`
	NextAlarm *struct {
		Next string `json:"next"`
	} `json:"nextAlarm"`
}

func (row idleChecklistRow) working() bool {
	busy, _ := session.BackgroundWork(row.Work)
	return (row.Status == "busy" && !row.TurnOver) || busy || row.Processes > 0
}

func checklistWaiting(row idleChecklistRow, now time.Time) bool {
	busy, _ := session.BackgroundWork(row.Work)
	if busy || row.Processes > 0 || (len(row.Pause) > 0 && string(row.Pause) != "null") {
		return false
	}
	if row.NextAlarm != nil {
		if at, err := time.Parse(time.RFC3339Nano, row.NextAlarm.Next); err == nil && !at.After(now.Add(30*time.Minute)) {
			return false
		}
	}
	return true
}

func checklistChildWorking(children []action.ChildSession, rows []idleChecklistRow) bool {
	for _, child := range children {
		for _, row := range rows {
			if (row.OpenedKey != "" && row.OpenedKey == child.Key) ||
				(row.OpenedKey == "" && child.Conversation != "" && row.SessionID == child.Conversation) ||
				(row.Agent == schema.AgentCodex && row.SessionID == child.Key) {
				if row.working() {
					return true
				}
			}
		}
	}
	return false
}

type idleChecklistResult struct {
	done  chan struct{}
	at    time.Time
	block bool
}

type idleChecklistCache struct {
	mu      sync.Mutex
	results map[string]*idleChecklistResult
}

func (c *idleChecklistCache) check(ctx context.Context, key string, review func() bool) bool {
	c.mu.Lock()
	if c.results == nil {
		c.results = map[string]*idleChecklistResult{}
	}
	for k, result := range c.results {
		if !result.at.IsZero() && time.Since(result.at) >= idleChecklistKeep {
			delete(c.results, k)
		}
	}
	if previous, ok := c.results[key]; ok {
		c.mu.Unlock()
		select {
		case <-previous.done:
			return previous.block
		case <-ctx.Done():
			return true
		}
	}
	result := &idleChecklistResult{done: make(chan struct{})}
	c.results[key] = result
	c.mu.Unlock()
	block := review()
	c.mu.Lock()
	result.block, result.at = block, time.Now()
	close(result.done)
	c.mu.Unlock()
	return block
}

func (s *Server) apiChecklistIdle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Conversation string           `json:"conversation"`
		Session      string           `json:"session"`
		Message      string           `json:"message"`
		Span         string           `json:"span"`
		Checklist    []checklist.Item `json:"checklist"`
		Codex        bool             `json:"codex"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || !uuidRE.MatchString(body.Conversation) || !uuidRE.MatchString(body.Message) || body.Session == "" ||
		len([]rune(body.Span)) > action.ChecklistLogMax || (body.Codex && body.Span != "") || (!body.Codex && (strings.TrimSpace(body.Span) == "" || len(body.Checklist) == 0)) {
		http.Error(w, "a checklist review needs its conversation, session, message, steps and work log", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Second)
	defer cancel()
	block := s.idleChecklists.check(ctx, body.Conversation+"\x00"+body.Message, func() bool {
		return s.reviewIdleChecklist(ctx, body.Conversation, body.Session, body.Span, body.Checklist, body.Codex, time.Now())
	})
	writeJSON(w, map[string]bool{"block": block})
}

func (s *Server) reviewIdleChecklist(ctx context.Context, conversation, name, span string, steps []checklist.Item, codex bool, now time.Time) bool {
	if s.host == nil {
		return true
	}
	raw, err := s.host.Raw()
	if err != nil {
		return true
	}
	var snapshot struct {
		Sessions []idleChecklistRow `json:"sessions"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return true
	}
	var live *idleChecklistRow
	for i := range snapshot.Sessions {
		row := &snapshot.Sessions[i]
		if row.SessionID == conversation && row.Name == name && !row.Outside && (row.Agent == schema.AgentCodex) == codex {
			live = row
			break
		}
	}
	if live == nil {
		return false
	}
	if !checklistWaiting(*live, now) {
		return false
	}
	if s.db == nil {
		return true
	}
	pauses, err := s.sessionPauses(ctx)
	if err != nil {
		return true
	}
	if _, paused := pauseConversation(pauses, conversation); paused {
		return false
	}
	cycles, err := s.db.RestartCycles(ctx)
	if err != nil {
		return true
	}
	for _, cycle := range cycles {
		if cycle.Conversation == conversation && !cycle.Done {
			return false
		}
	}
	if s.exec == nil {
		return true
	}
	children, err := s.exec.Children(ctx, name, conversation)
	if err != nil {
		return true
	}
	if checklistChildWorking(children, snapshot.Sessions) {
		return false
	}
	if codex {
		return true
	}
	verdict, err := s.exec.ChecklistCurrent(ctx, name, conversation, action.ChecklistInput{Checklist: action.ChecklistState{Items: steps}, Log: span})
	return err != nil || !(verdict.Current && verdict.Confidence >= 0.7)
}
