package stream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SessionOwner records who opened a helper. A name alone would give a later,
// unrelated session of that name its reports and the right to close it.
type SessionOwner struct {
	Name         string `json:"name"`
	Conversation string `json:"conversation"`
}

// OpenedSession keeps the child's identity with its opening record so a
// coordinator can still name it after it leaves the live snapshot.
type OpenedSession struct {
	SessionOwner
	Key       string `json:"-"`
	Session   string `json:"session,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Profile   string `json:"profile,omitempty"`
}

// OpenedPath is separate from ParentPath: standing a session under another
// on the screen grants neither reports nor the right to close it.
func OpenedPath(id string) string {
	return filepath.Join(keptDir(), "opened", id+".json")
}

func KeepOwner(id string, owner SessionOwner) error {
	record := openedSession(id)
	record.SessionOwner = owner
	return KeepOpened(id, record)
}

// KeepOpened writes identity into the same record that grants child closes;
// no status is kept here, since only the live snapshot knows that.
func KeepOpened(id string, record OpenedSession) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return Keep(OpenedPath(id), body)
}

func openedSession(id string) OpenedSession {
	var record OpenedSession
	body, err := os.ReadFile(OpenedPath(id))
	if err == nil {
		_ = json.Unmarshal(body, &record)
	}
	record.Key = id
	return record
}

// OpenedSessions reads the existing ownership records, including children
// whose process or thread no longer lives.
func OpenedSessions() ([]OpenedSession, error) {
	entries, err := os.ReadDir(filepath.Dir(OpenedPath("")))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []OpenedSession
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() {
			continue
		}
		if record := openedSession(id); record.Conversation != "" {
			out = append(out, record)
		}
	}
	return out, nil
}

func OwnerOf(id string) SessionOwner {
	body, err := os.ReadFile(OpenedPath(id))
	if err != nil {
		return SessionOwner{}
	}
	var owner SessionOwner
	if json.Unmarshal(body, &owner) != nil {
		return SessionOwner{}
	}
	return owner
}

// PassOwners hands every session the conversation from opened to owner. A
// session restarted anew runs a new conversation, and the sessions it opened
// before stay its children: the right to close them and their reports go on
// with it rather than with a conversation that no longer runs.
func PassOwners(from string, owner SessionOwner) error {
	if from == "" || owner.Conversation == "" || owner.Conversation == from {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(keptDir(), "opened", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		if OwnerOf(id).Conversation != from {
			continue
		}
		if err := KeepOwner(id, owner); err != nil {
			return err
		}
	}
	return nil
}
