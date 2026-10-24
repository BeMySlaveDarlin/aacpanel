package executor

import (
	"context"
	"errors"
	"os"

	registry "aacpanel/internal/contours"
	"aacpanel/internal/stream"
)

// Question takes the contour from the live session, never a path supplied by
// the service. The root of that contour is where the account wrapper selects
// its credentials, as it does for the limits probe.
func (e *Executor) Question(ctx context.Context, target, conversation, text string) (bool, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return false, err
	}
	if s.SessionID != conversation {
		return false, errors.New("the session runs another conversation")
	}
	for _, c := range registry.Load() {
		if c.Config != s.Config {
			continue
		}
		dir, err := probeDir(c.Prefix)
		if err != nil {
			return false, err
		}
		bin := os.Getenv(probeClaudeEnv)
		if bin == "" {
			bin = "claude"
		}
		return stream.ClassifyQuestion(ctx, bin, dir, text)
	}
	return false, errors.New("the session's contour is not registered")
}
