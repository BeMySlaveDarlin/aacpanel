package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

func TestFreeNameReservesLiveCodexTitlesAndTmuxNames(t *testing.T) {
	fakeProc(t, fproc{pid: 200, comm: "claude", args: []string{"claude", "-n", "shop"}})
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	tmux := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(tmux, []byte("#!/bin/sh\n[ \"$1\" = list-sessions ] && printf 'shop-3\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, tmux)
	const thread = "019a2000-0000-7000-8000-000000000001"
	state := codex.State{Summary: stream.Summary{Name: codex.SessionName(thread), SessionID: thread, Holder: os.Getpid()},
		Agent: codex.Agent, Title: "shop-2"}
	put := func() {
		t.Helper()
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(stream.Dir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stream.StatePath(thread), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put()
	if name, err := FreeName("shop"); err != nil || name != "shop-4" {
		t.Fatalf("name = %q, %v; live names must be reserved across agents", name, err)
	}
	if takenNames(thread)["shop-2"] {
		t.Fatal("a Codex terminal reserves its own thread's title")
	}
	state.Holder = 0
	put()
	if name, err := FreeName("shop"); err != nil || name != "shop-2" {
		t.Fatalf("a stale Codex summary reserved its title: %q, %v", name, err)
	}
}
