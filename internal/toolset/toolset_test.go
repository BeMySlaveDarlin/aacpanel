package toolset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("AACP_PANEL_URL", "http://127.0.0.1:1")
	os.Exit(m.Run())
}

// The server calls a tool by its name and takes the first of the name: a
// second tool of the same name would never be called, and claude would list
// both.
func TestEveryToolHasANameOfItsOwn(t *testing.T) {
	var names []string
	for _, tool := range tools() {
		if tool.Name == "" || slices.Contains(names, tool.Name) {
			t.Errorf("the tool %q is named twice or not at all among %v", tool.Name, names)
		}
		names = append(names, tool.Name)
	}
}

// What the launcher allows is what the tools mark allowed, under the names
// claude gives them and a permission rule knows them by: none of them waits
// on the person, and the letter to another session and the start of an
// observer, which runs a command on the host, are not among them.
func TestTheLauncherIsGivenTheAllowedTools(t *testing.T) {
	want := []string{
		"mcp__aacpanel__checklist",
		"mcp__aacpanel__role",
		"mcp__aacpanel__brief_publish",
		"mcp__aacpanel__brief_delete",
		"mcp__aacpanel__notify",
		"mcp__aacpanel__secret_ask",
		"mcp__aacpanel__request",
		"mcp__aacpanel__session_restart",
		"mcp__aacpanel__session_close",
		"mcp__aacpanel__observer_list",
		"mcp__aacpanel__observer_stop",
		"mcp__aacpanel__observer_tail",
		"mcp__aacpanel__alarm_set",
		"mcp__aacpanel__alarm_list",
		"mcp__aacpanel__alarm_cancel",
	}
	if got := Allowed(); !slices.Equal(got, want) {
		t.Errorf("allowed %v", got)
	}
}

// A thread of codex the panel starts calls without asking the tools a claude
// session is allowed, by their names on the server — its own close among them
// — and the letter asks there too.
func TestACodexThreadIsAllowedWhatAClaudeSessionIs(t *testing.T) {
	want := []string{"checklist", "brief_publish", "brief_delete", "notify", "secret_ask", "request", "session_close",
		"observer_list", "observer_stop", "observer_tail", "alarm_set", "alarm_list", "alarm_cancel"}
	if got := CodexAllowed(); !slices.Equal(got, want) {
		t.Errorf("codex runs %v without asking, meant %v", got, want)
	}
}

