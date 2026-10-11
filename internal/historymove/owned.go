package historymove

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func owned(config, slug string) ([]string, error) {
	root := filepath.Join("projects", slug)
	entries, err := os.ReadDir(filepath.Join(config, root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	roots := []string{root}
	ids := map[string]bool{}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if sessionID.MatchString(id) {
			ids[id] = true
		}
	}
	// These directories carry only entries keyed by conversation. Word-named
	// plans and shared prompt/paste history have no such ownership and stay put.
	for _, sub := range []string{"file-history", "session-env", "tasks", "todos", "plans", "teams", "session-models"} {
		entries, err := os.ReadDir(filepath.Join(config, sub))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			for id := range ids {
				if name == id || name == "session-"+id[:8] || strings.HasPrefix(name, id+".") || strings.HasPrefix(name, id+"-") {
					roots = append(roots, filepath.Join(sub, name))
					break
				}
			}
		}
	}
	slices.Sort(roots)
	return roots, nil
}
