package contours

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonalNameHasOneOwner(t *testing.T) {
	const owner = "internal/contours/contours.go"

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const literal = `"personal"`

	var found []string
	seen := false
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// The working directory of an agent is not the project: it holds
			// scratch copies of this very tree, and every rule below would
			// then be broken by a copy of the file that keeps it.
			case ".git", ".claude", "node_modules", "dist", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(raw), literal) {
			return nil
		}
		if rel == owner {
			seen = true
			return nil
		}
		found = append(found, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	if !seen {
		t.Fatalf("%s does not name %s — the constant was moved, and the test guards an empty place", owner, literal)
	}
	for _, path := range found {
		t.Errorf("%s writes %s as a literal of its own — the name of the personal contour is decided by contours.Personal, "+
			"otherwise the sides part silently", path, literal)
	}
}
