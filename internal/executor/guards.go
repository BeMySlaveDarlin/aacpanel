package executor

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
)

// GuardsPath is where the host reads the context guard of every place the map
// knows: the context guard hook and the prompt stamp of every contour, run as
// this user. A line a place — the directory, the cap in percent of the model's
// window, and 1 where a session past it restarts itself — so that a hook in
// any language, a shell one included, reads it without a parser.
func GuardsPath() string {
	return filepath.Join(stateDir(), "guards.tsv")
}

// stateDir is where the executor keeps what the hooks of the host read.
func stateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "aacpanel")
}

// restartingPath is the marker a restart of the session keeps while it is
// under way, named by the session escaped to one path element. The context
// guard hook reads it: the close of a restart lets the session stop once more,
// and a hook that asked for a restart there would ask for a second one.
func restartingPath(session string) string {
	return filepath.Join(stateDir(), "restarting", url.PathEscape(session))
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
func (e *Executor) KeepGuards(_ context.Context, guards []action.Guard) error {
	lines := make([]string, 0, len(guards))
	for _, g := range guards {
		restart := "0"
		if g.Restart {
			restart = "1"
		}
		lines = append(lines, g.Path+"\t"+strconv.Itoa(g.Cap)+"\t"+restart)
	}
	slices.Sort(lines)

	path := GuardsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("the guards directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".guards-*")
	if err != nil {
		return fmt.Errorf("the guards file: %w", err)
	}
	defer os.Remove(tmp.Name())
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if _, err := tmp.WriteString(body); err != nil {
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
