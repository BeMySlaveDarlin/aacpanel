package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/plan"
)

// The plan mode as a session runs it: claude starts the executor with -plan,
// shakes hands and calls the tool, and the plan lands under the conversation
// of the claude that is the server's parent — found in the file that process
// keeps of itself, in the config directory it was started with.
func TestThePlanModeKeepsThePlanOfItsParentsConversation(t *testing.T) {
	const (
		parent = 4242
		start  = "5555"
		id     = "9e3f0a4b-5c6d-4e7f-8a9b-0c1d2e3f4a5b"
	)
	proc, config, state := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("AACP_PROC", proc)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("AACP_CLAUDE_HOME", filepath.Join(t.TempDir(), "no-contour"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	dir := filepath.Join(proc, fmt.Sprint(parent))
	fields := append([]string{"S", "1"}, strings.Split(strings.Repeat("0 ", 17), " ")[:17]...)
	for name, body := range map[string]string{
		"stat":    fmt.Sprintf("%d (claude) %s %s\n", parent, strings.Join(fields, " "), start),
		"environ": "HOME=/home/u\x00CLAUDE_CONFIG_DIR=" + config + "\x00",
		"comm":    "claude\n",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(config, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	session := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":"/srv/proj/lab","procStart":%q,"kind":"interactive"}`, parent, id, start)
	if err := os.WriteFile(filepath.Join(config, "sessions", fmt.Sprintf("%d.json", parent)), []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude-code","version":"2.1.283"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"plan","arguments":{"items":[{"text":"read the code","status":"done"},{"text":"write the tests","status":"active"}]}}}`,
	}, "\n") + "\n"
	var out strings.Builder
	if code := runPlan(strings.NewReader(in), &out, parent); code != 0 {
		t.Fatalf("the plan mode ended with %d", code)
	}

	var replies []map[string]any
	scan := bufio.NewScanner(strings.NewReader(out.String()))
	for scan.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			t.Fatalf("the server wrote %q", scan.Text())
		}
		replies = append(replies, m)
	}
	if len(replies) != 2 {
		t.Fatalf("meant two replies, one to the handshake and one to the call: %v", replies)
	}
	if res, _ := replies[1]["result"].(map[string]any); res == nil || res["isError"] != nil {
		t.Fatalf("the call answered %v", replies[1])
	}

	got := plan.Read(filepath.Join(state, "aacpanel", "plans"), id)
	if got == nil || got.PID != parent || len(got.Items) != 2 || got.Items[1].Status != plan.Active {
		t.Errorf("the plan on disk is %+v", got)
	}
}
