package main

import (
	"os"
	"path/filepath"
	"testing"
)

// socketPath is a place for a unix socket that stays inside the length such a
// path may have. Not t.TempDir(): it names the directory after the test
// function together with the subtest, while sun_path holds 108 bytes in all —
// a long test name pushes the socket over that, and the listen fails with
// "invalid argument" on a name that looks perfectly ordinary.
func socketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aacp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}
