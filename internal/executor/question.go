package executor

import (
	"context"
	"errors"
	"os"

	registry "aacpanel/internal/contours"
	formquestion "aacpanel/internal/question"
	"aacpanel/internal/stream"
)

// Question takes the contour from the live session, never a path supplied by
// the service. The root of that contour is where the account wrapper selects
// its credentials, as it does for the limits probe.
func (e *Executor) Question(ctx context.Context, target, conversation, text string, alreadyAsked []string) (formquestion.Verdict, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return formquestion.Verdict{}, err
	}
	if s.SessionID != conversation {
		return formquestion.Verdict{}, errors.New("the session runs another conversation")
	}
	for _, c := range registry.Load() {
		if c.Config != s.Config {
			continue
		}
		dir, err := probeDir(c.Prefix)
		if err != nil {
			return formquestion.Verdict{}, err
		}
		bin := os.Getenv(probeClaudeEnv)
		if bin == "" {
			bin = "claude"
		}
		return stream.ClassifyQuestion(ctx, bin, dir, text, alreadyAsked)
	}
	return formquestion.Verdict{}, errors.New("the session's contour is not registered")
}
