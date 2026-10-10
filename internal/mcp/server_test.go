package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"
)

// client talks to a server over pipes, the way claude does over stdio: one
// JSON message a line each way. The pipes are the kernel's, with a buffer:
// a server that says a line too many does not stop the next request from
// being written, and the test reads the stray line instead of hanging.
type client struct {
	t    *testing.T
	in   *os.File
	out  *bufio.Reader
	next int
	done chan error
}

func serve(t *testing.T, s *Server) *client {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, in: inW, out: bufio.NewReader(outR), done: make(chan error, 1)}
	go func() {
		err := s.Serve(context.Background(), inR, outW)
		outW.Close()
		inR.Close()
		c.done <- err
	}()
	t.Cleanup(func() {
		defer outR.Close()
		inW.Close()
		select {
		case err := <-c.done:
			if err != nil {
				t.Errorf("the server ended with %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("the server did not end when its input closed")
		}
	})
	return c
}

func (c *client) send(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, line+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) read() map[string]any {
	c.t.Helper()
	got := make(chan string, 1)
	go func() {
		line, _ := c.out.ReadString('\n')
		got <- line
	}()
	select {
	case line := <-got:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			c.t.Fatalf("the server wrote %q: %v", line, err)
		}
		if m["jsonrpc"] != "2.0" {
			c.t.Errorf("a reply without jsonrpc 2.0: %v", m)
		}
		return m
	case <-time.After(3 * time.Second):
		c.t.Fatal("the server did not answer")
		return nil
	}
}

// call sends a request and returns its reply, checking the reply is to it.
func (c *client) call(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	c.send(string(body))
	m := c.read()
	if id, _ := m["id"].(float64); int(id) != c.next {
		c.t.Fatalf("the reply to %s carries id %v, meant %d: %v", method, m["id"], c.next, m)
	}
	return m
}

func (c *client) result(method string, params any) map[string]any {
	c.t.Helper()
	m := c.call(method, params)
	res, ok := m["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s answered without a result: %v", method, m)
	}
	return res
}

func (c *client) errorCode(m map[string]any) int {
	c.t.Helper()
	e, ok := m["error"].(map[string]any)
	if !ok {
		c.t.Fatalf("an error was meant, the reply is %v", m)
	}
	code, _ := e["code"].(float64)
	return int(code)
}

func text(res map[string]any) string {
	content, _ := res["content"].([]any)
	if len(content) != 1 {
		return ""
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "text" {
		return ""
	}
	s, _ := part["text"].(string)
	return s
}

var lab = Binding{Place: Place{ConfigDir: "/srv/claude", Dir: "/srv/proj/lab"}, SessionID: "5a0c7d1e-2b3f-4a5b-8c6d-7e8f9a0b1c2d", PID: 4242}

func placed() (Binding, error) { return lab, nil }

func lost() (Binding, error) {
	return Binding{}, errors.New("where claude process 9 works is not known")
}

// called is what a fake tool was called with.
type called struct {
	args  string
	bound Binding
	err   error
}

// fake is a tool that answers with its name and the arguments it got, and
// keeps what it was called with. A tool that fails says so as its error.
func fake(name string, calls *[]called) Tool {
	return Tool{
		Name:         name,
		Title:        "The " + name,
		Description:  "What " + name + " does.",
		InputSchema:  map[string]any{"type": "object", "properties": map[string]any{name: map[string]any{"type": "string"}}},
		Instructions: "Use " + name + " when it helps.",
		Call: func(_ context.Context, bind Bind, args json.RawMessage) (string, bool) {
			b, err := bind()
			*calls = append(*calls, called{args: string(args), bound: b, err: err})
			if err != nil {
				return name + " could not: " + err.Error(), true
			}
			return name + " took " + string(args), false
		},
	}
}

// The handshake claude makes: initialize, the notification that it is done,
// the list of tools. The server answers in the version asked, says it has
// tools, and lists every tool it has in its order, each as the tool says.
func TestTheServerIntroducesItselfAndListsItsTools(t *testing.T) {
	var calls []called
	first, second := fake("first", &calls), fake("second", &calls)
	c := serve(t, &Server{Lead: "The panel is here.", Tools: []Tool{first, second}, Bind: placed})

	hello := c.result("initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "claude-code", "version": "2.1.283"},
	})
	if hello["protocolVersion"] != "2025-06-18" {
		t.Errorf("asked 2025-06-18, answered %v", hello["protocolVersion"])
	}
	if caps, _ := hello["capabilities"].(map[string]any); caps["tools"] == nil {
		t.Errorf("the server does not say it has tools: %v", hello["capabilities"])
	}
	if info, _ := hello["serverInfo"].(map[string]any); info["name"] != ServerName {
		t.Errorf("the server calls itself %v", hello["serverInfo"])
	}

	// A notification is not answered: the next line is the reply to the list.
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	tools, _ := c.result("tools/list", map[string]any{})["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("the server lists %d tools, meant two: %v", len(tools), tools)
	}
	for i, want := range []Tool{first, second} {
		got := tools[i].(map[string]any)
		schema, _ := json.Marshal(want.InputSchema)
		listed, _ := json.Marshal(got["inputSchema"])
		if got["name"] != want.Name || got["title"] != want.Title || got["description"] != want.Description ||
			string(listed) != string(schema) || len(got) != 4 {
			t.Errorf("tool %d is listed as %v, meant %s", i, got, want.Name)
		}
	}
	if len(calls) != 0 {
		t.Errorf("listing called a tool: %v", calls)
	}

	if got := c.result("ping", nil); len(got) != 0 {
		t.Errorf("ping answered %v", got)
	}
}

