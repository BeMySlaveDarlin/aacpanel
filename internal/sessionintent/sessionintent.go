// Package sessionintent keeps a session's opening messages beside its role,
// under the same account, repository and name. Empty messages inherit the map.
package sessionintent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
	"aacpanel/internal/sessionfile"
)

// First names the message sent at an open or a resume, under the same key
// the map uses so that the session can override its inherited value.
const First = "intent"

// Restart names the message a restart sends instead of the first message,
// unless a continued conversation carries words the session wrote for it.
const Restart = "restartIntent"

// Values holds the session's own messages by their launch keys. A missing or
// empty value inherits the map, so the two messages can be cleared separately.
type Values map[string]string

// Pick leaves an inherited value intact when the session sets none.
func (v Values) Pick(key, inherited string) string {
	if own := v[key]; own != "" {
		return own
	}
	return inherited
}

type record struct {
	ConfigDir string `json:"configDir"`
	Repo      string `json:"repo"`
	Name      string `json:"name"`
	Values    Values `json:"values"`
}

// Dir is where the messages lie in the owner's state, beside the roles whose
// account, repository and session-name key they share.
func Dir() string { return sessionfile.Dir("intents") }

// Updates of the two messages may arrive separately, so their read and write
// share a lock in the executor that owns this store.
var mu sync.Mutex

// Read returns the messages of the session named name at place p, or an empty
// set when it has none. A file that cannot be read or names another session
// returns an error, so the caller can say why its overrides are unavailable.
func Read(dir string, p mcp.Place, name string) (Values, error) {
	where, ok := p.Clean()
	if !ok {
		return nil, fmt.Errorf("the place of the session is not known")
	}
	raw, err := os.ReadFile(filepath.Join(dir, role.File(where, name)))
	if errors.Is(err, os.ErrNotExist) {
		return Values{}, nil
	}
	if err != nil {
		return nil, err
	}
	var r record
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.ConfigDir != where.ConfigDir || r.Repo != role.Repo(where.Dir) || r.Name != name {
		return nil, fmt.Errorf("the messages belong to another session")
	}
	if r.Values == nil {
		return Values{}, nil
	}
	return r.Values, nil
}

// Keep changes only the messages named in change; an empty text removes its
// override. The file is replaced whole, so a launch sees the old set or the
// new one, and separate updates cannot erase each other's message.
func Keep(dir string, p mcp.Place, name string, change Values) error {
	mu.Lock()
	defer mu.Unlock()
	values, err := Read(dir, p, name)
	if err != nil {
		return err
	}
	for key, value := range change {
		if key != First && key != Restart {
			return fmt.Errorf("unknown session message %q", key)
		}
		if value = strings.TrimSpace(value); value == "" {
			delete(values, key)
		} else {
			values[key] = value
		}
	}
	return write(dir, p, name, values)
}

// Move carries the messages from a session's old name to its new one when
// the panel renames it. A session with no overrides moves nothing.
func Move(dir string, p mcp.Place, from, to string) error {
	mu.Lock()
	defer mu.Unlock()
	values, err := Read(dir, p, from)
	if err != nil || len(values) == 0 || from == to {
		return err
	}
	if err := write(dir, p, to, values); err != nil {
		return err
	}
	return write(dir, p, from, nil)
}

func write(dir string, p mcp.Place, name string, values Values) error {
	where, _ := p.Clean()
	path := filepath.Join(dir, role.File(where, name))
	if len(values) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	body, err := json.Marshal(record{where.ConfigDir, role.Repo(where.Dir), name, values})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".intent-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
