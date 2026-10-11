package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/hostcfg"
)

// An executor whose description names cores systemd would not hold a unit to
// does not start, and says why: the value is refused where it is set, not at
// every session, terminal and observer started on it. The socket lies under a
// file, so an executor that took the value would stop at its socket — before
// it touched a unit of the machine — and say something else.
func TestTheExecutorRefusesCoresAtItsStart(t *testing.T) {
	t.Setenv(hostcfg.PathEnv, filepath.Join(t.TempDir(), "no-description.env"))
	t.Setenv(hostcfg.CoresEnv, "8-x")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(filepath.Join(blocker, "sock"), "unix:///nonexistent.sock", defaultSelf)
	if err == nil || !strings.Contains(err.Error(), hostcfg.CoresEnv+"=8-x") {
		t.Fatalf("the executor started on cores no unit could run on, or refused them without naming them: %v", err)
	}
}