func TestAVersionTheServerDoesNotKnowIsAnsweredWithItsNewest(t *testing.T) {
	c := serve(t, &Server{Bind: placed})
	if got := c.result("initialize", map[string]any{"protocolVersion": "2099-01-01"}); got["protocolVersion"] != Protocols[0] {
		t.Errorf("answered %v", got["protocolVersion"])
	}
}

// A call goes to the tool it names and to no other, with the arguments as
// the model sent them and the place found at the call; the tool's answer,
// error or not, is the result of the call.
func TestACallGoesToTheToolItNames(t *testing.T) {
	var firsts, seconds []called
	c := serve(t, &Server{Tools: []Tool{fake("first", &firsts), fake("second", &seconds)}, Bind: placed})

	res := c.result("tools/call", map[string]any{"name": "second", "arguments": map[string]any{"second": "x"}})
	if res["isError"] != nil || text(res) != `second took {"second":"x"}` {
		t.Errorf("the call of second answered %v", res)
	}
	if len(firsts) != 0 || len(seconds) != 1 || seconds[0].bound != lab || seconds[0].err != nil {
		t.Errorf("first was called %v, second %v", firsts, seconds)
	}

	res = c.result("tools/call", map[string]any{"name": "first"})
	if res["isError"] != nil || text(res) != "first took " || len(firsts) != 1 || len(seconds) != 1 {
		t.Errorf("the call of first answered %v; first was called %v, second %v", res, firsts, seconds)
	}
}

// A server with a caller finds who calls by each call's own _meta and not by
// its parent: codex holds many threads in one process and names the thread
// in the call. The handshake carries no call, and the place it is told of is
// Bind's.
func TestACallerFindsWhoCallsByTheCallsMeta(t *testing.T) {
	var calls []called
	var metas []string
	thread := Binding{Place: Place{ConfigDir: "/srv/codex", Dir: "/srv/proj/shop"}, SessionID: "019a1f00-0000-7000-8000-00000000abcd",
		PID: 77, Codex: true}
	caller := func(meta json.RawMessage) Bind {
		metas = append(metas, string(meta))
		return func() (Binding, error) {
			if string(meta) == "" {
				return Binding{}, errors.New("the call names no thread")
			}
			return thread, nil
		}
	}
	c := serve(t, &Server{Tools: []Tool{fake("first", &calls)}, Bind: lost, Caller: caller})

	res := c.result("tools/call", map[string]any{"name": "first", "arguments": map[string]any{},
		"_meta": map[string]any{"threadId": thread.SessionID}})
	if res["isError"] != nil || len(calls) != 1 || calls[0].bound != thread {
		t.Fatalf("the call answered %v and the tool was called %+v", res, calls)
	}
	if len(metas) != 1 || metas[0] != `{"threadId":"`+thread.SessionID+`"}` {
		t.Errorf("the caller was asked with %q", metas)
	}

	res = c.result("tools/call", map[string]any{"name": "first", "arguments": map[string]any{}})
	if res["isError"] != true || text(res) != "first could not: the call names no thread" {
		t.Errorf("a call with no _meta answered %v", res)
	}
}

// What a tool could not do is its own error, which the model reads: the
// protocol has nothing to say of it.
func TestWhatAToolCouldNotDoComesBackAsItsError(t *testing.T) {
	var calls []called
	c := serve(t, &Server{Tools: []Tool{fake("first", &calls)}, Bind: lost})
	m := c.call("tools/call", map[string]any{"name": "first", "arguments": map[string]any{}})
	res, _ := m["result"].(map[string]any)
	if m["error"] != nil || res["isError"] != true || text(res) != "first could not: where claude process 9 works is not known" {
		t.Errorf("a tool that could not answered %v", m)
	}
}

