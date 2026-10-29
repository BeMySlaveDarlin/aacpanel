package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionintent"
)

// The session's message for its next start wins over either saved rule.
func restartIntentKey(intent string) string {
	if intent != "" {
		return ""
	}
	return sessionintent.Restart
}

// Intents reads the messages using the live session's place and name, rather
// than trusting paths from the caller.
func (e *Executor) Intents(_ context.Context, target string) (sessionintent.Values, error) {
	place, name, err := e.intentPlace(target)
	if err != nil {
		return nil, err
	}
	return sessionintent.Read(sessionintent.Dir(), place, name)
}

func (e *Executor) sessionIntent(target string, values sessionintent.Values) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	return keepIntent(placeOf(s), s.Name, values)
}

func keepIntent(place mcp.Place, name string, values sessionintent.Values) (string, error) {
	if err := sessionintent.Keep(sessionintent.Dir(), place, name, values); err != nil {
		return "", fmt.Errorf("the messages of session %s were not kept: %w", name, err)
	}
	return fmt.Sprintf("session %s has its opening messages; empty ones inherit the project", name), nil
}

func (e *Executor) intentPlace(target string) (mcp.Place, string, error) {
	th, err := e.codexSession(target)
	if err != nil {
		return mcp.Place{}, "", err
	}
	if th != nil {
		return codexPlace(*th), th.Name, nil
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return mcp.Place{}, "", err
	}
	return placeOf(s), s.Name, nil
}
