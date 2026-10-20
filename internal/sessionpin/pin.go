// Package sessionpin keeps a session group's place in the list across devices
// and conversations. Its key is the role's: account, repository and name.
package sessionpin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
	"aacpanel/internal/sessionfile"
)

// Stamp has a fixed width so the browser can compare pins in their order of
// creation without losing the fractions of a second.
const Stamp = "2006-01-02T15:04:05.000000000Z"

type Pin struct {
	ConfigDir string `json:"configDir"`
	Repo      string `json:"repo"`
	Name      string `json:"name"`
	At        string `json:"at"`
}

func Dir() string { return sessionfile.Dir("pins") }

// Read returns the pin of this account, repository and name, if it has one.
func Read(dir string, place mcp.Place, name string) (*Pin, error) {
	p, ok := place.Clean()
	if !ok || name == "" {
		return nil, errors.New("the account, directory and name of the session must be known to pin it")
	}
	raw, err := os.ReadFile(filepath.Join(dir, role.File(p, name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pin Pin
	if err := json.Unmarshal(raw, &pin); err != nil {
		return nil, err
	}
	if pin.ConfigDir != p.ConfigDir || pin.Repo != role.Repo(p.Dir) || pin.Name != name {
		return nil, errors.New("the pin belongs to another session")
	}
	if _, err := time.Parse(Stamp, pin.At); err != nil {
		return nil, fmt.Errorf("the pin has no time: %w", err)
	}
	return &pin, nil
}

// A repeated press from another device keeps the first pin's place. Reading
// and writing under one lock keeps concurrent requests from moving it.
var writes sync.Mutex

func Keep(dir string, place mcp.Place, name string, on bool, now time.Time) error {
	writes.Lock()
	defer writes.Unlock()
	p, ok := place.Clean()
	if !ok || name == "" {
		return errors.New("the account, directory and name of the session must be known to pin it")
	}
	path := filepath.Join(dir, role.File(p, name))
	if !on {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	kept, err := Read(dir, p, name)
	if err != nil || kept != nil {
		return err
	}
	body, err := json.Marshal(Pin{ConfigDir: p.ConfigDir, Repo: role.Repo(p.Dir), Name: name,
		At: now.UTC().Format(Stamp)})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pin-*")
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
