package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
)

// fakeClaude puts a live claude process into a fake /proc, with the file it
// keeps of itself in its config directory: a session named name in
// /srv/proj/lab.
func fakeClaude(t *testing.T, proc, config string, pid int, start, id, name string) {
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
	session := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":"/srv/proj/lab","procStart":%q,"kind":"interactive","name":%q}`,
		pid, id, start, name)
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
// checklist begins, to the byte: the lead and the checklist's line stand in the
// system prompt of every session the panel starts, and the lines of the other
// tools follow.
const noChecklist = "The person follows this session in the panel, on their phone and desk: " +
	"these tools reach them there, and the terminal does not show what they do.\n" +
	"Keep work of several steps with the checklist tool, which the person sees " +
	"in the panel: the whole list, sent before the work, when a step starts or ends and when the plan " +
	"changes. A short task needs none."

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
// checklist lands under the session of the claude that is the server's parent
// — the config directory it keeps the file of itself in, and the directory
// and the name that file names. The session started again under its name,
// another process in another conversation, is told of the checklist at its
// handshake; another session of the directory is told of none.
func TestTheServerKeepsTheChecklistOfItsParentsSession(t *testing.T) {
	const (
		first     = "9e3f0a4b-5c6d-4e7f-8a9b-0c1d2e3f4a5b"
		restarted = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
		beside    = "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"
	)
	proc, config, state := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("AACP_PROC", proc)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("AACP_CLAUDE_HOME", filepath.Join(t.TempDir(), "no-contour"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	fakeClaude(t, proc, config, 4242, "5555", first, "lab")

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
	got := checklist.Read(filepath.Join(state, "aacpanel", "checklists"), lab, "lab")
	if got == nil || got.PID != 4242 || got.SessionID != first || len(got.Items) != 2 || got.Items[1].Status != checklist.Active {
		t.Fatalf("the checklist on disk is %+v", got)
	}

	fakeClaude(t, proc, config, 6262, "8888", beside, "lab-review")
	replies = talk(t, 6262, handshake)
	res, _ := replies[0]["result"].(map[string]any)
	said, _ := res["instructions"].(string)
	if !strings.HasPrefix(said, noChecklist+"\n") || strings.Contains(said, "already has a checklist") {
		t.Errorf("another session of the directory was told of the checklist of this one: %q", said)
	}

	fakeClaude(t, proc, config, 5151, "7777", restarted, "lab")
	replies = talk(t, 5151, handshake)
	res, _ = replies[0]["result"].(map[string]any)
	said, _ = res["instructions"].(string)
	if !strings.HasPrefix(said, noChecklist+" This session already has a checklist") ||
		!strings.Contains(said, "1 of 2 steps finished, the current step: “write the tests”") {
		t.Errorf("the session started again under its name was not told of its checklist: %q", said)
	}
}

// fakeCodex puts a live codex into a fake /proc: a daemon holding the locks
// of the threads given, under home.
func fakeCodex(t *testing.T, proc, home string, pid int, threads ...string) {
	t.Helper()
	dir := filepath.Join(proc, fmt.Sprint(pid))
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("codex\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, thread := range threads {
		lock := filepath.Join(home, "thread-writer-locks", thread+".lock")
		if err := os.Symlink(lock, filepath.Join(dir, "fd", fmt.Sprint(20+i))); err != nil {
			t.Fatal(err)
		}
	}
}

// The server under codex serves one codex thread a call: the one the call
// names in its _meta, held by the codex that started the server. It offers the
// tools of a thread — the restart and the new session are not among them — a
// letter goes from that thread, with the home of its codex and the directory
// codex started the server in, and a checklist is kept as the thread's; a
// call that names no thread, or one the codex does not hold, sends nothing and
// keeps nothing.
func TestTheServerUnderCodexWritesFromTheThreadTheCallNames(t *testing.T) {
	const (
		first  = "019a1f00-0000-7000-8000-00000000aaaa"
		second = "019a1f00-0000-7000-8000-00000000bbbb"
		stray  = "019a1f00-0000-7000-8000-00000000cccc"
	)
	proc, home, dir, state := t.TempDir(), filepath.Join(t.TempDir(), ".codex"), t.TempDir(), t.TempDir()
	t.Setenv("AACP_PROC", proc)
	t.Setenv("XDG_STATE_HOME", state)
	t.Chdir(dir)
	fakeCodex(t, proc, home, 800, first, second)
	var asked []map[string]any
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		asked = append(asked, body)
		_, _ = w.Write([]byte(`{"ok":true,"detail":"a letter from codex-0000bbbb to lab"}`))
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)

	steps := `{"items":[{"text":"read the code","status":"done"},{"text":"write the tests","status":"active"}]}`
	replies := talk(t, 800, handshake,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"send_to_session","arguments":{"to":"lab","text":"done"},`+
			`"_meta":{"threadId":"`+second+`","callId":"call_1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"send_to_session","arguments":{"to":"lab","text":"done"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"checklist","arguments":`+steps+`,`+
			`"_meta":{"threadId":"`+second+`","callId":"call_2"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"checklist","arguments":`+steps+`}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"checklist","arguments":`+steps+`,`+
			`"_meta":{"threadId":"`+stray+`"}}}`,
	)
	if len(replies) != 7 {
		t.Fatalf("meant seven replies: %v", replies)
	}
	list, _ := replies[1]["result"].(map[string]any)
	tools, _ := list["tools"].([]any)
	var offered []string
	for _, tool := range tools {
		offered = append(offered, tool.(map[string]any)["name"].(string))
	}
	if want := []string{"checklist", "brief_publish", "brief_delete", "notify", "secret_ask", "send_to_session"}; !slices.Equal(offered, want) {
		t.Errorf("codex was offered %v", offered)
	}
	if res, _ := replies[2]["result"].(map[string]any); res == nil || res["isError"] != nil {
		t.Fatalf("the letter answered %v", replies[2])
	}
	for _, at := range []int{3, 5, 6} {
		if res, _ := replies[at]["result"].(map[string]any); res == nil || res["isError"] != true {
			t.Errorf("a call of no thread the codex holds answered %v", replies[at])
		}
	}
	if res, _ := replies[4]["result"].(map[string]any); res == nil || res["isError"] != nil {
		t.Fatalf("the checklist answered %v", replies[4])
	}
	kept := filepath.Join(state, "aacpanel", "checklists")
	got := checklist.ReadThread(kept, second)
	if got == nil || got.PID != 800 || got.ConfigDir != home || got.Dir != dir || got.Name != "codex-0000bbbb" ||
		len(got.Items) != 2 || got.Items[1].Status != checklist.Active {
		t.Fatalf("the checklist of the thread is %+v", got)
	}
	if entries, _ := os.ReadDir(kept); len(entries) != 1 {
		t.Errorf("the calls kept %v", entries)
	}
	if len(asked) != 1 {
		t.Fatalf("the panel was asked %v", asked)
	}
	params, _ := asked[0]["params"].(map[string]any)
	sender, _ := params["fromCodex"].(map[string]any)
	if params["from"] != second || sender["home"] != home || sender["dir"] != dir {
		t.Errorf("the letter went as %v", asked[0])
	}
}
