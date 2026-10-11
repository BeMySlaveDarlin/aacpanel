package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexCleanupKeepsCurrentAndRollback(t *testing.T) {
	root := t.TempDir()
	for _, release := range []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0"} {
		putUpdateFile(t, filepath.Join(root, "releases", release, "bin", "codex"), release, 0o700)
	}
	if err := os.Symlink("releases/4.0.0", filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := rememberRelease(root, filepath.Join(root, "releases", "3.0.0")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := pruneReleases(root); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(root, "releases"))
		if err != nil || len(entries) != 2 || entries[0].Name() != "3.0.0" || entries[1].Name() != "4.0.0" {
			t.Fatalf("kept %v: %v", entries, err)
		}
		old, err := filepath.EvalSymlinks(filepath.Join(root, "previous"))
		if err != nil || old != filepath.Join(root, "releases", "3.0.0") {
			t.Fatalf("rollback points at %s: %v", old, err)
		}
	}
}
