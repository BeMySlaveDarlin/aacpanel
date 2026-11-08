package toolset

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
	"aacpanel/internal/taskreg"
)

func TestRegistryToolsAreVisibleOnlyInPanelProjects(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/briefs" {
			_, _ = w.Write([]byte(`{"briefs":[]}`))
			return
		}
		var in taskreg.Request
		if r.URL.Path != "/api/tasks/registry" || json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Errorf("unexpected registry request: %s", r.URL.Path)
			http.Error(w, "bad request", 400)
			return
		}
		writer := taskreg.FilesWriter
		if in.CWD == "/srv/panel" {
			writer = taskreg.PanelWriter
		}
		json.NewEncoder(w).Encode(taskreg.Context{Project: taskreg.Project{ID: 1, Writer: writer}})
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	names := []string{"task_list", "task_show", "task_create", "task_update", "handoff_write", "handoff_read"}
	baseline := Allowed()
	for _, agent := range []string{"claude", "codex"} {
		for _, writer := range []string{taskreg.FilesWriter, taskreg.PanelWriter} {
			t.Run(agent+"/"+writer, func(t *testing.T) {
				dir := "/srv/files"
				if writer == taskreg.PanelWriter {
					dir = "/srv/panel"
				}
				bind := func() (mcp.Binding, error) {
					return mcp.Binding{Place: mcp.Place{Dir: dir}, SessionID: "conversation"}, nil
				}
				srv := Server(bind)
				base := tools()
				if agent == "codex" {
					srv = CodexServer(codexCaller, dir)
					base = codexTools()
				}
				replies := exchange(t, srv, `{"jsonrpc":"2.0","id":0,"method":"initialize"}`, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
				var listed []string
				result := replies[1]["result"].(map[string]any)
				for _, v := range result["tools"].([]any) {
					listed = append(listed, v.(map[string]any)["name"].(string))
				}
				want := []string{}
				for _, tool := range base {
					want = append(want, tool.Name)
				}
				if writer == taskreg.PanelWriter {
					want = append(want, names...)
				}
				if !slices.Equal(listed, want) {
					t.Fatalf("listed %v, want %v", listed, want)
				}
				instructions := replies[0]["result"].(map[string]any)["instructions"].(string)
				if strings.Contains(instructions, "Use task_list") != (writer == taskreg.PanelWriter) {
					t.Fatalf("wrong scoped instructions: %s", instructions)
				}
				if writer == taskreg.FilesWriter && !slices.Equal(AllowedFor(dir), baseline) {
					t.Fatal("file project's allowed tool baseline changed")
				}
				if writer == taskreg.PanelWriter && len(AllowedFor(dir)) != len(baseline)+len(names) {
					t.Fatal("panel project's task tools are not allowed")
				}
			})
		}
	}
}
