package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/stream"
)

// sessionRename gives a live session on the stream a new name. Claude takes it
// with a request of its own and writes it into the file of the session, which
// is where the panel and the host look a session up by name; the holder is
// keyed by the conversation and does not notice. A name another live session
// answers to is refused: the two would answer to it at once. A terminal is
// renamed on its own screen, /rename with keys.
func (e *Executor) sessionRename(ctx context.Context, target, name string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if !onStream(s) {
		return "", fmt.Errorf("session %s runs in a console: it is renamed on its own screen, /rename with keys", s.Name)
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
	return fmt.Sprintf("session %s is called %s now", s.Name, name), nil
}
