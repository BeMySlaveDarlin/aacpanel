package executor

import (
	"fmt"

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
	th, err := e.codexSession(target)
	if err != nil {
		return "", err
	}
	var key, conversation string
	if th != nil {
		key, conversation = th.ID, th.ID
	} else {
		s, err := findOneLiveSession(target)
		if err != nil {
			return "", err
		}
		key, err = processOwnerKey(s.PID)
		if err != nil {
			return "", err
		}
		conversation = s.SessionID
	}
	if owner := stream.OwnerOf(key); owner.Conversation == "" || owner.Conversation != caller {
		return "", fmt.Errorf("session %s was not opened by the calling session: only its own child can be closed", target)
	}
	if conversation == "" {
		return "", fmt.Errorf("child %s has no known conversation yet; wait for it to start", target)
	}
	return conversation, nil
}