func TestRequestsOfBothAgentsCarryTheirBoundConversation(t *testing.T) {
	var conversations []string
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Conversation string `json:"conversation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		conversations = append(conversations, body.Conversation)
		json.NewEncoder(w).Encode(map[string]string{"id": strings.Repeat("a", 32)})
	}))
	defer panel.Close()
	t.Setenv("AACP_PANEL_URL", panel.URL)
	here := mcp.Binding{SessionID: "567f4d24-cd5f-48fa-bdc1-04c89d203494", Name: "work"}
	for _, test := range []struct {
		server     *mcp.Server
		meta, want string
	}{
		{Server(func() (mcp.Binding, error) { return here, nil }), "", here.SessionID},
		{CodexServer(codexCaller), `{"threadId":"` + codexThread + `"}`, codexThread},
	} {
		replies := exchange(t, test.server, toolCall(1, "request", `{"title":"Sign in","steps":"Open the browser","blocking":false}`, test.meta))
		result, _ := replies[1]["result"].(map[string]any)
		if result == nil || result["isError"] == true {
			t.Fatalf("request failed: %v", replies)
		}
		if len(conversations) == 0 || conversations[len(conversations)-1] != test.want {
			t.Fatalf("request caller: %v, want %s", conversations, test.want)
		}
	}
	before := len(conversations)
	replies := exchange(t, CodexServer(codexCaller), toolCall(1, "request", `{"title":"Sign in","steps":"Open the browser","blocking":false}`, ""))
	result, _ := replies[1]["result"].(map[string]any)
	if result == nil || result["isError"] != true || len(conversations) != before {
		t.Fatal("an unbound codex requested a task")
	}
}

// collectorSocket stands in for one socket of the collector, named to the
// tools by the variable that moves it: it answers every request with ok and
// hands the request over. The directory is short, since a unix socket's path
// holds 108 bytes.
func collectorSocket(t *testing.T, env string) <-chan map[string]any {
	t.Helper()
	dir, err := os.MkdirTemp("", "b")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ln.Close()
		os.RemoveAll(dir)
	})
	t.Setenv(env, ln.Addr().String())
	got := make(chan map[string]any, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			raw, _ := io.ReadAll(conn)
			var request map[string]any
			_ = json.Unmarshal(raw, &request)
			got <- request
			_, _ = conn.Write([]byte(`{"ok":true,"id":"a-brief"}`))
			conn.Close()
		}
	}()
	return got
}

// Every tool of the collector reaches the socket of its own kind: a call
// handed to the shelf of briefs would be refused there as a brief without a
// title, and the person would never be called.
func TestEveryToolOfTheCollectorReachesItsOwnSocket(t *testing.T) {
	briefs := collectorSocket(t, "AACP_BRIEF_SOCKET")
	calls := collectorSocket(t, "AACP_NOTIFY_SOCKET")
	bind := func() (mcp.Binding, error) {
		return mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.claude", Dir: "/srv/proj"},
			SessionID: "567f4d24-cd5f-48fa-bdc1-04c89d203494", PID: 4242}, nil
	}
	list := tools()
	for _, c := range []struct {
		tool, args string
		socket     <-chan map[string]any
	}{
		{"brief_publish", `{"doc":{"id":"a-brief","title":"A brief"}}`, briefs},
		{"brief_delete", `{"id":"a-brief"}`, briefs},
		{"notify", `{"text":"stuck on the migration"}`, calls},
		{"secret_ask", `{"name":"github-token","title":"GitHub token"}`, calls},
	} {
		at := slices.IndexFunc(list, func(t mcp.Tool) bool { return t.Name == c.tool })
		if at < 0 {
			t.Fatalf("there is no tool %s", c.tool)
		}
		if said, failed := list[at].Call(context.Background(), bind, json.RawMessage(c.args)); failed {
			t.Errorf("%s answered %q", c.tool, said)
		}
		select {
		case <-c.socket:
		case <-time.After(2 * time.Second):
			t.Errorf("%s did not reach its socket", c.tool)
		}
	}
	select {
	case stray := <-briefs:
		t.Errorf("the shelf of briefs got a request of another tool: %v", stray)
	case stray := <-calls:
		t.Errorf("the socket of calls got a request of another tool: %v", stray)
	default:
	}
}

const codexThread = "019a1f00-0000-7000-8000-00000000abcd"

// codexCaller stands in for the codex the server runs under: the caller is the
// thread the call names when it is the thread that codex holds, and there is
// none otherwise.
func codexCaller(meta json.RawMessage) (mcp.Binding, error) {
	var said struct {
		Thread string `json:"threadId"`
	}
	_ = json.Unmarshal(meta, &said)
	if said.Thread != codexThread {
		return mcp.Binding{}, fmt.Errorf("codex holds no thread %q", said.Thread)
	}
	return mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.codex", Dir: "/srv/proj"}, Name: "codex-0000abcd",
		SessionID: codexThread, PID: 800, Codex: true}, nil
}

// exchange runs a server over the lines given and returns its replies by id.
func exchange(t *testing.T, srv *mcp.Server, lines ...string) map[float64]map[string]any {
	t.Helper()
	var out strings.Builder
	if err := srv.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	replies := map[float64]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("the server wrote %q", line)
		}
		id, _ := m["id"].(float64)
		replies[id] = m
	}
	return replies
}

func toolCall(id int, tool, args, meta string) string {
	line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s`, id, tool, args)
	if meta != "" {
		line += `,"_meta":` + meta
	}
	return line + "}}"
}

// A thread of codex is offered the tools a thread has, in the order a claude
// session is offered them, and the server's word to it names only those:
// nothing of the restart it does not have, nor of the
// tools of claude it is pointed to elsewhere — a brief is weighed against a
// question in the conversation, and the rules say so before anything else.
func TestACodexIsToldOnlyOfWhatItHas(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	replies := exchange(t, CodexServer(codexCaller),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		toolCall(3, "brief_publish", `{}`, `{"threadId":"`+codexThread+`"}`),
	)
	hello, _ := replies[1]["result"].(map[string]any)
	said, _ := hello["instructions"].(string)
	listed, _ := replies[2]["result"].(map[string]any)
	tools, _ := listed["tools"].([]any)
	var names []string
	descriptions := ""
	for _, tool := range tools {
		tool, _ := tool.(map[string]any)
		name, _ := tool["name"].(string)
		about, _ := tool["description"].(string)
		names = append(names, name)
		descriptions += about + "\n"
	}
	if want := []string{"checklist", "brief_publish", "brief_delete", "notify", "secret_ask", "request", "session_close", "session_open",
		"send_to_session", "observer_start", "observer_list", "observer_stop", "observer_tail", "alarm_set", "alarm_list",
		"alarm_cancel"}; !slices.Equal(names, want) {
		t.Errorf("codex is offered %v, meant %v", names, want)
	}
	if lines := strings.Split(said, "\n"); len(lines) != len(names)+1 || lines[0] != codexLead {
		t.Errorf("the word is not the lead and a line a tool:\n%s", said)
	}
	for _, name := range names {
		if !strings.Contains(said, name) {
			t.Errorf("the word does not name %s:\n%s", name, said)
		}
	}
	for _, missing := range []string{"session_restart", "AskUserQuestion", "SendMessage"} {
		if strings.Contains(said, missing) {
			t.Errorf("the word to codex names %s, which it does not have:\n%s", missing, said)
		}
	}
	for _, missing := range []string{"session_restart", "AskUserQuestion"} {
		if strings.Contains(descriptions, missing) {
			t.Errorf("a tool tells codex of %s:\n%s", missing, descriptions)
		}
	}
	rules, _ := replies[3]["result"].(map[string]any)
	if content, _ := rules["content"].([]any); len(content) != 1 ||
		!strings.HasPrefix(content[0].(map[string]any)["text"].(string), "This session is a thread of codex.") {
		t.Errorf("the rules of a brief read to codex as %v", rules)
	}
}

