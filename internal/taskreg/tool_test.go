package taskreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func TestRegistryToolsSendOnlyTheirBoundConversation(t *testing.T) {
	requests := []Request{}
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		requests = append(requests, in)
		w.Write([]byte(`{"saved":true}`))
	}))
	t.Cleanup(panel.Close)
	tools := Tools(panel.URL)
	args := []string{`{"status":["open"],"tags":["backend"],"text":"Work","next":true,"limit":3}`, `{"number":4}`, `{"title":"Work","markdown":"Body"}`, `{"number":4,"status":"done","markdown":"Finished","section":"Result"}`, `{"title":"Saved","markdown":"Progress","next":"Review"}`, `{"key":"saved-key"}`}
	bind := func() (mcp.Binding, error) { return mcp.Binding{SessionID: "bound-conversation", Name: "worker"}, nil }
	for i, tool := range tools {
		if text, failed := tool.Call(t.Context(), bind, json.RawMessage(args[i])); failed {
			t.Fatalf("%s: %s %v", tool.Name, text, failed)
		}
		in := requests[len(requests)-1]
		if in.Conversation != "bound-conversation" || in.Project != 0 || in.CWD != "" {
			t.Fatalf("caller identity: %+v", in)
		}
		if _, failed := tool.Call(t.Context(), bind, json.RawMessage(`{"project":99}`)); !failed {
			t.Fatalf("%s accepted a project supplied by the model", tool.Name)
		}
	}
	if len(requests) != 6 || requests[0].Operation != "list" || !requests[0].Next || requests[1].Number != 4 || requests[2].Edit.Markdown == nil || requests[3].Edit.Status == nil || requests[4].Handoff.Next != "Review" || requests[5].Key != "saved-key" {
		t.Fatalf("tool payloads: %+v", requests)
	}
}

func registryToolReply(t *testing.T, name, args string, response json.RawMessage) string {
	t.Helper()
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(response)
	}))
	t.Cleanup(panel.Close)
	bind := func() (mcp.Binding, error) { return mcp.Binding{SessionID: "bound-conversation", Name: "worker"}, nil }
	for _, tool := range Tools(panel.URL) {
		if tool.Name != name {
			continue
		}
		text, failed := tool.Call(t.Context(), bind, json.RawMessage(args))
		if failed {
			t.Fatalf("%s failed: %s", name, text)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(text)); err != nil || compact.String() != text {
			t.Fatalf("%s did not return compact JSON: %v", name, err)
		}
		return text
	}
	t.Fatalf("tool %s is missing", name)
	return ""
}

func assertToolJSON(t *testing.T, got, want string) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal([]byte(got), &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("tool reply = %s, want %s", got, want)
	}
}

