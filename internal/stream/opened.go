package stream

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SessionOwner records who opened a helper. A name alone would give a later,
// unrelated session of that name its reports and the right to close it.
type SessionOwner struct {
	Name         string `json:"name"`
	Conversation string `json:"conversation"`
}

// OpenedPath is separate from ParentPath: standing a session under another
// on the screen grants neither reports nor the right to close it.
func OpenedPath(id string) string {
	return filepath.Join(keptDir(), "opened", id+".json")
}

func KeepOwner(id string, owner SessionOwner) error {
	body, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	return Keep(OpenedPath(id), body)
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
