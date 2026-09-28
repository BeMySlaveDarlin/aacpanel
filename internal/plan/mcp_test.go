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

func fixed(id string, pid int) func() (string, int, error) {
	return func() (string, int, error) { return id, pid, nil }
}

func clock() time.Time { return t0 }

// The handshake claude makes: initialize, the notification that it is done,
// the list of tools. The server answers in the version asked, says it has
// tools, and the one it lists is the plan with its schema.
func TestTheServerIntroducesItselfAndItsTool(t *testing.T) {
	c := serve(t, &Server{Dir: t.TempDir(), Session: fixed(sid, 1), Now: clock})

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
	if said, _ := hello["instructions"].(string); !strings.Contains(said, "plan tool") {
		t.Errorf("the instructions do not name the tool: %q", said)
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
	schema, _ := tool["inputSchema"].(map[string]any)
	if schema["type"] != "object" || !reflect.DeepEqual(schema["required"], []any{"items"}) {
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
	c := serve(t, &Server{Dir: t.TempDir(), Session: fixed(sid, 1), Now: clock})
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

// A call keeps the plan under the conversation of the claude the server
// serves, asked on every call: the same process moves to another
// conversation on /clear, and the next plan is that one's.
func TestACallKeepsThePlanOfTheConversationAskedOnEveryCall(t *testing.T) {
	dir := t.TempDir()
	conversations := []string{sid, "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"}
	calls := 0
	c := serve(t, &Server{Dir: dir, Now: clock, Session: func() (string, int, error) {
		calls++
		return conversations[min(calls, 2)-1], 4242, nil
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
	got := Read(dir, sid)
	if got == nil || got.PID != 4242 || got.Note != "waits on nothing" || len(got.Items) != 3 ||
		got.Items[1] != (Item{Text: "write the tests", Status: Active, Since: t0.Format(Stamp)}) {
		t.Fatalf("the file holds %+v", got)
	}

	callPlan(c, map[string]any{"items": []any{map[string]any{"text": "start over", "status": "active"}}})
	if next := Read(dir, conversations[1]); next == nil || next.Items[0].Text != "start over" {
		t.Errorf("the plan after /clear went to %+v", next)
	}
	if again := Read(dir, sid); again == nil || len(again.Items) != 3 {
		t.Errorf("the plan of the conversation before /clear was touched: %+v", again)
	}
}

// What the model got wrong, or what the host could not do, is the tool's own
// error: the model reads it and sends the plan again. Nothing is written.
func TestWhatTheCallGotWrongComesBackAsTheToolsError(t *testing.T) {
	for name, c := range map[string]struct {
		args    any
		session func() (string, int, error)
		says    string
	}{
		"an unknown status": {map[string]any{"items": []any{map[string]any{"text": "x", "status": "in_progress"}}},
			fixed(sid, 1), `"in_progress"`},
		"no items at all":        {map[string]any{"note": "x"}, fixed(sid, 1), "items is required"},
		"items of another shape": {map[string]any{"items": "read, test"}, fixed(sid, 1), "not the plan's"},
		"a conversation not found": {map[string]any{"items": []any{}},
			func() (string, int, error) { return "", 0, errors.New("the conversation of process 9 is not known") },
			"process 9"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cl := serve(t, &Server{Dir: dir, Session: c.session, Now: clock})
			res := callPlan(cl, c.args)
			if res["isError"] != true || !strings.Contains(text(res), c.says) {
				t.Errorf("answered %v, meant a tool error saying %q", res, c.says)
			}
			if Read(dir, sid) != nil {
				t.Error("a refused plan was written")
			}
		})
	}
}

func TestAnEmptyListClearsThePlanThroughTheTool(t *testing.T) {
	dir := t.TempDir()
	c := serve(t, &Server{Dir: dir, Session: fixed(sid, 1), Now: clock})
	callPlan(c, map[string]any{"items": []any{map[string]any{"text": "one", "status": "active"}}})
	res := callPlan(c, map[string]any{"items": []any{}})
	if res["isError"] != nil || !strings.Contains(text(res), "cleared") || Read(dir, sid) != nil {
		t.Errorf("clearing answered %v and left %+v", res, Read(dir, sid))
	}
}

// Only what is not a call of the plan is an error of the protocol: another
// tool, another method, a line that is not JSON.
func TestWhatIsNotThePlanIsAnErrorOfTheProtocol(t *testing.T) {
	c := serve(t, &Server{Dir: t.TempDir(), Session: fixed(sid, 1), Now: clock})
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
