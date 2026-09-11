package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeNotFoundNamesEveryPlaceItLooked(t *testing.T) {
	t.Setenv(claudeEnv, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	_, err := claudeBin("")
	if err == nil {
		t.Fatal("claude was found although it is in none of the three places")
	}
	said := err.Error()
	for _, want := range []string{"profile", claudeEnv, "PATH"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not name %q — nobody learns where it looked or what to fix: %s", want, said)
		}
	}
	if !strings.Contains(said, "machine description") {
		t.Errorf("the refusal does not say where to write the path — a diagnosis without a cure: %s", said)
	}
}

func machineClaude(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "machine-claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(claudeEnv, path)
	return path
}

func TestClaudeFromMachineMustExist(t *testing.T) {
	bin := pathClaude(t)
	missing := filepath.Join(t.TempDir(), "no-such-file")
	t.Setenv(claudeEnv, missing)

	choice, err := claudeBin("")
	if err == nil {
		t.Fatalf("a claude from the machine description that does not exist was silently replaced with %s", choice.Path)
	}
	if strings.Contains(err.Error(), bin) {
		t.Errorf("the refusal talks about the claude from PATH, which is not the one that was asked for: %v", err)
	}
	for _, want := range []string{missing, claudeEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, and that is the whole explanation: %v", want, err)
		}
	}
}

func TestClaudeIsNotLookedForInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(claudeEnv, "")
	t.Setenv("PATH", t.TempDir())

	wrapper := filepath.Join(home, ".claude-contours", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	choice, err := claudeBin("")
	if err == nil {
		t.Fatalf("the session would have gone through %s — a path nobody named", choice.Path)
	}
}
