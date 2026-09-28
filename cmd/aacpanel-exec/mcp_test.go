package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
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

// talk runs the MCP server for one claude over the lines given and returns
// its replies.
func talk(t *testing.T, parent int, lines ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if code := runMCP(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, parent); code != 0 {
		t.Fatalf("the MCP server ended with %d", code)
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

// noChecklist is how the server's word to a session in a place with no
// checklist begins, to the byte: the lead and the checklist's line stand in the system prompt
// of every session the panel starts, and the lines of the other tools follow.
const noChecklist = "The panel is how the person follows this session from their phone and desk; " +
	"its tools reach them there, and the terminal does not show what they do.\n" +
	"When the work has several steps, keep it with the checklist tool, which the person sees " +
	"in the panel: the whole list every time, updated when a step starts or ends and when the checklist changes. " +
	"A short task needs no checklist."

// Both flags start the one server: the launcher writes -mcp into the MCP
// configuration of a session, and a live session whose configuration names
// -plan starts the same server when it reconnects to it.
func TestThePlanAndMCPFlagsStartOneServer(t *testing.T) {
	if mcp.Flag != "-mcp" {
		t.Errorf("the launcher writes %s", mcp.Flag)
	}
	for _, args := range [][]string{{"-mcp"}, {"-plan"}, {"-plan", "-mcp"}, {}} {
		set := flag.NewFlagSet("aacpanel-exec", flag.ContinueOnError)
		on := mcpFlags(set)
		if err := set.Parse(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if *on != (len(args) > 0) {
			t.Errorf("%v start the server: %v", args, *on)
		}
	}
}

// The server as a session runs it: claude starts the executor with the flag
// of its configuration, shakes hands and calls the checklist tool, and the
// checklist lands under the place of the claude that is the server's parent —
// the config directory it keeps the file of itself in and the directory that
// file names. A session started again in the place, another process in
// another conversation, is told of the checklist at its handshake.
func TestTheServerKeepsTheChecklistOfItsParentsPlace(t *testing.T) {
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
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"checklist","arguments":{"items":[{"text":"read the code","status":"done"},{"text":"write the tests","status":"active"}]}}}`,
	)
	if len(replies) != 2 {
		t.Fatalf("meant two replies, one to the handshake and one to the call: %v", replies)
	}
	hello, _ := replies[0]["result"].(map[string]any)
	told, _ := hello["instructions"].(string)
	if !strings.HasPrefix(told, noChecklist+"\n") || strings.Contains(told, "already has a checklist") {
		t.Errorf("a place with no checklist was told otherwise: %v", replies[0])
	}
	if res, _ := replies[1]["result"].(map[string]any); res == nil || res["isError"] != nil {
		t.Fatalf("the call answered %v", replies[1])
	}

	lab := mcp.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	got := checklist.Read(filepath.Join(state, "aacpanel", "checklists"), lab)
	if got == nil || got.PID != 4242 || got.SessionID != first || len(got.Items) != 2 || got.Items[1].Status != checklist.Active {
		t.Fatalf("the checklist on disk is %+v", got)
	}

	fakeClaude(t, proc, config, 5151, "7777", restarted)
	replies = talk(t, 5151, handshake)
	res, _ := replies[0]["result"].(map[string]any)
	said, _ := res["instructions"].(string)
	if !strings.HasPrefix(said, noChecklist+" This place already has a checklist") ||
		!strings.Contains(said, "1 of 2 steps finished, the current step: “write the tests”") {
		t.Errorf("the session started again was not told of the checklist of its place: %q", said)
	}
}
