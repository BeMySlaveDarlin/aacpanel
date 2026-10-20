package executor

import (
	"fmt"
	"time"

	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionpin"
)

func (e *Executor) sessionPin(target string, on bool) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	return keepPin(placeOf(s), s.Name, on)
}

func keepPin(place mcp.Place, name string, on bool) (string, error) {
	if err := sessionpin.Keep(sessionpin.Dir(), place, name, on, time.Now()); err != nil {
		return "", fmt.Errorf("the pin of session %s was not kept: %w", name, err)
	}
	if on {
		return fmt.Sprintf("the group of session %s is pinned", name), nil
	}
	return fmt.Sprintf("the group of session %s is not pinned", name), nil
}
