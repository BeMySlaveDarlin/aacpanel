package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func selectedRelease(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, "current"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return path, err
}

// The previous selection must survive a busy daemon and a later timer run:
// current already points at the new package when that restart can happen.
func rememberRelease(root, previous string) error {
	current, err := selectedRelease(root)
	if err != nil || previous == "" || previous == current {
		return err
	}
	return restoreCurrent(filepath.Join(root, "previous"), previous)
}

func pruneReleases(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	current, err := selectedRelease(root)
	if err != nil || current == "" {
		return err
	}
	dir := filepath.Join(root, "releases")
	if filepath.Dir(current) != dir {
		return fmt.Errorf("the selected Codex release is outside %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	previous, err := filepath.EvalSymlinks(filepath.Join(root, "previous"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if previous == current || filepath.Dir(previous) != dir {
		previous = ""
	}
	// An installation handed over without our previous link keeps its newest
	// other package once. Subsequent updates keep the actual prior selection.
	if previous == "" {
		var newest int64
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if !entry.IsDir() || path == current {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if previous == "" || info.ModTime().UnixNano() > newest {
				previous, newest = path, info.ModTime().UnixNano()
			}
		}
		if err := rememberRelease(root, previous); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() && path != current && path != previous {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}
