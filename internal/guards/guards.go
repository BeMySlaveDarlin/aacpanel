// Package guards is the context guard of every place the map knows, as the
// executor keeps it on the host for the hooks of sessions: guards.tsv, and
// the marker of a restart under way. The executor writes both; the context
// guard and the prompt stamp read them — the executor's hooks through this
// package, the delivery's scripts through guards.py, which reads the file by
// the same rules. The two share their cases in deploy/claude/testdata/guards.json.
package guards

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/action"
)

// CapDefault is the cap of a place the file names none for, in percent of
// the model's window: the prompt stamp says where it falls all the same.
const CapDefault = 80

// RestartMargin is how far under the cap, in points of the window, a session
// of a project that restarts its sessions is restarted at the end of a turn.
// A session wraps its work up for the restart it sees coming and stops a
// little short of the cap, and a session stopped under it does not stop again
// until someone writes to it. The prompt stamp names the cap itself, never the
// margin: told of the margin, a session would stop short of that instead.
const RestartMargin = 1.0

// Home is the directory the executor keeps what the hooks of the host read
// in, under the user's state directory.
func Home() string {
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

// Path is the file of the guards. A line a place — the directory, the cap in
// percent of the model's window, and 1 where a session past it restarts
// itself — so that a hook in any language, a shell one included, reads it
// without a parser.
func Path() string {
	return filepath.Join(Home(), "guards.tsv")
}

// RestartingPath is the marker a restart of the session keeps while it is
// under way, named by the session escaped to one path element. The context
// guard reads it: the close of a restart lets the session stop once more, and
// a hook that asked for a restart there would ask for a second one.
func RestartingPath(session string) string {
	return filepath.Join(Home(), "restarting", url.PathEscape(session))
}

// Lines is the body of the file for the guards: a line a place, sorted.
func Lines(list []action.Guard) string {
	lines := make([]string, 0, len(list))
	for _, g := range list {
		restart := "0"
		if g.Restart {
			restart = "1"
		}
		lines = append(lines, g.Path+"\t"+strconv.Itoa(g.Cap)+"\t"+restart)
	}
	slices.Sort(lines)
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return body
}

// Of is the guard of the place closest above the directory, read from the
// file; false where the file names none or is not there.
func Of(dir string) (action.Guard, bool) {
	raw, err := os.ReadFile(Path())
	if err != nil {
		return action.Guard{}, false
	}
	return Closest(string(raw), dir)
}

// Closest is the guard of the place closest above the directory in the body
// of a file: the longest path that is the directory itself or holds it — a
// project inside its contour's tree — the first of two as long. A line that
// is not three fields, or whose cap is not a whole number, is passed over.
func Closest(body, dir string) (action.Guard, bool) {
	var best action.Guard
	found := false
	if dir == "" {
		return best, false
	}
	for _, line := range strings.FieldsFunc(body, lineBreak) {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		place := parts[0]
		if dir != place && !strings.HasPrefix(dir, strings.TrimRight(place, "/")+"/") {
			continue
		}
		if found && len(place) <= len(best.Path) {
			continue
		}
		limit, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		best, found = action.Guard{Path: place, Cap: limit, Restart: parts[2] == "1"}, true
	}
	return best, found
}

// lineBreak tells the characters guards.py splits the lines of the file at,
// which are python's: a path holding one of them is two broken lines there,
// and so it is here.
func lineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}
