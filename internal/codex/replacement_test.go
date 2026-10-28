package codex

import (
	"testing"

	"aacpanel/internal/codex/codextest"
)

func TestADeferredCloseSurvivesTheLinkAndLeavesTheCallingTurnAlone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := codextest.New(t)
	srv.Lag()
	first := NewLink(srv.Home, "acme")
	stop := running(t, first)
	ready(t, first)
	id, err := first.Start(t.Context(), Begin{CWD: "/srv/proj", Hold: true, Message: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.CloseAfterTurn(id); err != nil {
		t.Fatal(err)
	}
	c, err := first.client()
	if err != nil {
		t.Fatal(err)
	}
	if first.finishClose(t.Context(), c, id) || !srv.Subscribed(id) {
		t.Fatal("an idle status hid the calling turn and closed it")
	}
	first.enqueue(id, Queued{Message: Message{ID: "queued", Text: "do not start another turn"}})
	stop()
	next := NewLink(srv.Home, "acme")
	running(t, next)
	ready(t, next)
	until(t, "the replacement link to hold the old thread", func() bool { return srv.Subscribed(id) })
	if !next.Closing(id) {
		t.Fatal("the executor lost the pending close")
	}
	srv.Complete(id, "completed", "")
	until(t, "the old thread to be released", func() bool { return !srv.Subscribed(id) && !next.Closing(id) })
	if len(srv.Calls("turn/interrupt")) != 0 || len(srv.Calls("turn/start")) != 1 || len(srv.Calls("turn/steer")) != 0 {
		t.Fatal("a deferred close interrupted the caller or delivered its queued message")
	}
	if _, err := next.Resume(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if next.Closing(id) {
		t.Fatal("a resumed thread inherited the pending close")
	}
}

func TestARestartTakesTheSandboxFromLiveSettings(t *testing.T) {
	_, l, id := started(t, Begin{Sandbox: "workspace-write"})
	for _, tc := range []struct {
		mode, sandbox string
	}{{"", "workspace-write"}, {ModeReadOnly, "read-only"}, {ModeAsk, "workspace-write"}} {
		if tc.mode != "" {
			if _, err := l.Configure(t.Context(), id, Setting{Mode: tc.mode}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := l.RestartSettings(id)
		if err != nil || got.Sandbox != tc.sandbox {
			t.Fatalf("mode %s restarts with sandbox %s, wanted %s: %v", tc.mode, got.Sandbox, tc.sandbox, err)
		}
	}
}