// A tool the server does not have is an error of the protocol, and no tool
// is called in its place.
func TestAToolTheServerDoesNotHaveIsAnErrorOfTheProtocol(t *testing.T) {
	var calls []called
	c := serve(t, &Server{Tools: []Tool{fake("first", &calls), fake("second", &calls)}, Bind: placed})
	for _, params := range []any{
		map[string]any{"name": "TodoWrite", "arguments": map[string]any{}},
		map[string]any{"name": "", "arguments": map[string]any{}},
		map[string]any{"arguments": map[string]any{}},
	} {
		if code := c.errorCode(c.call("tools/call", params)); code != codeInvalidParams {
			t.Errorf("%v: code %d", params, code)
		}
	}
	if code := c.errorCode(c.call("tools/call", "first")); code != codeInvalidParams {
		t.Errorf("a call that is not an object: code %d", code)
	}
	if len(calls) != 0 {
		t.Errorf("a tool was called in place of one the server does not have: %v", calls)
	}
}

func instructionsOf(t *testing.T, s *Server) string {
	t.Helper()
	said, _ := serve(t, s).result("initialize", map[string]any{"protocolVersion": "2025-06-18"})["instructions"].(string)
	return said
}

// The server's word to a session is its lead and then the line of every
// tool, in the order of the tools, each followed by what the place holds for
// it where the tool says that. A place not known is said nothing of, and
// what is empty leaves no gap.
func TestTheInstructionsAreTheLeadAndTheLineOfEveryTool(t *testing.T) {
	var calls []called
	first, second := fake("first", &calls), fake("second", &calls)
	var asked []Binding
	second.Standing = func(b Binding) string {
		asked = append(asked, b)
		return "The place holds two of second."
	}

	said := instructionsOf(t, &Server{Lead: "The panel is here.", Tools: []Tool{first, second}, Bind: placed})
	if want := "The panel is here.\nUse first when it helps.\nUse second when it helps. The place holds two of second."; said != want {
		t.Errorf("the instructions are %q, meant %q", said, want)
	}
	if len(asked) != 1 || asked[0] != lab {
		t.Errorf("the standing of second was asked for %v", asked)
	}

	said = instructionsOf(t, &Server{Lead: "The panel is here.", Tools: []Tool{first, second}, Bind: lost})
	if want := "The panel is here.\nUse first when it helps.\nUse second when it helps."; said != want {
		t.Errorf("a place not known: %q", said)
	}

	second.Standing = func(Binding) string { return "" }
	first.Instructions = ""
	if said := instructionsOf(t, &Server{Tools: []Tool{first, second}, Bind: placed}); said != "Use second when it helps." {
		t.Errorf("with nothing to say but one line: %q", said)
	}
	if len(calls) != 0 {
		t.Errorf("the handshake called a tool: %v", calls)
	}
}

// The launcher allows a tool by the name claude gives it, and only a tool
// that is marked allowed.
func TestAllowedAreTheAllowedToolsByTheNamesClaudeGivesThem(t *testing.T) {
	var calls []called
	first, second, third := fake("first", &calls), fake("second", &calls), fake("third", &calls)
	first.Allowed, third.Allowed = true, true
	got := (&Server{Tools: []Tool{first, second, third}}).Allowed()
	if want := []string{"mcp__aacpanel__first", "mcp__aacpanel__third"}; !reflect.DeepEqual(got, want) {
		t.Errorf("allowed %v, meant %v", got, want)
	}
	if got := (&Server{Tools: []Tool{second}}).Allowed(); got != nil {
		t.Errorf("a server with nothing allowed allows %v", got)
	}
}

// What is not a request the server knows is an error of the protocol: another
// method, a line that is not JSON, a batch. A notification and the answer of
// the client to a request are not answered at all.
func TestWhatIsNotARequestTheServerKnowsIsAnErrorOfTheProtocol(t *testing.T) {
	c := serve(t, &Server{Bind: placed})
	if code := c.errorCode(c.call("resources/list", nil)); code != codeNoMethod {
		t.Errorf("another method: code %d", code)
	}
	c.send(`{"jsonrpc":"2.0","id":7,"method":`)
	m := c.read()
	if c.errorCode(m) != codeParse || m["id"] != nil {
		t.Errorf("a broken line: %v", m)
	}
	c.send(`[{"jsonrpc":"2.0","id":8,"method":"ping"}]`)
	if m := c.read(); c.errorCode(m) != codeInvalidRequest {
		t.Errorf("a batch: %v", m)
	}
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	c.send(`{"jsonrpc":"2.0","id":9,"result":{}}`)
	if got := c.result("ping", nil); len(got) != 0 {
		t.Errorf("ping after a notification and a client's answer: %v", got)
	}
}

// A place is taken in one form: both paths absolute and cleaned.
func TestAPlaceIsCleanedOrIsNone(t *testing.T) {
	if got, ok := (Place{ConfigDir: "/srv/claude//", Dir: "/srv/proj/./lab/"}).Clean(); !ok || got != lab.Place {
		t.Errorf("cleaned to %+v %v", got, ok)
	}
	for _, p := range []Place{{ConfigDir: "srv/claude", Dir: "/srv/proj/lab"}, {ConfigDir: "/srv/claude"}, {}} {
		if got, ok := p.Clean(); ok || got != (Place{}) {
			t.Errorf("%+v is taken for a place: %+v", p, got)
		}
	}
}
