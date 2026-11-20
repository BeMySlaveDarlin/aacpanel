package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/store"
)

// Missing conversations have no row in the live loop. Only a persisted
// restart attempt authorizes opening one again; a preparation alone does not.
func (s *Server) recoverRestarts(ctx context.Context, cycles []store.RestartCycle, snap restartCensus, now time.Time) (bool, error) {
	at := time.Unix(snap.At, 0)
	for _, c := range cycles {
		if c.Done || !at.After(c.CommandAt) {
			continue
		}
		old, replacement := false, false
		for _, row := range snap.Sessions {
			old = old || row.SessionID == c.Conversation
			replacement = replacement || row.Name == c.Session || (c.RecoveredSession != "" && row.Name == c.RecoveredSession)
		}
		if c.RecoveredSession == "" && !old && replacement {
			return true, s.finishRestart(ctx, c, "replacement is live")
		}
		if c.AttemptedAt != nil && (!at.After(*c.AttemptedAt) || now.Before(c.AttemptedAt.Add(restartGrace))) {
			continue
		}
		if c.Attempts >= restartAttempts {
			return true, s.finishRestart(ctx, c, "attempt limit reached")
		}
		if old {
			continue
		}
		closed, err := s.db.RestartClosed(ctx, c)
		if err != nil {
			return false, err
		}
		if closed {
			return true, s.finishRestart(ctx, c, "closed outside the restart cycle")
		}
		if c.Attempts == 0 || (c.Finish != "" && c.Finish != store.PrepareRestart) || (c.RecoveredSession != "" && !replacement) {
			return true, s.finishRestart(ctx, c, "conversation is closed")
		}
		c.Attempts++
		c.AttemptedAt = &now
		if err := s.db.KeepRestartCycle(ctx, c); err != nil {
			return false, err
		}
		if err := s.reopenRestart(ctx, &c); err != nil {
			log.Printf("context restart of %s: recovery attempt %d of %d: %v", c.Session, c.Attempts, restartAttempts, err)
			if c.Attempts == restartAttempts {
				return true, s.finishRestart(ctx, c, "attempt limit reached")
			}
			return true, nil
		}
		return true, s.finishRestart(ctx, c, "replacement started and intent delivered")
	}
	return false, nil
}

func (s *Server) finishRestart(ctx context.Context, c store.RestartCycle, reason string) error {
	c.Done, c.AttemptedAt = true, nil
	if err := s.db.KeepRestartCycle(ctx, c); err != nil {
		return err
	}
	log.Printf("context restart of %s: %s", c.Session, reason)
	return nil
}

func (s *Server) reopenRestart(ctx context.Context, c *store.RestartCycle) error {
	params := map[string]any{"conversation": c.Conversation, "automatic": true, "recovery": true, "attempt": c.Attempts}
	if c.RecoveredSession == "" {
		project, err := s.recoveryProject(ctx, *c)
		req := action.Request{Kind: action.SessionOpen, Target: c.Session, Conversation: c.Conversation,
			Project: project, Device: action.PanelContextRestart}
		resp, err := s.restartDelivery(ctx, req, params, err)
		if err != nil {
			return err
		}
		c.RecoveredSession = resp.Session
		if c.RecoveredSession == "" {
			c.RecoveredSession = c.Session
		}
		// A failed message delivery must retry on this session, not open
		// another one after the service starts again.
		if err := s.db.KeepRestartCycle(ctx, *c); err != nil {
			return err
		}
	}
	if c.Intent == "" {
		return nil
	}
	_, err := s.restartDelivery(ctx, action.Request{Kind: action.SessionSend, Target: c.RecoveredSession,
		Text: c.Intent, Device: action.PanelContextRestart}, params, nil)
	return err
}

func (s *Server) recoveryProject(ctx context.Context, c store.RestartCycle) (*action.Project, error) {
	if c.CWD == "" {
		host, err := s.db.HostID(ctx, s.hostName)
		if err != nil {
			return nil, err
		}
		living, err := s.db.LivingConversation(ctx, host, c.Conversation)
		if err != nil {
			return nil, err
		}
		if living != nil {
			c.CWD, c.Parent = living.CWD, living.Parent
		} else {
			c.CWD, _, err = s.db.SessionResumeAt(ctx, host, c.Conversation)
			if err != nil {
				return nil, err
			}
		}
	}
	if c.CWD == "" {
		return nil, fmt.Errorf("the closed conversation's project is not known")
	}
	project, _, err := s.launchProject(ctx, nil, c.CWD, "")
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("the closed conversation no longer belongs to a project of the map")
	}
	if err := restartsClaude(c.Session, project.Launch); err != nil {
		return nil, err
	}
	project.Session, project.Parent = c.Session, c.Parent
	return project, nil
}
