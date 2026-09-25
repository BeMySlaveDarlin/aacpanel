package executor

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// What claude answers to mcp_status, in the shape the holder passes it on:
// the control response around its own response.
const mcpAnswer = `{"subtype":"success","request_id":"panel-2","response":{"mcpServers":[
 {"name":"plugin:context7:context7","status":"connected",
  "serverInfo":{"name":"Context7","version":"4.1.1","description":"Docs for libraries."},
  "config":{"type":"http","url":"https://user:secret@mcp.context7.com/mcp?client=cc&key=abc#frag",
            "headers":{"Authorization":"Bearer token"}},
  "scope":"dynamic","source":"plugin","tools":[{"name":"resolve-library-id"},{"name":"query-docs"}]},
 {"name":"docker","status":"connected","scope":"user","source":"user",
  "config":{"type":"stdio","command":"/usr/local/bin/docker-mcp","args":["--token","s3cret"],"env":{"KEY":"v"}},
  "tools":[{"name":"ps"}]},
 {"name":"tg-shadow","status":"failed","error":"ECONNREFUSED: Unable to connect.","scope":"user","source":"user",
  "config":{"type":"http","url":"http://127.0.0.1:8901/mcp"}},
 {"status":"connected"}]}}`

// The list is claude's own, and only what the panel shows leaves the host:
// no headers, no arguments, no environment, and no part of an address that
// carries a key.
func TestMcpListsTheServersWithoutTheirKeys(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.answers = map[string]string{"mcp_status": mcpAnswer}
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Mcp(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Transport != action.SwitchStream || len(got.Servers) != 3 {
		t.Fatalf("the answer is %+v, expected three servers of a session on the stream", got)
	}
	want := action.McpServer{Name: "plugin:context7:context7", Status: "connected", Source: "plugin", Scope: "dynamic",
		Type: "http", URL: "https://mcp.context7.com/mcp", Title: "Context7", Version: "4.1.1",
		Description: "Docs for libraries.", Tools: []string{"resolve-library-id", "query-docs"}}
	if !reflect.DeepEqual(got.Servers[0], want) {
		t.Errorf("the first server is %+v, expected %+v", got.Servers[0], want)
	}
	if got.Servers[1].Command != "docker-mcp" || got.Servers[2].Error != "ECONNREFUSED: Unable to connect." {
		t.Errorf("the command is %q and the error %q", got.Servers[1].Command, got.Servers[2].Error)
	}
	for _, secret := range []string{"secret", "Bearer", "token", "s3cret", "key=abc", "KEY"} {
		for _, s := range got.Servers {
			if strings.Contains(strings.Join([]string{s.URL, s.Command, s.Error, s.Title}, " "), secret) {
				t.Errorf("%q left the host with server %s", secret, s.Name)
			}
		}
	}
	if asked := f.asked(); len(asked) != 1 || asked[0].Op != stream.OpControl || asked[0].Subtype != "mcp_status" {
		t.Errorf("the holder was asked %+v", asked)
	}
}

// A change goes as claude's own request for it: a reconnect by name, and a
// toggle that says which way.
func TestMcpChangesGoAsClaudesRequests(t *testing.T) {
	for _, c := range []struct {
		do, subtype string
		enabled     any
	}{
		{action.McpReconnect, "mcp_reconnect", nil},
		{action.McpDisable, "mcp_toggle", false},
		{action.McpEnable, "mcp_toggle", true},
	} {
		t.Run(c.do, func(t *testing.T) {
			f := onTheStream(t, false)
			e, _ := newTest(t, "")
			r := req(action.SessionMcp, "demo")
			r.Mcp = &action.McpChange{Server: "tg-shadow", Do: c.do}
			detail, err := e.Execute(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			asked := f.asked()
			if len(asked) != 1 || asked[0].Subtype != c.subtype || asked[0].Fields["serverName"] != "tg-shadow" ||
				asked[0].Fields["enabled"] != c.enabled {
				t.Fatalf("the holder was asked %+v", asked)
			}
			if !strings.Contains(detail, "tg-shadow") {
				t.Errorf("the report %q does not name the server", detail)
			}
		})
	}
}

// A terminal lists its servers on a screen driven by keys: the answer says
// where the session lives, and a change is refused rather than typed blind.
func TestATerminalSessionHasNoMcpForThePanel(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	got, err := e.Mcp(context.Background(), "term")
	if err != nil || got.Transport != action.SwitchConsole || len(got.Servers) != 0 {
		t.Fatalf("a terminal answered %+v, %v", got, err)
	}
	r := req(action.SessionMcp, "term")
	r.Mcp = &action.McpChange{Server: "docker", Do: action.McpReconnect}
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "own screen") {
		t.Errorf("a terminal took a change of MCP: %v", err)
	}
}
