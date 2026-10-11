package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/guards"
)

// GuardsPath is where the host reads the context guard of every place the map
// knows: the context guard hook and the prompt stamp of every contour, run as
// this user (internal/guards).
func GuardsPath() string {
	return guards.Path()
}

// stateDir is where the executor keeps what the hooks of the host read.
func stateDir() string {
	return guards.Home()
}

// restartingPath is the marker a restart of the session keeps while it is
// under way, which the context guard hook reads.
func restartingPath(session string) string {
	return guards.RestartingPath(session)
}

// markRestarting keeps the marker of a restart of the session until the
// function it returns is called, whatever the restart ends with. A marker that
// cannot be written costs the hook its hint and nothing more: the restart goes
// on, and a second one is still refused here.
func markRestarting(session string) func() {
	path := restartingPath(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("restart: no marker for %s: %v", session, err)
		return func() {}
	}
	if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		log.Printf("restart: no marker for %s: %v", session, err)
		return func() {}
	}
	return func() { os.Remove(path) }
}

// KeepGuards writes the guards the service computed from the map. The file is
// replaced whole by a rename: a hook reading it mid-write sees the old places
// or the new ones, never half of each.
func (e *Executor) KeepGuards(_ context.Context, list []action.Guard) error {
	path := GuardsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("the guards directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".guards-*")
	if err != nil {
		return fmt.Errorf("the guards file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(guards.Lines(list)); err != nil {
		tmp.Close()
		return fmt.Errorf("the guards file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("the guards file: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("the guards file: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}
