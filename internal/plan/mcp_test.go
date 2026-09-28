package plan

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
		err := s.Serve(inR, outW)
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

func fixed(id string, pid int) func() (Binding, error) {
	return func() (Binding, error) { return bound(id, pid), nil }
}

func clock() time.Time { return t0 }

// The handshake claude makes: initialize, the notification that it is done,
// the list of tools. The server answers in the version asked, says it has
// tools, and the one it lists is the plan with its schema.
func TestTheServerIntroducesItselfAndItsTool(t *testing.T) {
	c := serve(t, &Server{Dir: t.TempDir(), Bind: fixed(sid, 1), Now: clock})

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
	if said, _ := hello["instructions"].(string); said != Instructions {
		t.Errorf("a place with no plan is told more than what the tool is for: %q", said)
	}

	// A notification is not answered: the next line is the reply to the list.
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	tools, _ := c.result("tools/list", map[string]any{})["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("the server lists %d tools, meant the plan alone: %v", len(tools), tools)
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != ToolName || tool["description"] != Description {
		t.Errorf("the tool is %v", tool)
	}
	// Items are not required: a call without them reads the plan.
	schema, _ := tool["inputSchema"].(map[string]any)
	if schema["type"] != "object" || schema["required"] != nil {
		t.Errorf("the input schema is %v", schema)
	}
	item := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	status := item["properties"].(map[string]any)["status"].(map[string]any)
	if !reflect.DeepEqual(status["enum"], []any{"pending", "active", "done", "dropped"}) ||
		!reflect.DeepEqual(item["required"], []any{"text", "status"}) {
		t.Errorf("a step is described as %v", item)
	}

	if got := c.result("ping", nil); len(got) != 0 {
		t.Errorf("ping answered %v", got)
	}
}

func TestAVersionTheServerDoesNotKnowIsAnsweredWithItsNewest(t *testing.T) {
	c := serve(t, &Server{Dir: t.TempDir(), Bind: fixed(sid, 1), Now: clock})
	if got := c.result("initialize", map[string]any{"protocolVersion": "2099-01-01"}); got["protocolVersion"] != Protocols[0] {
		t.Errorf("answered %v", got["protocolVersion"])
	}
}

func callPlan(c *client, args any) map[string]any {
	c.t.Helper()
	return c.result("tools/call", map[string]any{"name": ToolName, "arguments": args})
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

// A call keeps the plan of the place the claude the server serves works in,
// with the conversation found on every call: the same process moves to
// another conversation on /clear, and the plan it sends next says so.
func TestACallKeepsThePlanOfThePlaceWithTheConversationAskedOnEveryCall(t *testing.T) {
	dir := t.TempDir()
	conversations := []string{sid, "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"}
	calls := 0
	c := serve(t, &Server{Dir: dir, Now: clock, Bind: func() (Binding, error) {
		calls++
		return bound(conversations[min(calls, 2)-1], 4242), nil
	}})

	res := callPlan(c, map[string]any{
		"items": []any{
			map[string]any{"text": "read the code", "status": "done"},
			map[string]any{"text": "write the tests", "status": "active"},
			map[string]any{"text": "mutate", "status": "pending"},
		},
		"note": "waits on nothing",
	})
	if res["isError"] != nil || !strings.Contains(text(res), "1 of 3") {
		t.Errorf("the call answered %v", res)
	}
	got := Read(dir, lab)
	if got == nil || got.PID != 4242 || got.SessionID != sid || got.Note != "waits on nothing" || len(got.Items) != 3 ||
		got.Items[1] != (Item{Text: "write the tests", Status: Active, Since: t0.Format(Stamp)}) {
		t.Fatalf("the file holds %+v", got)
	}

	callPlan(c, map[string]any{"items": []any{map[string]any{"text": "start over", "status": "active"}}})
	if next := Read(dir, lab); next == nil || next.Items[0].Text != "start over" || next.SessionID != conversations[1] {
		t.Errorf("the plan after /clear is %+v", next)
	}
}

// standingPlan is a plan a session left in the place before it restarted.
func standingPlan(t *testing.T, dir string, items ...Item) {
	t.Helper()
	if _, err := Keep(dir, bound("6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e", 999), items, "waits on CI", t0); err != nil {
		t.Fatal(err)
	}
}

func hello(t *testing.T, s *Server) string {
	t.Helper()
	said, _ := serve(t, s).result("initialize", map[string]any{"protocolVersion": "2025-06-18"})["instructions"].(string)
	return said
}

// A session started again in a place that has a plan learns of it with the
// handshake: how far it got, the step it stands at, and how to read the rest
// or clear it. The step is quoted short and the whole word stays bounded,
// since it stands in the system prompt of the whole session.
func TestTheHandshakeTellsARestartedSessionOfThePlanOfItsPlace(t *testing.T) {
	dir := t.TempDir()
	standingPlan(t, dir, Item{Text: "read the code", Status: Done}, Item{Text: "the old idea", Status: Dropped},
		Item{Text: "write the tests", Status: Active}, Item{Text: "mutate", Status: Pending})
	said := hello(t, &Server{Dir: dir, Bind: fixed(sid, 1), Now: clock})
	for _, want := range []string{Instructions, "2 of 4 steps finished", "the current step: “write the tests”",
		"without items", "clear it with an empty list", t0.Format(Stamp)} {
		if !strings.Contains(said, want) {
			t.Errorf("the instructions do not say %q: %q", want, said)
		}
	}

	next := t.TempDir()
	standingPlan(t, next, Item{Text: "read the code", Status: Done}, Item{Text: "mutate", Status: Pending})
	if said := hello(t, &Server{Dir: next, Bind: fixed(sid, 1), Now: clock}); !strings.Contains(said, "the next step: “mutate”") {
		t.Errorf("a plan with no step at work: %q", said)
	}

	over := t.TempDir()
	standingPlan(t, over, Item{Text: "read the code", Status: Done})
	if said := hello(t, &Server{Dir: over, Bind: fixed(sid, 1), Now: clock}); !strings.Contains(said, "1 of 1 steps finished, nothing left to do") {
		t.Errorf("a finished plan: %q", said)
	}

	long := t.TempDir()
	step := strings.Repeat("é", MaxText)
	standingPlan(t, long, Item{Text: step, Status: Active})
	said = hello(t, &Server{Dir: long, Bind: fixed(sid, 1), Now: clock})
	if strings.Contains(said, step) || !strings.Contains(said, strings.Repeat("é", standingStep-1)+"…") {
		t.Errorf("the step at its longest is quoted whole: %q", said)
	}
	if extra := utf8.RuneCountInString(said) - utf8.RuneCountInString(Instructions); extra > 500 {
		t.Errorf("the plan adds %d characters to the instructions", extra)
	}
}

// A place with no plan, or one not known at the handshake, is told what the
// tool is for and nothing more.
func TestTheHandshakeOfAPlaceWithoutAPlanSaysNothingOfOne(t *testing.T) {
	if said := hello(t, &Server{Dir: t.TempDir(), Bind: fixed(sid, 1), Now: clock}); said != Instructions {
		t.Errorf("an empty place: %q", said)
	}
	dir := t.TempDir()
	standingPlan(t, dir, Item{Text: "one", Status: Active})
	unknown := func() (Binding, error) { return Binding{}, errors.New("no file of the session yet") }
	if said := hello(t, &Server{Dir: dir, Bind: unknown, Now: clock}); said != Instructions {
		t.Errorf("a place not known: %q", said)
	}
}

// A call without items writes nothing and reads the plan whole, so a session
// that goes on with a plan it did not send can send it back with its
// changes. A place without a plan says so.
func TestACallWithoutItemsReadsThePlan(t *testing.T) {
	dir := t.TempDir()
	c := serve(t, &Server{Dir: dir, Bind: fixed(sid, 1), Now: clock})
	if res := callPlan(c, map[string]any{}); res["isError"] != nil || text(res) != "There is no plan in this place." {
		t.Errorf("an empty place read %v", res)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("reading wrote %v", entries)
	}

	standingPlan(t, dir, Item{Text: "read the code", Status: Done}, Item{Text: "write the tests", Status: Active})
	before := Read(dir, lab)
	res := callPlan(c, map[string]any{"note": "ignored"})
	want := "The plan, last sent 2026-09-28T10:00:00Z: 1 of 2 steps finished.\n" +
		"1. [done] read the code\n2. [active] write the tests\nNote: waits on CI"
	if res["isError"] != nil || text(res) != want {
		t.Errorf("the plan read as %q", text(res))
	}
	if after := Read(dir, lab); !reflect.DeepEqual(after, before) {
		t.Errorf("reading the plan changed it: %+v", after)
	}
}

// The first call of a place takes over a plan filed under a conversation of
// it, and a step sent again keeps the time it had there.
func TestACallTakesOverAPlanFiledUnderAConversationOfThePlace(t *testing.T) {
	dir, config := t.TempDir(), t.TempDir()
	here := Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	const before = "11111111-1111-4111-8111-111111111111"
	transcriptOf(t, here, before)
	filed(t, dir, before, "2026-09-28T09:00:00Z", Item{Text: "read the code", Status: Active, Since: "2026-09-28T08:00:00Z"})

	said := hello(t, &Server{Dir: dir, Now: clock, Bind: func() (Binding, error) { return Binding{Place: here, PID: 1}, nil }})
	if !strings.Contains(said, "the current step: “read the code”") {
		t.Errorf("the handshake did not take the filed plan over: %q", said)
	}
	c := serve(t, &Server{Dir: dir, Now: clock, Bind: func() (Binding, error) { return Binding{Place: here, SessionID: sid, PID: 1}, nil }})
	callPlan(c, map[string]any{"items": []any{map[string]any{"text": "read the code", "status": "active"}}})
	if got := Read(dir, here); got == nil || got.Items[0].Since != "2026-09-28T08:00:00Z" || got.SessionID != sid {
		t.Errorf("the plan went on as %+v", got)
	}
}

// What the model got wrong, or what the host could not do, is the tool's own
// error: the model reads it and sends the plan again. Nothing is written.
func TestWhatTheCallGotWrongComesBackAsTheToolsError(t *testing.T) {
	lost := func() (Binding, error) { return Binding{}, errors.New("where claude process 9 works is not known") }
	for name, c := range map[string]struct {
		args any
		bind func() (Binding, error)
		says string
	}{
		"an unknown status": {map[string]any{"items": []any{map[string]any{"text": "x", "status": "in_progress"}}},
			fixed(sid, 1), `"in_progress"`},
		"items of another shape":    {map[string]any{"items": "read, test"}, fixed(sid, 1), "not the plan's"},
		"a place not found":         {map[string]any{"items": []any{}}, lost, "not kept: where claude process 9"},
		"a place not found to read": {map[string]any{}, lost, "not read: where claude process 9"},
		"a place that is not one": {map[string]any{"items": []any{map[string]any{"text": "x", "status": "active"}}},
			func() (Binding, error) { return Binding{Place: Place{ConfigDir: "/srv/claude", Dir: "lab"}}, nil }, "place of the session is not known"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cl := serve(t, &Server{Dir: dir, Bind: c.bind, Now: clock})
			res := callPlan(cl, c.args)
			if res["isError"] != true || !strings.Contains(text(res), c.says) {
				t.Errorf("answered %v, meant a tool error saying %q", res, c.says)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("a refused plan was written: %v", entries)
			}
		})
	}
}

func TestAnEmptyListClearsThePlanThroughTheTool(t *testing.T) {
	dir := t.TempDir()
	c := serve(t, &Server{Dir: dir, Bind: fixed(sid, 1), Now: clock})
	callPlan(c, map[string]any{"items": []any{map[string]any{"text": "one", "status": "active"}}})
	res := callPlan(c, map[string]any{"items": []any{}})
	if res["isError"] != nil || !strings.Contains(text(res), "cleared") || Read(dir, lab) != nil {
		t.Errorf("clearing answered %v and left %+v", res, Read(dir, lab))
	}
}

// Only what is not a call of the plan is an error of the protocol: another
// tool, another method, a line that is not JSON.
func TestWhatIsNotThePlanIsAnErrorOfTheProtocol(t *testing.T) {
	c := serve(t, &Server{Dir: t.TempDir(), Bind: fixed(sid, 1), Now: clock})
	if code := c.errorCode(c.call("tools/call", map[string]any{"name": "TodoWrite", "arguments": map[string]any{}})); code != codeInvalidParams {
		t.Errorf("another tool: code %d", code)
	}
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
	// The answer of the client to a request is not answered back.
	c.send(`{"jsonrpc":"2.0","id":9,"result":{}}`)
	if got := c.result("ping", nil); len(got) != 0 {
		t.Errorf("ping after a client's answer: %v", got)
	}
}
