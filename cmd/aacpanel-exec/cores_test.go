package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/hostcfg"
)

// Validation uses a made-up machine, so the fallback is checked even on a
// host that happens to have every core a carried description names.
func TestUnusableCoresLeavePlacementToTheSystem(t *testing.T) {
	var journal bytes.Buffer
	was := log.Writer()
	log.SetOutput(&journal)
	t.Cleanup(func() { log.SetOutput(was) })
	for _, c := range []struct{ raw, want, reason string }{
		{"8-x", "", "not a list of cores systemd takes"},
		{"8-31", "", "names cores 24-31 the machine does not have"},
		{"8-11 16-23", "8-11,16-23", ""},
		{"", "", ""},
	} {
		journal.Reset()
		got := executionCores(hostcfg.ValidateCores(c.raw, []byte("0-23\n")))
		if got != c.want {
			t.Errorf("%q starts with affinity %q, want %q", c.raw, got, c.want)
		}
		if c.reason == "" {
			if journal.Len() != 0 {
				t.Errorf("valid cores %q warned: %s", c.raw, journal.String())
			}
			continue
		}
		for _, want := range []string{hostcfg.CoresEnv, c.reason, "running without CPU affinity"} {
			if !strings.Contains(journal.String(), want) {
				t.Errorf("%q does not log %q: %s", c.raw, want, journal.String())
			}
		}
		if strings.Count(journal.String(), "\n") != 1 {
			t.Errorf("%q logs more than one warning: %q", c.raw, journal.String())
		}
	}
}

// A socket under a file stops startup before it can touch any host units.
// Reaching it proves a bad description did not refuse the executor's start.
func TestTheExecutorPassesUnusableCoresAtItsStart(t *testing.T) {
	var journal bytes.Buffer
	was := log.Writer()
	log.SetOutput(&journal)
	t.Cleanup(func() { log.SetOutput(was) })
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"8-x", "8-31"} {
		journal.Reset()
		err := run(filepath.Join(blocker, "sock"), "unix:///nonexistent.sock", defaultSelf, func() (string, error) {
			return hostcfg.ValidateCores(raw, []byte("0-23\n"))
		})
		if err == nil || !strings.Contains(err.Error(), blocker) || strings.Contains(err.Error(), hostcfg.CoresEnv) {
			t.Fatalf("%s: the executor did not reach its socket after discarding affinity: %v", raw, err)
		}
		if !strings.Contains(journal.String(), hostcfg.CoresEnv+"="+raw) || !strings.Contains(journal.String(), "running without CPU affinity") {
			t.Errorf("%s: startup did not explain its fallback: %s", raw, journal.String())
		}
	}
}
