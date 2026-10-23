package session

import (
	"net/http"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

func TestReadyReportsItsConversationWithoutRestarting(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true}`)
	h := hostWith(t, "", url)
	if !Ready(h).Allowed {
		t.Fatal("readiness waits for the person")
	}
	for _, intent := range []string{"", "Read the handoff", strings.Repeat("x", action.IntentMax)} {
		said, failed := call(t, Ready(h), bound(mine), map[string]any{"intent": intent})
		if failed || !strings.Contains(said, "End the turn") {
			t.Fatalf("ready: %s", said)
		}
	}
	for _, req := range p.calls() {
		params := req["params"].(map[string]any)
		if req["kind"] != "session.ready" || params["conversation"] != mine || params["resume"] != nil {
			t.Fatalf("readiness restarted or lost its conversation: %v", req)
		}
	}
	for _, intent := range []string{" ", " /clear", "-p", strings.Repeat("x", action.IntentMax+1), "bad\x00text"} {
		if said, failed := call(t, Ready(h), bound(mine), map[string]any{"intent": intent}); !failed {
			t.Fatalf("accepted %q: %s", intent, said)
		}
	}
	if _, failed := call(t, Ready(h), bound(""), map[string]any{}); !failed {
		t.Fatal("accepted no conversation")
	}
	if _, failed := call(t, Ready(h), bound(mine), map[string]any{"continue": true}); !failed {
		t.Fatal("accepted continue")
	}
	if len(p.calls()) != 3 {
		t.Fatal("bad readiness reached the panel")
	}
}

func TestReadyWithoutACycleExplainsTheAskedForRestart(t *testing.T) {
	_, url := startPanel(t, http.StatusConflict, `{"error":"The panel has not asked; use session_restart for a restart the person asked for."}`)
	said, failed := call(t, Ready(hostWith(t, "", url)), bound(mine), map[string]any{})
	if !failed || !strings.Contains(said, "session_restart") {
		t.Fatalf("unasked readiness: %s", said)
	}
}