func TestRegistryToolsReturnOnlyTheRequestedContent(t *testing.T) {
	task := json.RawMessage(`{
		"projectId":1,"project":"Project","contourId":2,"contour":"Contour",
		"number":4,"title":"Work","status":"open","priority":"HIGH","order":2.5,
		"tags":["backend"],"blockedBy":[2],"external":{"key":"LMS-42"},
		"externalRefs":["LMS-42"],"externalUrl":"https://issues.example/LMS-42",
		"createdAt":"2025-01-01T10:00:00Z","updatedAt":"2025-01-02T10:00:00Z",
		"closedAt":null,"createdBy":"creator-id","closedBy":"","touchedBy":["worker-id"],
		"session":"worker-id","body":"Task markdown","markdown":"Task markdown",
		"source":{"body":"Task markdown","extra":"Imported data"},
		"importedAt":"2025-01-02T10:00:00Z","deletedAt":null,
		"lastEventAt":"2025-01-02T10:00:00Z","lastActor":"worker-id",
		"lastActorName":"worker-id","inWork":true,
		"people":[{"name":"worker-id","conversations":["worker-id"],"live":true}],
		"extra":"Screen data"
	}`)
	row := `{"number":4,"title":"Work","status":"open","priority":"HIGH","tags":["backend"],"blockedBy":[2],"external":"https://issues.example/LMS-42","updatedAt":"2025-01-02T10:00:00Z"}`
	handoff := json.RawMessage(`{
		"key":"saved-key","title":"Saved","at":"2025-01-02T10:00:00Z",
		"sessionName":"worker","next":"Review","file":"panel-saved-key",
		"session":"worker-id","kind":"session","date":"2025-01-02","cwd":"/srv/project",
		"git":{"branch":"main","commit":"abc"},
		"tasks":{"created":[4],"closed":[2],"touched":[4,5]},"taskNumbers":[2,4,5],
		"body":"Handoff markdown","source":{"body":"Handoff markdown","extra":"Imported data"},
		"deletedAt":null,"extra":"Screen data"
	}`)
	handoffRow := `{"key":"saved-key","title":"Saved","at":"2025-01-02T10:00:00Z","sessionName":"worker"}`
	events := `[
		{"id":7,"kind":"closed","at":"2025-01-02T10:00:00Z","precision":"instant",
		 "actor":"worker-id","actorName":"recorded-worker","origin":"panel",
		 "payload":{"from":"open","to":"done","status":"done","changes":{
		    "title":{"from":"Old","to":"Work"},"status":{"from":"open","to":"done"},
		    "priority":{"from":"LOW","to":"HIGH"},"order":2.5,"tags":["backend"],
		    "blockedBy":[2],"external":"LMS-42","body":"Task markdown",
		    "unknown":{"body":"Task markdown"}},"body":"Task markdown"}},
		{"kind":"closed","at":"2025-01-01T00:00:00Z","actor":"creator-id","origin":"cctm","payload":{"status":"cancelled"}},
		{"kind":"status_changed","at":"2025-01-01T00:00:00Z","origin":"cctm","payload":{"from":"deferred","to":"open"}},
		{"kind":"created","at":"2025-01-01T00:00:00Z","origin":"cctm","payload":{}}
	]`
	show := fmt.Sprintf(`{"task":%s,"events":%s,"handoffs":[%s]}`, task, events, handoff)
	showWant := fmt.Sprintf(`{"task":%s,"events":[
		{"kind":"closed","at":"2025-01-02T10:00:00Z","actorName":"recorded-worker","origin":"panel",
		 "from":"open","to":"done","status":"done","changes":{
		    "title":{"from":"Old","to":"Work"},"status":{"from":"open","to":"done"},
		    "priority":{"from":"LOW","to":"HIGH"},"order":2.5,"tags":["backend"],
		    "blockedBy":[2],"external":"LMS-42"}},
		{"kind":"closed","at":"2025-01-01T00:00:00Z","origin":"cctm","status":"cancelled"},
		{"kind":"status_changed","at":"2025-01-01T00:00:00Z","origin":"cctm","from":"deferred","to":"open"},
		{"kind":"created","at":"2025-01-01T00:00:00Z","origin":"cctm"}
	],"handoffs":[%s]}`, strings.TrimSuffix(row, "}")+`,"markdown":"Task markdown"}`, handoffRow)
	handoffWant := strings.TrimSuffix(handoffRow, "}") + `,"next":"Review","markdown":"Handoff markdown","tasks":{"created":[4],"closed":[2],"touched":[4,5]}}`
	for _, tc := range []struct {
		name, args, response, want string
	}{
		{"task_list", `{}`, fmt.Sprintf(`{"tasks":[%s,{"number":5,"title":"Small","status":"open","priority":"","tags":[],"blockedBy":[],"external":null,"updatedAt":null}],"total":5,"limit":2,"offset":1}`, task), `{"tasks":[` + row + `,{"number":5,"title":"Small","status":"open"}],"total":5,"limit":2,"offset":1}`},
		{"task_show", `{"number":4}`, show, showWant},
		{"task_create", `{"title":"Work","markdown":"Task markdown"}`, string(task), row},
		{"task_update", `{"number":4,"markdown":"Task markdown"}`, string(task), row},
		{"handoff_read", `{"key":"saved-key"}`, string(handoff), handoffWant},
		{"handoff_write", `{"title":"Saved","markdown":"Handoff markdown","next":"Review"}`, string(handoff), handoffWant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := registryToolReply(t, tc.name, tc.args, json.RawMessage(tc.response))
			assertToolJSON(t, text, tc.want)
			if tc.name == "task_show" && strings.Count(text, "Task markdown") != 1 {
				t.Fatal("task markdown must appear exactly once")
			}
			if strings.HasPrefix(tc.name, "handoff_") && strings.Count(text, "Handoff markdown") != 1 {
				t.Fatal("handoff markdown must appear exactly once")
			}
		})
	}
}

func TestTaskListReplyStaysSmallWithLargeDocuments(t *testing.T) {
	body, err := json.Marshal(strings.Repeat("Large body", 1024))
	if err != nil {
		t.Fatal(err)
	}
	tasks := []json.RawMessage{}
	for number := 1; number <= 15; number++ {
		tasks = append(tasks, json.RawMessage(fmt.Sprintf(`{"number":%d,"title":"Work","status":"open","priority":"HIGH","tags":["backend"],"blockedBy":[20],"body":%s,"markdown":%s,"source":{"body":%s},"people":[{"name":"worker-id"}]}`, number, body, body, body)))
	}
	response, err := json.Marshal(struct {
		Tasks []json.RawMessage `json:"tasks"`
		Total int               `json:"total"`
		Limit int               `json:"limit"`
	}{tasks, 15, 100})
	if err != nil {
		t.Fatal(err)
	}
	text := registryToolReply(t, "task_list", `{}`, response)
	if len(text) >= 5000 || strings.Contains(text, "Large body") {
		t.Fatalf("task list includes documents: %d bytes", len(text))
	}
	var reply struct {
		Tasks []Task `json:"tasks"`
		Total int    `json:"total"`
	}
	if err = json.Unmarshal([]byte(text), &reply); err != nil || len(reply.Tasks) != 15 || reply.Total != 15 {
		t.Fatalf("task list lost its rows or total: %+v, %v", reply, err)
	}
	if strings.Contains(text, `"limit"`) || strings.Contains(text, `"offset"`) {
		t.Fatal("a complete list does not need pagination metadata")
	}
	t.Logf("list reply: %d bytes for 15 tasks with 10 KiB bodies", len(text))
}

