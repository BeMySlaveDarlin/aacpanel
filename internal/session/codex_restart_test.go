package session

import (
	"net/http"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func TestACodexRestartProvesItsThreadAndStartsFresh(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"replacement started"}`)
	tool := CodexRestart(hostWith(t, "", url))
	props := tool.InputSchema["properties"].(map[string]any)
	if !tool.Allowed || props["resume"] != nil || props["continue"] != nil || props["clean"] == nil {
		t.Fatalf("Codex restart has the wrong schema or approval: %+v", tool)
	}
	for _, bind := range []mcp.Bind{lost, bound("")} {
		if said, failed := call(t, tool, bind, map[string]any{}); !failed {
			t.Fatalf("an unproven thread restarted: %s", said)
		}
	}
	for _, args := range []map[string]any{
		{"resume": true}, {"continue": true}, {"clean": true, "intent": "go"},
	} {
		if said, failed := call(t, tool, bound(mine), args); !failed {
			t.Fatalf("invalid arguments restarted the thread: %v: %s", args, said)
		}
	}
	if len(p.calls()) != 0 {
		t.Fatal("a refused restart reached the panel")
	}
	for _, args := range []map[string]any{{"intent": "Read the handoff"}, {"clean": true}} {
		said, failed := call(t, tool, bound(mine), args)
		if failed || !strings.Contains(said, "End the turn") {
			t.Fatalf("restart: %s, failed %v", said, failed)
		}
	}
	for i, got := range p.calls() {
		params := got["params"].(map[string]any)
		if got["kind"] != "session.restart" || got["target"] != "" || params["conversation"] != mine ||
			params["resume"] != nil || params["clean"] != (i == 1) {
			t.Fatalf("the restart lost its caller or options: %v", got)
		}
		if i == 0 && params["intent"] != "Read the handoff" {
			t.Fatalf("the restart lost its first message: %v", got)
		}
	}
}
