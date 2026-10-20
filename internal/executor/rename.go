package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/role"
	"aacpanel/internal/sessionintent"
	"aacpanel/internal/stream"
)

// sessionRename gives a live session on the stream a new name. Claude takes it
// with a request of its own and writes it into the file of the session, which
// is where the panel and the host look a session up by name; the holder is
// keyed by the conversation and does not notice. A name another live session
// answers to is refused: the two would answer to it at once. A terminal is
// renamed on its own screen, /rename with keys.
func (e *Executor) sessionRename(ctx context.Context, target, name string) (string, error) {
	// A claude session named like a codex thread passed the check of a
	// thread's name; its own name is a key, and keeps to the stricter rule.
	if err := action.ValidSessionName(name); err != nil {
		return "", err
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if !onStream(s) {
		return "", fmt.Errorf("session %s lives in tmux: it is renamed on its own screen, /rename with keys", s.Name)
	}
	namesakes, _, err := liveSessionsByName(name)
	if err != nil {
		return "", err
	}
	if len(namesakes) > 0 {
		return "", fmt.Errorf("a live session is called %s already: two sessions would answer to one name", name)
	}
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "rename_session",
		Fields: map[string]any{"title": name, "source": "host"}}); err != nil {
		return "", err
	}
	// The role is kept by the name, so it follows the session to the new one.
	if err := role.Move(role.Dir(), placeOf(s), s.Name, name); err != nil {
		return fmt.Sprintf("session %s is called %s now; its role stayed under the old name: %v", s.Name, name, err), nil
	}
	if err := sessionintent.Move(sessionintent.Dir(), placeOf(s), s.Name, name); err != nil {
		return fmt.Sprintf("session %s is called %s now; its messages stayed under the old name: %v", s.Name, name, err), nil
	}
	return fmt.Sprintf("session %s is called %s now", s.Name, name), nil
}
