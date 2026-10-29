package executor

import (
	"fmt"
	"time"

	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
)

// sessionRole sets the role of a live claude session as the person gives it,
// or clears it. It goes into the file the panel's server tells the session its
// role from, kept by the account the session runs in, the repository it works
// in and its name — all read from the file claude keeps of the session — and
// the collector carries it into the row the prompt stamp tells the running
// conversation from, with its next prompt or batch of tools: a session on the
// stream and one in tmux take it alike. Nothing is typed into the
// conversation: words typed there would reach the model as the person's
// message.
func (e *Executor) sessionRole(target, text string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	return keepRole(placeOf(s), s.Name, text)
}

func keepRole(place mcp.Place, name, text string) (string, error) {
	kept, err := role.Keep(role.Dir(), place, name, text, role.ByPerson, time.Now())
	if err != nil {
		return "", fmt.Errorf("the role of session %s was not kept: %w", name, err)
	}
	if kept == nil {
		return fmt.Sprintf("session %s has no role now", name), nil
	}
	return fmt.Sprintf("session %s has its role; it is told it with its next prompt", name), nil
}

// placeOf is where a live session works, as the panel's server places it.
func placeOf(s liveSession) mcp.Place {
	return mcp.Place{ConfigDir: s.Config, Dir: s.CWD}
}
