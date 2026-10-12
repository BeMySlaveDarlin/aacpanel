package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"aacpanel/internal/browserlink"
)

// Rooted access refuses profile symlinks as well as traversal. IDs never come
// from display names, and Chrome sees only the data directory of its own ID.
func profileRoot(c Config) (*os.Root, error) {
	if err := os.MkdirAll(c.Root, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(c.Root)
}

func profileDir(root *os.Root, id string) error {
	if !browserlink.ID(id) {
		return errors.New("invalid browser profile id")
	}
	st, err := root.Lstat(id)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("the browser profile is not an owned directory")
	}
	return nil
}

// Create records a profile under an opaque id. Its display name is metadata
// only, so a name cannot select another directory on the host.
func Create(c Config, id, name string) error {
	if err := c.Check(); err != nil {
		return err
	}
	if !browserlink.ID(id) {
		return errors.New("invalid browser profile id")
	}
	if err := browserlink.Name(name); err != nil {
		return err
	}
	root, err := profileRoot(c)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Mkdir(id, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(browserlink.Profile{ID: id, Name: name})
	if err != nil {
		return err
	}
	if err = root.WriteFile(filepath.Join(id, "profile.json"), raw, 0o600); err != nil {
		root.RemoveAll(id)
		return err
	}
	return nil
}

// Profiles lists owned profiles without starting Chrome or reading its data.
func Profiles(c Config) ([]browserlink.Profile, error) {
	out := []browserlink.Profile{}
	if !c.Enabled {
		return out, nil
	}
	entries, err := os.ReadDir(c.Root)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(c.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, e := range entries {
		if !browserlink.ID(e.Name()) {
			continue
		}
		if err = profileDir(root, e.Name()); err != nil {
			return nil, err
		}
		raw, err := root.ReadFile(filepath.Join(e.Name(), "profile.json"))
		if err != nil {
			return nil, err
		}
		var p browserlink.Profile
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.ID != e.Name() {
			return nil, errors.New("a browser profile has the wrong id")
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Delete removes only the named profile. The caller must stop its holder
// first, so Chrome cannot recreate cookies during deletion.
func Delete(c Config, id string) error {
	if err := c.Check(); err != nil {
		return err
	}
	root, err := profileRoot(c)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = profileDir(root, id); err != nil {
		return err
	}
	return root.RemoveAll(id)
}

// DataDir prepares the owned directories Chrome needs and refuses symlinks
// before handing their paths to a process outside the rooted file API.
func DataDir(c Config, id string) (string, error) {
	root, err := profileRoot(c)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = profileDir(root, id); err != nil {
		return "", err
	}
	for _, name := range []string{"data", "home", "cache", "tmp"} {
		path := filepath.Join(id, name)
		if err = root.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		st, err := root.Lstat(path)
		if err != nil {
			return "", err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("browser %s is not a directory", name)
		}
	}
	return filepath.Join(c.Root, id), nil
}

// Socket names a holder endpoint outside the service-mounted runtime tree.
func Socket(c Config, id string) string { return filepath.Join(c.Runtime, id+".sock") }
