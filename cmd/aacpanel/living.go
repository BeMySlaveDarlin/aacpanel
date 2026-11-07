package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"aacpanel/internal/host"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

const livingEvery = 5 * time.Second
const livingFresh = 30 * time.Second

type livingRow struct {
	host.LiveSession
	Profile       string          `json:"profile"`
	ConfigDir     string          `json:"configDir"`
	Pause         *pauseAlarm     `json:"pauseAlarm"`
	Status        string          `json:"status,omitempty"`
	StatusAt      int64           `json:"statusUpdatedAt,omitempty"`
	LastRequestAt string          `json:"lastRequestAt,omitempty"`
	TurnOver      bool            `json:"turnOver,omitempty"`
	Work          json.RawMessage `json:"work,omitempty"`
}

type livingSnapshot struct {
	BootID   string      `json:"bootId"`
	At       int64       `json:"sessionsAt"`
	Notes    []string    `json:"sessionNotes"`
	Sessions []livingRow `json:"sessions"`
}

func (s *Server) livingSnapshot(now time.Time) (livingSnapshot, error) {
	var snap livingSnapshot
	raw, err := s.host.JSON()
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snap, err
	}
	at := time.Unix(snap.At, 0)
	if snap.BootID == "" || snap.At == 0 || now.Sub(at) > livingFresh || at.After(now.Add(time.Second)) || len(snap.Notes) > 0 || snap.Sessions == nil {
		return snap, fmt.Errorf("a complete fresh host census with its boot identity is not available")
	}
	return snap, nil
}

func (s *Server) keepLiving(ctx context.Context, now time.Time) error {
	snap, err := s.livingSnapshot(now)
	if err != nil {
		return err
	}
	profiles, err := s.db.Profiles(ctx)
	if err != nil {
		return err
	}
	hostID, err := s.db.HostID(ctx, s.hostName)
	if err != nil {
		return err
	}
	parents := map[string]string{}
	for _, row := range snap.Sessions {
		parents[row.Name] = row.SessionID
	}
	live := make([]store.LivingSession, 0, len(snap.Sessions))
	for _, row := range snap.Sessions {
		if row.SessionID == "" {
			continue
		}
		item := store.LivingSession{Conversation: row.SessionID, Session: row.Name, CWD: row.CWD, Contour: row.Profile,
			ConfigDir: row.ConfigDir, Transport: row.Transport, Agent: row.Agent, Outside: row.Outside}
		if item.Agent == "" {
			item.Agent = schema.AgentClaude
		}
		if row.Parent != nil {
			item.Parent = row.Parent.Session
			item.ParentConversation = parents[item.Parent]
		}
		if row.Pause != nil {
			item.Pause, err = json.Marshal(row.Pause)
			if err != nil {
				return err
			}
		}
		if found, ok := projectAt(profiles, row.CWD, s.worktrees(), s.db.ProjectRoots()); ok {
			item.ProjectID, item.Project = found.project.ID, found.project.Name
			item.Contour, item.ConfigDir = found.profile.Name, found.profile.ConfigDir
		}
		live = append(live, item)
	}
	return s.db.KeepLiving(ctx, hostID, snap.BootID, time.Unix(snap.At, 0), live)
}

func (s *Server) runLiving(ctx context.Context) {
	tick := time.NewTicker(livingEvery)
	defer tick.Stop()
	lastErr := ""
	for {
		err := s.keepLiving(ctx, time.Now())
		if err != nil && err.Error() != lastErr {
			log.Printf("session census: %v", err)
			lastErr = err.Error()
		}
		if err == nil {
			lastErr = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) archivedParent(ctx context.Context, conversation string) (string, string, error) {
	if s.db == nil || s.host == nil {
		return "", "", nil
	}
	hostID, err := s.db.HostID(ctx, s.hostName)
	if err != nil {
		return "", "", err
	}
	row, err := s.db.LivingConversation(ctx, hostID, conversation)
	pauses, pauseErr := s.sessionPauses(ctx)
	if pauseErr != nil {
		return "", "", pauseErr
	} else if p, ok := pauseConversation(pauses, conversation); ok {
		row = &p.LivingSession
	}
	if err != nil || row == nil || row.Parent == "" {
		return "", "", err
	}
	parent, live := s.host.LiveSession(row.Parent)
	if row.ParentConversation != "" {
		parent, live = s.host.LiveSessionOf(row.ParentConversation)
	}
	if !live || parent.Outside {
		for _, p := range pauses {
			if p.Sleeping && ((row.ParentConversation != "" && p.Conversation == row.ParentConversation) || (row.ParentConversation == "" && p.Session == row.Parent)) {
				return p.Session, "", nil
			}
		}
		return "", "parent " + row.Parent + " is not live; resumed without a parent", nil
	}
	return parent.Name, "", nil
}
