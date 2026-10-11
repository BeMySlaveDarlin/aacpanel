package executor

import (
	"encoding/json"
	"fmt"

	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

// sessionParent stands a live claude session under another, as the launcher
// stands a session session_open opens under the one that calls it. The
// environment of a running process does not change, so the parent goes into a
// file of the session's claude process, beside its pid and start, where the
// collector reads it; a restart starts the next process with the parent in its
// environment, as the snapshot holds it. That both sessions live and belong to
// one project is the panel's to check: the map is there, not here.
func (e *Executor) sessionParent(target, parent string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	start, ok := procStart(s.PID)
	if !ok {
		return "", fmt.Errorf("session %s ended before it was stood under %s", s.Name, parent)
	}
	body, err := json.Marshal(stream.ProcessParent{Session: parent, Start: start})
	if err == nil {
		err = stream.Keep(stream.ProcessParentPath(s.PID), body)
	}
	if err != nil {
		return "", fmt.Errorf("session %s was not stood under %s: %w", s.Name, parent, err)
	}
	return fmt.Sprintf("session %s stands under %s now", s.Name, parent), nil
}

// codexParent stands a live codex thread under a session, as a thread the
// panel started at the word of that session stands.
func codexParent(th codex.Thread, parent string) (string, error) {
	if err := th.Link.SetParent(th.ID, parent); err != nil {
		return "", fmt.Errorf("session %s was not stood under %s: %w", th.Name, parent, err)
	}
	return fmt.Sprintf("session %s stands under %s now", th.Name, parent), nil
}
