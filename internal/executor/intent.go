package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/sessionintent"
)

// The session's message for its next start wins over either saved rule.
func restartIntentKey(intent string) string {
	if intent != "" {
		return ""
	}
	return sessionintent.Restart
}

// Intents reads the messages of a live session using the account, directory
// and name in its session file, rather than trusting paths from the caller.
func (e *Executor) Intents(_ context.Context, target string) (sessionintent.Values, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	return sessionintent.Read(sessionintent.Dir(), placeOf(s), s.Name)
}

func (e *Executor) sessionIntent(target string, values sessionintent.Values) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if err := sessionintent.Keep(sessionintent.Dir(), placeOf(s), s.Name, values); err != nil {
		return "", fmt.Errorf("the messages of session %s were not kept: %w", s.Name, err)
	}
	return fmt.Sprintf("session %s has its opening messages; empty ones inherit the project", s.Name), nil
}
