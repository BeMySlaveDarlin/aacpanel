package toolset

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
)

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
// on the person, and the letter to another session is not among them.
func TestTheLauncherIsGivenTheAllowedTools(t *testing.T) {
	want := []string{
		"mcp__aacpanel__checklist",
		"mcp__aacpanel__brief_publish",
		"mcp__aacpanel__brief_delete",
		"mcp__aacpanel__notify",
		"mcp__aacpanel__secret_ask",
		"mcp__aacpanel__session_restart",
	}
	if got := Allowed(); !slices.Equal(got, want) {
		t.Errorf("allowed %v", got)
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

// The server's word fits what claude keeps of it even when the place holds the
// longest checklist the tool takes: cut, it loses the lines of the last tools,
// and the model never learns when to reach for them.
func TestTheServersWordFitsWhatClaudeKeeps(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
	// Claude counts the word as JavaScript counts a string, in UTF-16 units.
	if n := len(utf16.Encode([]rune(said))); n > WordMax {
		t.Errorf("the server's word is %d units, claude keeps %d:\n%s", n, WordMax, said)
	}
}
