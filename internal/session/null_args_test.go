package session

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func callThroughMCP(t *testing.T, tool mcp.Tool, args json.RawMessage) (string, bool) {
	t.Helper()
	params := map[string]any{"name": tool.Name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	srv := mcp.Server{Tools: []mcp.Tool{tool}, Bind: boundAs("lead", mine)}
	var out bytes.Buffer
	if err := srv.Serve(t.Context(), bytes.NewReader(request), &out); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Error) != 0 || len(reply.Result.Content) != 1 || reply.Result.Content[0].Type != "text" {
		t.Fatalf("the MCP call returned no tool text: %s", out.Bytes())
	}
	return reply.Result.Content[0].Text, reply.Result.IsError
}

func TestMCPLetterNullsMatchOmittedArguments(t *testing.T) {
	for _, variant := range []struct {
		name string
		tool func(Host) mcp.Tool
	}{{"claude", Letter}, {"codex", CodexLetter}} {
		t.Run(variant.name, func(t *testing.T) {
			tool := variant.tool(letterListHost(t, machine))
			omitted, failed := callThroughMCP(t, tool, nil)
			if failed || !strings.Contains(omitted, "- shop — work — /srv/work/shop") {
				t.Fatalf("listing with omitted arguments: %q (error %v)", omitted, failed)
			}
			nulls, failed := callThroughMCP(t, tool, []byte(`{"to":null,"text":null,"expects":null,"due":null,"subject":null,"re":null,"outcome":null}`))
			if failed || nulls != omitted {
				t.Fatalf("listing with nulls: %q (error %v), want %q", nulls, failed, omitted)
			}

			p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"delivered"}`)
			tool = variant.tool(hostWith(t, "", url))
			omitted, failed = callThroughMCP(t, tool, []byte(`{"to":"shop","text":"Work"}`))
			if failed {
				t.Fatal(omitted)
			}
			nulls, failed = callThroughMCP(t, tool, []byte(`{"to":"shop","text":"Work","expects":null,"due":null,"subject":null,"re":null,"outcome":null}`))
			if failed || nulls != omitted {
				t.Fatalf("plain letter with nulls: %q (error %v), want %q", nulls, failed, omitted)
			}
			asked := p.calls()
			if len(asked) != 2 || asked[0]["kind"] != "session.letter" || asked[0]["target"] != "shop" ||
				!reflect.DeepEqual(asked[0]["params"], map[string]any{"text": "Work", "from": mine}) ||
				!reflect.DeepEqual(asked[0], asked[1]) {
				t.Fatalf("nulls changed the letter sent to the panel: %v", asked)
			}
			conflict := `{"to":"shop","text":"Work","expects":"done","re":"` + strings.Repeat("a", 32) + `","due":null,"subject":null,"outcome":null}`
			said, failed := callThroughMCP(t, tool, []byte(conflict))
			if !failed || !strings.Contains(said, "expects and re cannot be used together") || len(p.calls()) != 2 {
				t.Fatalf("a real claim conflict was not refused: %q (error %v), panel calls %v", said, failed, p.calls())
			}
		})
	}
}

func TestMCPOpenNullOptionsKeepClaudeAndRoleSemantics(t *testing.T) {
	for _, variant := range []struct {
		name string
		tool func(Host) mcp.Tool
	}{{"claude", Open}, {"codex", CodexOpen}} {
		t.Run(variant.name, func(t *testing.T) {
			p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"session lab-2 started","contour":"work"}`)
			tool := variant.tool(hostWith(t, "", url))
			omitted, failed := callThroughMCP(t, tool, []byte(`{"dir":"/srv/proj/lab","agent":"claude"}`))
			if failed {
				t.Fatal(omitted)
			}
			nulls, failed := callThroughMCP(t, tool, []byte(`{"dir":"/srv/proj/lab","agent":"claude","name":null,"resume":null,"role":null,"model":null,"effort":null,"sandbox":null,"writableRoots":null}`))
			if failed || nulls != omitted {
				t.Fatalf("opening Claude with nulls: %q (error %v), want %q", nulls, failed, omitted)
			}
			said, failed := callThroughMCP(t, tool, []byte(`{"dir":"/srv/proj/lab","agent":"claude","role":""}`))
			if failed {
				t.Fatal(said)
			}
			asked := p.calls()
			if len(asked) != 3 || !reflect.DeepEqual(asked[0], asked[1]) {
				t.Fatalf("null options changed the open sent to the panel: %v", asked)
			}
			params := asked[1]["params"].(map[string]any)
			if !reflect.DeepEqual(params, map[string]any{"path": "/srv/proj/lab", "agent": "claude", "parent": "lead", "openedBy": mine}) {
				t.Errorf("null role or Codex options reached the panel: %v", params)
			}
			if role, present := asked[2]["params"].(map[string]any)["role"]; !present || role != "" {
				t.Errorf("an empty role did not clear the role: %v", asked[2])
			}
			said, failed = callThroughMCP(t, tool, []byte(`{"dir":"/srv/proj/lab","agent":"claude","model":null,"effort":null,"sandbox":null,"writableRoots":[]}`))
			if !failed || !strings.Contains(said, "model, effort, sandbox and writableRoots are Codex options") || len(p.calls()) != 3 {
				t.Fatalf("explicit empty roots were not refused: %q (error %v), panel calls %v", said, failed, p.calls())
			}
		})
	}
}
