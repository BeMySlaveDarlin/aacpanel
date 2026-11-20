package executor

import (
	"context"
	"errors"
	"os"
	"strings"

	"aacpanel/internal/action"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/stream"
)

func (e *Executor) ChecklistCurrent(ctx context.Context, target, conversation string, input action.ChecklistInput) (action.ChecklistVerdict, error) {
	if err := (action.Request{Ask: action.AskChecklist, Target: target, Conversation: conversation, Checklist: &input}).Validate(); err != nil {
		return action.ChecklistVerdict{}, err
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return action.ChecklistVerdict{}, err
	}
	if s.SessionID != conversation {
		return action.ChecklistVerdict{}, errors.New("the session runs another conversation")
	}
	for _, c := range registry.Load() {
		if c.Config != s.Config {
			continue
		}
		dir, err := probeDir(c.Prefix)
		if err != nil {
			return action.ChecklistVerdict{}, err
		}
		bin := os.Getenv(probeClaudeEnv)
		if bin == "" {
			bin = "claude"
		}
		return stream.ClassifyChecklist(ctx, bin, dir, input)
	}
	return action.ChecklistVerdict{}, errors.New("the session's contour is not registered")
}

func (e *Executor) Children(ctx context.Context, target, conversation string) ([]action.ChildSession, error) {
	owner, err := e.openingSession(conversation)
	if err != nil {
		return nil, err
	}
	if owner.Name != target || owner.Conversation == "" {
		return nil, errors.New("the session runs another conversation")
	}
	records, err := stream.OpenedSessions()
	if err != nil {
		return nil, err
	}
	var children []action.ChildSession
	for _, record := range records {
		if record.Conversation != conversation {
			continue
		}
		id := record.SessionID
		if id == "" && !strings.HasPrefix(record.Key, "process-") {
			id = record.Key
		}
		children = append(children, action.ChildSession{Key: record.Key, Conversation: id})
	}
	return children, nil
}