// Every tool of a thread of codex works for the thread the call proves, and
// for nobody else: a call that names no thread, or a thread the codex does
// not hold, is refused, reaches no socket of the collector and keeps no
// checklist. The same calls from the thread itself go out under it — the
// checklist lands in the file of the thread, and every request of the
// collector names the thread for the session.
func TestEveryToolOfCodexWorksOnlyForTheThreadItProves(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("AACP_PANEL_URL", "http://127.0.0.1:1")
	briefs := collectorSocket(t, "AACP_BRIEF_SOCKET")
	calls := collectorSocket(t, "AACP_NOTIFY_SOCKET")
	acts := []struct{ tool, args string }{
		{"checklist", `{"items":[{"text":"read the code","status":"active"}]}`},
		{"checklist", `{}`},
		{"brief_publish", `{"doc":{"id":"a-brief","title":"A brief"}}`},
		{"brief_delete", `{"id":"a-brief"}`},
		{"notify", `{"text":"stuck on the migration"}`},
		{"secret_ask", `{"name":"github-token","title":"GitHub token"}`},
		{"send_to_session", `{"to":"lab","text":"done"}`},
		{"session_close", `{}`},
		{"session_open", `{"dir":"/srv/proj","agent":"codex"}`},
	}
	for _, meta := range []string{"", `{"callId":"call_1"}`, `{"threadId":"019a1f00-0000-7000-8000-00000000ef01"}`} {
		var lines []string
		for i, act := range acts {
			lines = append(lines, toolCall(i+1, act.tool, act.args, meta))
		}
		replies := exchange(t, CodexServer(codexCaller), lines...)
		for i, act := range acts {
			if res, _ := replies[float64(i+1)]["result"].(map[string]any); res == nil || res["isError"] != true {
				t.Errorf("%s with _meta %s answered %v", act.tool, meta, replies[float64(i+1)])
			}
		}
	}
	select {
	case stray := <-briefs:
		t.Errorf("a call of no thread reached the shelf of briefs: %v", stray)
	case stray := <-calls:
		t.Errorf("a call of no thread reached the socket of calls: %v", stray)
	default:
	}
	if entries, _ := os.ReadDir(filepath.Join(state, "aacpanel", "checklists")); len(entries) != 0 {
		t.Errorf("a call of no thread kept a checklist: %v", entries)
	}

	var lines []string
	for i, act := range acts[:6] {
		lines = append(lines, toolCall(i+1, act.tool, act.args, `{"threadId":"`+codexThread+`"}`))
	}
	replies := exchange(t, CodexServer(codexCaller), lines...)
	for i, act := range acts[:6] {
		if res, _ := replies[float64(i+1)]["result"].(map[string]any); res == nil || res["isError"] != nil {
			t.Errorf("%s from the thread answered %v", act.tool, replies[float64(i+1)])
		}
	}
	kept := checklist.ReadThread(checklist.Dir(), codexThread)
	if kept == nil || kept.PID != 800 || len(kept.Items) != 1 || kept.Items[0].Text != "read the code" {
		t.Errorf("the checklist of the thread is %+v", kept)
	}
	for _, socket := range []<-chan map[string]any{briefs, briefs, calls, calls} {
		select {
		case got := <-socket:
			if got["sessionId"] != codexThread {
				t.Errorf("the collector was asked for another session: %v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a call of the thread did not reach its socket")
		}
	}
}

// fakePanel stands in for the panel's local listener, named to the tools by
// the variable that moves it: it answers every action with ok and hands the
// action over.
func fakePanel(t *testing.T) <-chan map[string]any {
	t.Helper()
	got := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var asked map[string]any
		_ = json.NewDecoder(r.Body).Decode(&asked)
		got <- asked
		_, _ = w.Write([]byte(`{"ok":true,"detail":"session lab closed gracefully, the transcript is complete"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AACP_PANEL_URL", srv.URL)
	return got
}

// The close goes for the session its server serves and for nobody else: a
// claude session names the conversation its server finds, a thread of codex
// the thread its call proves, and a call of codex that names no thread the
// codex holds closes nothing and reaches no panel.
func TestTheCloseClosesOnlyTheSessionCalling(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AACP_STATE_DIR", t.TempDir())
	asked := fakePanel(t)
	here := mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.claude", Dir: "/srv/proj"}, Name: "lab",
		SessionID: "567f4d24-cd5f-48fa-bdc1-04c89d203494", PID: 4242}

	replies := exchange(t, Server(func() (mcp.Binding, error) { return here, nil }),
		toolCall(1, "session_close", `{}`, ""))
	replies2 := exchange(t, CodexServer(codexCaller),
		toolCall(1, "session_close", `{}`, `{"threadId":"`+codexThread+`"}`),
		toolCall(2, "session_close", `{}`, ""),
		toolCall(3, "session_close", `{}`, `{"threadId":"019a1f00-0000-7000-8000-00000000ef01"}`),
	)
	for name, reply := range map[string]map[string]any{"claude": replies[1], "the thread": replies2[1]} {
		if res, _ := reply["result"].(map[string]any); res == nil || res["isError"] != nil {
			t.Errorf("the close of %s answered %v", name, reply)
		}
	}
	for _, id := range []float64{2, 3} {
		if res, _ := replies2[id]["result"].(map[string]any); res == nil || res["isError"] != true {
			t.Errorf("a close of no thread the codex holds answered %v", replies2[id])
		}
	}
	for _, conversation := range []string{here.SessionID, codexThread} {
		select {
		case got := <-asked:
			params, _ := got["params"].(map[string]any)
			if got["kind"] != "session.close" || got["target"] != "" || params["conversation"] != conversation {
				t.Errorf("the panel was asked %v, meant the close of conversation %s", got, conversation)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("the close of conversation %s did not reach the panel", conversation)
		}
	}
	select {
	case stray := <-asked:
		t.Errorf("a close of no thread reached the panel: %v", stray)
	default:
	}
}

// The server's word fits what claude keeps of it even when the session has the
// longest role, the place holds the longest checklist standing and three
// longest brief titles: cut, it loses the lines of the last tools, and the
// model never learns when to reach for them.
func TestTheServersWordFitsWhatClaudeKeeps(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cards []map[string]any
		for range 4 {
			cards = append(cards, map[string]any{"cwd": "/srv/proj", "title": strings.Repeat("𝔸", 200), "answered": 100, "questions": 100})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"briefs": cards})
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	here := mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.claude", Dir: "/srv/proj"},
		SessionID: "567f4d24-cd5f-48fa-bdc1-04c89d203494", PID: 4242}
	// A step of letters outside the basic plane: each is two units to claude.
	items := make([]checklist.Item, checklist.MaxItems)
	for i := range items {
		items[i] = checklist.Item{Text: strings.Repeat("𝔸", checklist.MaxText), Status: checklist.Done}
	}
	items[len(items)-1].Status = checklist.Active
	if _, err := checklist.Keep(checklist.Dir(), here, items, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	longest := strings.Repeat("𝔸", role.MaxText)
	if _, err := role.Keep(role.Dir(), here.Place, here.Name, longest, role.ByOpener(strings.Repeat("n", 64)),
		time.Now()); err != nil {
		t.Fatal(err)
	}

	srv := Server(func() (mcp.Binding, error) { return here, nil })
	var out strings.Builder
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out.String()), &reply); err != nil {
		t.Fatalf("the server answered %q: %v", out.String(), err)
	}
	said := reply.Result.Instructions
	if !strings.Contains(said, "39 of 40 steps finished") {
		t.Fatalf("the word does not carry the checklist of the place: %q", said)
	}
	if !strings.Contains(said, "and 1 more") {
		t.Fatalf("the word does not carry the answered briefs: %q", said)
	}
	if !strings.Contains(said, "“"+longest+"”") {
		t.Fatalf("the word does not carry the role of the session whole: %q", said)
	}
	// Claude counts the word as JavaScript counts a string, in UTF-16 units.
	t.Logf("server word: %d UTF-16 units", len(utf16.Encode([]rune(said))))
	if n := len(utf16.Encode([]rune(said))); n > WordMax {
		t.Errorf("the server's word is %d units, claude keeps %d:\n%s", n, WordMax, said)
	}
}
