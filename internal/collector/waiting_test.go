package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aacpanel/internal/mcp"
)

func TestTheBriefStandingSaysWhatTheHookSays(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/claude/testdata/brief-waiting.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, CWD, Line string
		Cards           []json.RawMessage
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := waitingBriefs(c.Cards, c.CWD); got != c.Line {
				t.Errorf("line %q, want %q", got, c.Line)
			}
		})
	}
}

// The line is computed at initialize, in the directory the server binds,
// and not at tool construction or on tools/list and tools/call.
func TestTheBriefStandingIsReadWhenTheSessionConnects(t *testing.T) {
	var calls atomic.Int32
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/briefs" {
			t.Errorf("the standing asked %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"briefs":[{"cwd":"/srv/proj","title":"Waiting","answered":2,"questions":5},{"cwd":"/srv/other","title":"Elsewhere","answered":5}]}`)
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL+"/")
	tool := BriefPublish("", "")
	if calls.Load() != 0 {
		t.Fatal("constructing the tool read the shelf")
	}
	srv := &mcp.Server{Tools: []mcp.Tool{tool}, Bind: func() (mcp.Binding, error) {
		return mcp.Binding{Place: mcp.Place{Dir: "/srv/proj"}}, nil
	}}
	var out strings.Builder
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"brief_publish","arguments":{}}}` + "\n"
	if err := srv.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result struct{ Instructions string }
	}
	if err := json.Unmarshal([]byte(strings.Split(out.String(), "\n")[0]), &reply); err != nil {
		t.Fatal(err)
	}
	if said := reply.Result.Instructions; !strings.Contains(said, "«Waiting» (2 of 5 answered)") || strings.Contains(said, "Elsewhere") || calls.Load() != 1 {
		t.Errorf("%d shelf reads, instructions %q", calls.Load(), said)
	}
}

func TestAnUnavailableShelfLeavesTheSessionAlone(t *testing.T) {
	for _, body := range []string{`not JSON`, `[]`, `{"briefs":{}}`, `{"nothing":[]}`} {
		t.Run(body, func(t *testing.T) {
			panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			t.Cleanup(panel.Close)
			t.Setenv("AACP_PANEL_URL", panel.URL)
			if got := briefStanding(mcp.Binding{}); got != "" {
				t.Errorf("bad shelf said %q", got)
			}
		})
	}
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"briefs":[{"cwd":"/srv/proj","answered":5}]}`)
	}))
	t.Setenv("AACP_PANEL_URL", panel.URL)
	if got := briefStanding(mcp.Binding{Place: mcp.Place{Dir: "/srv/proj"}}); got != "" {
		t.Errorf("a refused shelf said %q", got)
	}
	panel.Close()
	start := time.Now()
	if got := briefStanding(mcp.Binding{}); got != "" || time.Since(start) > 3*time.Second {
		t.Errorf("a panel that is gone said %q after %s", got, time.Since(start))
	}
}
