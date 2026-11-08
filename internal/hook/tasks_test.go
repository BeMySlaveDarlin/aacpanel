package hook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/taskreg"
)

func TestTaskContextHookPrintsOnlyForThePanelWriter(t *testing.T) {
	writer := taskreg.PanelWriter
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in taskreg.Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.Operation != "context" || in.CWD != "/srv/proj" {
			t.Errorf("context request: %+v", in)
		}
		handoff := taskreg.Handoff{Key: "saved-key", Title: "Saved work", Body: json.RawMessage(`"The build is ready."`), Next: "Read the review", Tasks: json.RawMessage(`{"closed":[1],"created":[2],"touched":[3]}`)}
		context := taskreg.Context{Project: taskreg.Project{Name: "Project", Writer: writer}, Handoff: &handoff, HandoffTasks: []taskreg.Task{{Number: 1, Title: "Build"}, {Number: 2, Title: "Review"}, {Number: 3, Title: "Tests"}}}
		context.Queue.Tasks = []taskreg.Task{{Number: 2, Title: "Review", Priority: "HIGH"}}
		context.Queue.Total = 1
		json.NewEncoder(w).Encode(context)
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	run := func() string {
		var out, errs bytes.Buffer
		if code := Run([]string{"task-context"}, strings.NewReader(`{"cwd":"/srv/proj","session_id":"new-conversation"}`), &out, &errs); code != 0 || errs.Len() != 0 {
			t.Fatalf("hook: %d %s", code, errs.String())
		}
		return out.String()
	}
	got := run()
	for _, text := range []string{"Saved work", "handoff_read key=saved-key", "Closed: #1 [Build]", "Created: #2 [Review]", "Touched: #3 [Tests]", "Next step: Read the review", "HIGH #2 [Review]", "SessionStart"} {
		if !strings.Contains(got, text) {
			t.Errorf("hook lost %q: %s", text, got)
		}
	}
	writer = taskreg.FilesWriter
	if got := run(); got != "" {
		t.Fatalf("file project got duplicate context: %s", got)
	}
}
