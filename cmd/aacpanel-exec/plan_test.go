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

// fakeClaude puts a live claude process into a fake /proc, with the file it
// keeps of itself in its config directory: a session in /srv/proj/lab.
func fakeClaude(t *testing.T, proc, config string, pid int, start, id string) {
	t.Helper()
	dir := filepath.Join(proc, fmt.Sprint(pid))
	fields := append([]string{"S", "1"}, strings.Split(strings.Repeat("0 ", 17), " ")[:17]...)
	for name, body := range map[string]string{
		"stat":    fmt.Sprintf("%d (claude) %s %s\n", pid, strings.Join(fields, " "), start),
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
	session := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":"/srv/proj/lab","procStart":%q,"kind":"interactive"}`, pid, id, start)
	if err := os.WriteFile(filepath.Join(config, "sessions", fmt.Sprintf("%d.json", pid)), []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}
}

// talk runs the plan mode for one claude over the lines given and returns
// its replies.
func talk(t *testing.T, parent int, lines ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if code := runPlan(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, parent); code != 0 {
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
	return replies
}

const handshake = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude-code","version":"2.1.283"}}}`

// The plan mode as a session runs it: claude starts the executor with -plan,
// shakes hands and calls the tool, and the plan lands under the place of the
// claude that is the server's parent — the config directory it keeps the
// file of itself in and the directory that file names. A session started
// again in the place, another process in another conversation, is told of
// the plan at its handshake.
func TestThePlanModeKeepsThePlanOfItsParentsPlace(t *testing.T) {
	const (
		first     = "9e3f0a4b-5c6d-4e7f-8a9b-0c1d2e3f4a5b"
		restarted = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	)
	proc, config, state := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("AACP_PROC", proc)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("AACP_CLAUDE_HOME", filepath.Join(t.TempDir(), "no-contour"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	fakeClaude(t, proc, config, 4242, "5555", first)

	replies := talk(t, 4242, handshake,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"plan","arguments":{"items":[{"text":"read the code","status":"done"},{"text":"write the tests","status":"active"}]}}}`,
	)
	if len(replies) != 2 {
		t.Fatalf("meant two replies, one to the handshake and one to the call: %v", replies)
	}
	if res, _ := replies[0]["result"].(map[string]any); res == nil || res["instructions"] != plan.Instructions {
		t.Errorf("a place with no plan was told of one: %v", replies[0])
	}
	if res, _ := replies[1]["result"].(map[string]any); res == nil || res["isError"] != nil {
		t.Fatalf("the call answered %v", replies[1])
	}

	lab := plan.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	got := plan.Read(filepath.Join(state, "aacpanel", "plans"), lab)
	if got == nil || got.PID != 4242 || got.SessionID != first || len(got.Items) != 2 || got.Items[1].Status != plan.Active {
		t.Fatalf("the plan on disk is %+v", got)
	}

	fakeClaude(t, proc, config, 5151, "7777", restarted)
	replies = talk(t, 5151, handshake)
	res, _ := replies[0]["result"].(map[string]any)
	said, _ := res["instructions"].(string)
	if !strings.Contains(said, "1 of 2 steps finished, the current step: “write the tests”") {
		t.Errorf("the session started again was not told of the plan of its place: %q", said)
	}
}
