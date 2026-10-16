package executor

import (
	"encoding/json"
	"fmt"
	"os"

	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

// sessionParent stands a live claude session under another, as the launcher
// stands a session session_open opens under the one that calls it. The
// environment of a running process does not change, so the parent goes into a
// file of the session's claude process, beside its pid and start, where the
// collector reads it; a restart starts the next process with the parent in its
// environment. That both sessions live and belong to one project is the
// panel's to check: the map is there, not here.
func (e *Executor) sessionParent(target, parent string) (string, error) {
	name, err := keepProcessParent(target, parent, "stood under "+parent)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("session %s stands under %s now", name, parent), nil
}

// sessionDetach takes a live claude session from under the session it stands
// under. The parent it was opened with stays in the environment of its
// process, so the file of the process says it stands under none: over the
// environment, and in place of a parent stood later, which the same file
// held. A restart starts the next process with no parent at all.
func (e *Executor) sessionDetach(target string) (string, error) {
	name, err := keepProcessParent(target, "", "taken from under its parent")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("session %s stands on its own now", name), nil
}

// keepProcessParent writes the file of the claude process of a live session:
// the session it stands under, none for an empty parent, and the start of the
// process. It returns the name of the session; what is what was done to it,
// for the errors.
func keepProcessParent(target, parent, what string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	start, ok := procStart(s.PID)
	if !ok {
		return "", fmt.Errorf("session %s ended before it was %s", s.Name, what)
	}
	body, err := json.Marshal(stream.ProcessParent{Session: parent, Start: start})
	if err == nil {
		err = stream.Keep(stream.ProcessParentPath(s.PID), body)
	}
	if err != nil {
		return "", fmt.Errorf("session %s was not %s: %w", s.Name, what, err)
	}
	return s.Name, nil
}

// keptParent is the parent a restart or a move starts the next process of a
// session with: the one in the file of its process while the file is that
// process's — standing a session under another or taking it from under one is
// the later word — and else the one the request names. The panel names the
// parent its snapshot holds, read from the file a moment after the executor
// wrote it, and a move asked for with the panel down names the parent the
// session was started with.
func keptParent(pid int, named string) string {
	start, ok := procStart(pid)
	if !ok {
		return named
	}
	raw, err := os.ReadFile(stream.ProcessParentPath(pid))
	if err != nil {
		return named
	}
	var kept stream.ProcessParent
	if json.Unmarshal(raw, &kept) != nil || kept.Start != start {
		return named
	}
	return kept.Session
}

// codexParent stands a live codex thread under a session, as a thread the
// panel started at the word of that session stands.
func codexParent(th codex.Thread, parent string) (string, error) {
	if err := th.Link.SetParent(th.ID, parent); err != nil {
		return "", fmt.Errorf("session %s was not stood under %s: %w", th.Name, parent, err)
	}
	return fmt.Sprintf("session %s stands under %s now", th.Name, parent), nil
}

// codexDetach takes a live codex thread from under the session it stands
// under: a thread has no process to carry a parent in, only the file the
// executor keeps for it, and the file names none.
func codexDetach(th codex.Thread) (string, error) {
	if err := th.Link.SetParent(th.ID, ""); err != nil {
		return "", fmt.Errorf("session %s was not taken from under its parent: %w", th.Name, err)
	}
	return fmt.Sprintf("session %s stands on its own now", th.Name), nil
}
