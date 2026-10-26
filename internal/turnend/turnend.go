// Package turnend keeps whose move follows the session's last turn. The
// declaration belongs to one conversation and expires with its next human
// prompt; it does not change whether the process is busy, waiting or idle.
package turnend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionfile"
)

// MaxWhat bounds the words shown on the session's row and sent in its push.
// A longer request is refused, so the model can shorten it without losing
// its meaning to a silent cut.
const MaxWhat = 300

// Turn is the declaration as it lies on disk. A restarted conversation
// cannot inherit it even when it takes the same place and session name.
type Turn struct {
	ConfigDir string `json:"configDir"`
	Dir       string `json:"dir"`
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
	At        int64  `json:"at"`
	Next      string `json:"next"`
	What      string `json:"what"`
}

// Dir is where the owner's declarations lie, beside the other session files
// in the panel's state directory.
func Dir() string { return sessionfile.Dir("turns") }

// File names a Claude session's declaration by its account, working directory
// and name, or returns empty for an invalid place. The collector computes the
// same hash and checks those fields again before taking the file for its row.
func File(p mcp.Place, name string) string {
	where, ok := p.Clean()
	if !ok {
		return ""
	}
	return sessionfile.Name(where.ConfigDir, where.Dir, name)
}

// ThreadFile names a Codex thread's declaration, or returns empty for an
// invalid thread id. It shares the checklist's thread identity, so both
// readers agree even when the codex home is reached through different paths.
func ThreadFile(thread string) string { return checklist.ThreadFile(thread) }

// Keep validates and writes the bound conversation's declaration. An owner
// declaration must say what the person needs to do; the schema leaves that
// conditional rule to the call, since clients need a plain object schema.
// The whole file is replaced by rename, so the collector sees either turn
// intact even while it reads beside the tool writing.
func Keep(dir string, b mcp.Binding, next, what string, now time.Time) (*Turn, error) {
	where, ok := b.Place.Clean()
	if !ok || b.SessionID == "" {
		return nil, fmt.Errorf("the conversation's place or id is not known")
	}
	if next != "owner" && next != "self" && next != "done" {
		return nil, fmt.Errorf("next must be owner, self or done")
	}
	what = strings.TrimSpace(what)
	if next == "owner" && what == "" {
		return nil, fmt.Errorf("what must say what you need from the person")
	}
	if utf8.RuneCountInString(what) > MaxWhat {
		return nil, fmt.Errorf("what must be at most %d characters", MaxWhat)
	}
	file := File(where, b.Name)
	if b.Codex {
		file = ThreadFile(b.SessionID)
		if file == "" {
			return nil, fmt.Errorf("%q is not the id of a codex thread", b.SessionID)
		}
	}
	turn := &Turn{ConfigDir: where.ConfigDir, Dir: where.Dir, Name: b.Name, SessionID: b.SessionID,
		PID: b.PID, At: now.UnixMilli(), Next: next, What: what}
	body, err := json.Marshal(turn)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".turn-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, file)); err != nil {
		return nil, err
	}
	return turn, nil
}

// Sweep removes declarations nobody has written for as long as an abandoned
// checklist, including temporary files a write left behind, and returns how
// many files it removed. A conversation that is gone must not leave its last
// request on disk forever.
func Sweep(dir string, now time.Time) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	gone := 0
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".turn-")) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) <= checklist.MaxAge {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			gone++
		}
	}
	return gone
}
