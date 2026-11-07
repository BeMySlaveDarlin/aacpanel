package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aacpanel/internal/action"
)

// Restart holds the pause lock through this reply, so a waking child cannot
// read its old opener after PassOwners has changed the host's opening record.
func (s *Server) doAction(ctx context.Context, req action.Request) (action.Response, error) {
	resp, err := s.exec.Do(ctx, req)
	if err != nil || resp.Ownership == nil {
		return resp, err
	}
	passed := resp.Ownership
	if req.Kind != action.SessionRestart {
		return resp, fmt.Errorf("an ownership transfer came from an action other than restart")
	}
	if err := passed.Validate(); err != nil {
		return resp, err
	}
	if req.Conversation != "" && req.Conversation != passed.From {
		return resp, fmt.Errorf("the ownership transfer names another restarted conversation")
	}
	if s.db == nil {
		return resp, fmt.Errorf("the ownership transfer needs the database of sleeping sessions")
	}
	// The host action can consume its entire deadline. Its completed transfer
	// still needs a bounded database write after the caller stops waiting.
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	host, err := s.db.HostID(save, s.hostName)
	if err == nil {
		err = s.db.PassPauseOwners(save, host, passed.From, passed.Conversation, passed.Name, time.Now())
	}
	if err != nil {
		err = fmt.Errorf("the host passed its children from %s to %s, but the sleeping sessions could not follow: %w", passed.From, passed.Conversation, err)
		if !resp.OK && resp.Error != "" {
			err = errors.Join(errors.New(resp.Error), err)
		}
	}
	return resp, err
}
