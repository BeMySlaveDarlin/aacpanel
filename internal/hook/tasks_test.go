package hook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/taskreg"
)

func taskGuideForTest(registry bool) string {
	if registry {
		return "Use the panel's task registry."
	}
	return "Keep a checklist before starting work."
}

func TestTaskContextHookKeepsTheRegistryAlongsideTheGuide(t *testing.T) {
	t.Setenv(CodexHomeEnv, "")
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
		if code := RunWithGuide([]string{"task-context"}, strings.NewReader(`{"cwd":"/srv/proj","session_id":"new-conversation"}`), &out, &errs, taskGuideForTest); code != 0 || errs.Len() != 0 {
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
	got = run()
	if strings.Contains(got, "Project task registry:") || !strings.Contains(got, "## Panel tools") {
		t.Fatalf("file project did not get the guide alone: %s", got)
	}
}

func taskContextOutput(t *testing.T, payload string) string {
	t.Helper()
	var out, errs bytes.Buffer
	code := RunWithGuide([]string{"task-context"}, strings.NewReader(payload), &out, &errs, taskGuideForTest)
	if code != 0 || errs.Len() != 0 {
		t.Fatalf("hook: exit %d, stderr %s", code, errs.String())
	}
	if out.Len() == 0 {
		return ""
	}
	var reply struct {
		Output struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil || reply.Output.Event != "SessionStart" {
		t.Fatalf("invalid start context: %s (%v)", out.String(), err)
	}
	return reply.Output.Context
}

func TestTaskContextGuideReachesEveryClaudeStartAndNoCodexStart(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(taskreg.Context{Project: taskreg.Project{Name: "Project", Writer: taskreg.PanelWriter}})
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	for _, agent := range []string{"claude", "codex", "codex legacy hook"} {
		for _, source := range []string{"startup", "resume", "compact", "clear"} {
			t.Run(agent+"/"+source, func(t *testing.T) {
				t.Setenv(CodexHomeEnv, "")
				payload := map[string]string{"cwd": "/srv/proj", "source": source, "session_id": "conversation"}
				if agent == "codex" {
					t.Setenv(CodexHomeEnv, "/home/u/.codex")
				}
				if agent == "codex legacy hook" {
					payload["turn_id"] = "turn"
				}
				body, _ := json.Marshal(payload)
				got := taskContextOutput(t, string(body))
				if !strings.Contains(got, "Project task registry: Project.") {
					t.Fatalf("start lost the task registry: %s", got)
				}
				at := strings.Index(got, "## Panel tools\n\n")
				if agent == "claude" {
					if at < 0 || got[at:] != "## Panel tools\n\n"+taskGuideForTest(true) {
						t.Fatalf("start lost the complete guide: %s", got)
					}
				} else if at >= 0 || strings.Contains(got, taskGuideForTest(false)) {
					t.Fatalf("codex start got tool guidance: %s", got)
				}
			})
		}
	}
}

func TestTaskContextGuideSurvivesAnUnavailableRegistry(t *testing.T) {
	t.Setenv(CodexHomeEnv, "")
	prior := taskContextTimeout
	taskContextTimeout = 10 * time.Millisecond
	t.Cleanup(func() { taskContextTimeout = prior })
	for _, failure := range []string{"refusal", "invalid response", "timeout", "connection refused"} {
		t.Run(failure, func(t *testing.T) {
			panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in taskreg.Request
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				switch failure {
				case "refusal":
					http.Error(w, "not available", http.StatusServiceUnavailable)
				case "invalid response":
					_, _ = w.Write([]byte(`{"project":`))
				case "timeout":
					<-r.Context().Done()
				}
			}))
			t.Cleanup(panel.Close)
			t.Setenv("AACP_PANEL_URL", panel.URL)
			if failure == "connection refused" {
				panel.Close()
			}
			got := taskContextOutput(t, `{"cwd":"/srv/proj","source":"startup","session_id":"conversation"}`)
			if want := "## Panel tools\n\n" + taskGuideForTest(false); got != want {
				t.Fatalf("registry failure lost the local guide: %s", got)
			}
			t.Setenv(CodexHomeEnv, "/home/u/.codex")
			if got := taskContextOutput(t, `{"cwd":"/srv/proj","session_id":"thread"}`); got != "" {
				t.Fatalf("codex got context from an unavailable registry: %s", got)
			}
		})
	}
}