func TestRegistryToolRepliesKeepEmptyCollections(t *testing.T) {
	for _, tc := range []struct {
		name, args, response, want string
	}{
		{"task_list", `{}`, `{"tasks":[],"total":0,"limit":100,"offset":0}`, `{"tasks":[],"total":0}`},
		{"task_show", `{"number":1}`, `{"task":{"number":1,"title":"Work","status":"open"},"events":null,"handoffs":null}`, `{"task":{"number":1,"title":"Work","status":"open","markdown":""},"events":[],"handoffs":[]}`},
		{"handoff_read", `{}`, `{"key":"saved-key","title":"Saved","body":{"result":"Finished","notes":["Read review"]},"tasks":{"created":[],"closed":[],"touched":[]}}`, `{"key":"saved-key","title":"Saved","markdown":"## Result\n\nFinished\n\n## Notes\n\n- Read review","tasks":{"created":[],"closed":[],"touched":[]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertToolJSON(t, registryToolReply(t, tc.name, tc.args, json.RawMessage(tc.response)), tc.want)
		})
	}
}

func TestTaskShowKeepsAnEventWhosePayloadHasAnotherShape(t *testing.T) {
	response := `{"task":{"number":1,"title":"Work","status":"open"},"events":[` +
		`{"kind":"imported","at":"2026-11-14T00:00:00Z","actorName":"cctm","payload":["a","b"]},` +
		`{"kind":"status","at":"2026-11-14T00:01:00Z","actorName":"lead","payload":{"from":3,"to":"done"}}],"handoffs":[]}`
	want := `{"task":{"number":1,"title":"Work","status":"open","markdown":""},"events":[` +
		`{"kind":"imported","at":"2026-11-14T00:00:00Z","actorName":"cctm"},` +
		`{"kind":"status","at":"2026-11-14T00:01:00Z","actorName":"lead"}],"handoffs":[]}`
	assertToolJSON(t, registryToolReply(t, "task_show", `{"number":1}`, json.RawMessage(response)), want)
}

func TestTaskToolRowsKeepOnlyExternalIssueReferences(t *testing.T) {
	for _, tc := range []struct {
		external, url, want string
	}{
		{`"LMS-42"`, "", "LMS-42"},
		{`"https://issues.example/LMS-42"`, "", "https://issues.example/LMS-42"},
		{`{"key":"LMS-42","extra":"ignored"}`, "", "LMS-42"},
		{`{"url":"https://issues.example/LMS-42","key":"LMS-42"}`, "", "https://issues.example/LMS-42"},
		{`{"number":"LMS-42"}`, "", "LMS-42"},
		{`{"id":"LMS-42"}`, "", "LMS-42"},
		{`{"session":"conversation","id":"4"}`, "", ""},
		{`{"session":null,"id":"LMS-42"}`, "", ""},
		{`null`, "", ""},
		{`""`, "", ""},
		{`{}`, "", ""},
		{`null`, "https://issues.example/LMS-42", "https://issues.example/LMS-42"},
	} {
		t.Run(tc.external+tc.url, func(t *testing.T) {
			response := fmt.Sprintf(`{"number":1,"title":"Work","status":"open","external":%s,"externalUrl":%q}`, tc.external, tc.url)
			want := `{"number":1,"title":"Work","status":"open"}`
			if tc.want != "" {
				want = strings.TrimSuffix(want, "}") + fmt.Sprintf(`,"external":%q}`, tc.want)
			}
			assertToolJSON(t, registryToolReply(t, "task_create", `{"title":"Work"}`, json.RawMessage(response)), want)
		})
	}
}

func TestRegistryToolsKeepServiceErrors(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the task does not exist", http.StatusNotFound)
	}))
	t.Cleanup(panel.Close)
	bind := func() (mcp.Binding, error) { return mcp.Binding{SessionID: "bound-conversation"}, nil }
	for _, tool := range Tools(panel.URL) {
		text, failed := tool.Call(t.Context(), bind, json.RawMessage(`{}`))
		if !failed || text != "task registry: 404 Not Found: the task does not exist" {
			t.Fatalf("%s changed a service error: %s, failed=%v", tool.Name, text, failed)
		}
	}
}
