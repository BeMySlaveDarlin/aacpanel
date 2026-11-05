package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/schema"
)

const lastEvery = 20 * time.Second

// sessionLast is what the card of a live conversation shows beyond its
// transcript: the step of its checklist and how the session was run. The
// archive card shows the last of it once the session is gone. Done counts the
// steps behind the session, the dropped ones with the done, as the card
// numbers its step; Step is the step it stood on, empty with none left.
type sessionLast struct {
	Step        string `json:"step,omitempty"`
	Done        int    `json:"done"`
	Total       int    `json:"total"`
	Agent       string `json:"agent"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Transport   string `json:"transport,omitempty"`
	Cap         *int   `json:"cap,omitempty"`
	AutoRestart *bool  `json:"autoRestart,omitempty"`
	Remote      string `json:"remote,omitempty"`
	Checkout    string `json:"checkout,omitempty"`
}

type lastRow struct {
	SessionID   string `json:"sessionId"`
	Outside     bool   `json:"outside"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Mode        string `json:"mode"`
	Transport   string `json:"transport"`
	Cap         *int   `json:"cap"`
	AutoRestart *bool  `json:"autoRestart"`
	Remote      string `json:"remote"`
	Checkout    string `json:"checkout"`
	Checklist   *struct {
		Items []struct {
			Text   string `json:"text"`
			Status string `json:"status"`
		} `json:"items"`
	} `json:"checklist"`
}

func lastOf(row lastRow) sessionLast {
	out := sessionLast{Agent: row.Agent, Model: row.Model, Effort: row.Effort, Mode: row.Mode,
		Transport: row.Transport, Cap: row.Cap, AutoRestart: row.AutoRestart, Remote: row.Remote, Checkout: row.Checkout}
	if out.Agent == "" {
		out.Agent = schema.AgentClaude
	}
	if row.Checklist == nil {
		return out
	}
	active, pending := "", ""
	for _, item := range row.Checklist.Items {
		out.Total++
		switch item.Status {
		case "done", "dropped":
			out.Done++
		case "active":
			if active == "" {
				active = item.Text
			}
		case "pending":
			if pending == "" {
				pending = item.Text
			}
		}
	}
	out.Step = active
	if out.Step == "" {
		out.Step = pending
	}
	return out
}

// runSessionLast keeps what the cards of the live conversations show, on a
// schedule of its own: a session can end while no screen polls the host. A
// failure is said once and again only when it changes.
func (s *Server) runSessionLast(ctx context.Context) {
	kept := map[string]string{}
	failed := ""
	t := time.NewTicker(lastEvery)
	defer t.Stop()
	for {
		err := s.keepLast(ctx, kept)
		switch {
		case err != nil && err.Error() != failed:
			log.Printf("the last cards of the sessions: %v", err)
			failed = err.Error()
		case err == nil:
			failed = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// keepLast writes the cards that changed since the last look; kept is what
// was written, by conversation, and forgets the conversations no longer live.
func (s *Server) keepLast(ctx context.Context, kept map[string]string) error {
	if s.db == nil || s.host == nil {
		return nil
	}
	raw, err := s.host.JSON()
	if err != nil {
		return err
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return err
	}
	var snapshot struct {
		Sessions []lastRow `json:"sessions"`
	}
	if err := json.Unmarshal(sessionsGuarded(raw, list), &snapshot); err != nil {
		return err
	}
	live := map[string]bool{}
	for _, row := range snapshot.Sessions {
		if row.SessionID == "" || row.Outside {
			continue
		}
		live[row.SessionID] = true
		body, err := json.Marshal(lastOf(row))
		if err != nil {
			return err
		}
		if kept[row.SessionID] == string(body) {
			continue
		}
		if err := s.db.KeepSessionLast(ctx, row.SessionID, body); err != nil {
			return err
		}
		kept[row.SessionID] = string(body)
	}
	for id := range kept {
		if !live[id] {
			delete(kept, id)
		}
	}
	return nil
}

// lastOfArchive gives the archived conversations what their cards last showed
// while they were live; a conversation the panel never saw live has none.
func (s *Server) lastOfArchive(ctx context.Context, rows []chat.ArchiveRow) {
	if s.db == nil || len(rows) == 0 {
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SessionID)
	}
	lasts, err := s.db.SessionLasts(ctx, ids)
	if err != nil {
		log.Printf("session archive: the last cards are unavailable: %v", err)
		return
	}
	for i := range rows {
		rows[i].Last = lasts[rows[i].SessionID]
	}
}
