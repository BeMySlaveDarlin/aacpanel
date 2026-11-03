package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

func (e *Executor) openingSession(conversation string) (stream.SessionOwner, error) {
	if conversation == "" {
		return stream.SessionOwner{}, nil
	}
	if th, ok := e.codex.Thread(conversation); ok {
		return stream.SessionOwner{Name: th.Name, Conversation: th.ID}, nil
	}
	s, err := liveSessionOf(conversation)
	if err != nil {
		return stream.SessionOwner{}, fmt.Errorf("the session opening a helper is not live: %w", err)
	}
	return stream.SessionOwner{Name: s.Name, Conversation: conversation}, nil
}

func processOwnerKey(pid int) (string, error) {
	start, ok := procStart(pid)
	if !ok {
		return "", fmt.Errorf("session process %d is gone", pid)
	}
	return fmt.Sprintf("process-%d-%s", pid, start), nil
}

func (e *Executor) checkChild(target, caller string) (string, error) {
	if _, err := e.openingSession(caller); err != nil {
		return "", err
	}
	conversation, owner, err := e.openingRecord(target)
	if err != nil {
		return "", err
	}
	if owner.Conversation == "" || owner.Conversation != caller {
		return "", fmt.Errorf("session %s was not opened by the calling session: only its own child can be closed or answered", target)
	}
	if conversation == "" {
		return "", fmt.Errorf("child %s has no known conversation yet; wait for it to start", target)
	}
	return conversation, nil
}

func (e *Executor) openingRecord(target string) (string, stream.SessionOwner, error) {
	th, err := e.codexSession(target)
	if err != nil {
		return "", stream.SessionOwner{}, err
	}
	var key, conversation string
	if th != nil {
		key, conversation = th.ID, th.ID
	} else {
		s, err := findOneLiveSession(target)
		if err != nil {
			return "", stream.SessionOwner{}, err
		}
		key, err = processOwnerKey(s.PID)
		if err != nil {
			return "", stream.SessionOwner{}, err
		}
		conversation = s.SessionID
	}
	return conversation, stream.OwnerOf(key), nil
}

func (e *Executor) Opening(ctx context.Context, target, conversation string) (*action.Opening, error) {
	current, owner, err := e.openingRecord(target)
	if err != nil {
		return nil, err
	}
	if current != conversation {
		return nil, fmt.Errorf("session %s runs a different conversation now", target)
	}
	if owner.Conversation == "" {
		return nil, nil
	}
	opening := &action.Opening{Name: owner.Name, Conversation: owner.Conversation}
	if th, ok := e.codex.Thread(owner.Conversation); ok {
		opening.Name, opening.Letters = th.Name, true
		return opening, nil
	}
	s, err := liveSessionOf(owner.Conversation)
	if err == nil {
		opening.Name = s.Name
		opening.Letters = s.Socket != ""
		if onStream(s) {
			st, err := streamState(ctx, s)
			opening.Letters = err == nil && st.Protocol >= stream.LettersSince
		}
	}
	return opening, nil
}
