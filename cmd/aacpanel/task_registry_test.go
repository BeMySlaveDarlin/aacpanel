package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

func TestTaskRegistryChecksTheBoundProjectAndRecordsItsAuthorPG(t *testing.T) {
	srv, root := profilesServer(t)
	id := fillMap(t, srv, root)
	dir := filepath.Join(root, "aacpanel")
	other := otherProject(t, srv, root, "other-tasks")
	list, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found, ok := projectByPath(list, other)
	if !ok {
		t.Fatal("other project missing")
	}
	if _, err = srv.db.SwitchTaskWriter(t.Context(), id, dir, taskreg.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	const own = "567f4d24-cd5f-48fa-bdc1-04c89d203494"
	const foreign = "019a1f00-0000-7000-8000-00000000abcd"
	raw, _ := json.Marshal(map[string]any{"sessions": []map[string]any{{"session": "task-worker", "sessionId": own, "cwd": dir}, {"session": "other-worker", "sessionId": foreign, "cwd": other}}})
	srv.host = host.NewReader(snapshotWith(t, string(raw)))
	mux := srv.routes(srv.localGate())
	call := func(in taskreg.Request, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(in)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/registry", strings.NewReader(string(raw))))
		if w.Code != want {
			t.Fatalf("%s: %d %s", raw, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	title := "Work"
	create := taskreg.Request{Operation: "create", Conversation: own, Edit: taskreg.Edit{Title: &title}}
	response := call(create, 200)
	var task taskreg.Task
	if err = json.Unmarshal(response, &task); err != nil || task.Number != 1 {
		t.Fatalf("create: %s %v", response, err)
	}
	detail, err := srv.db.Task(t.Context(), id, 1)
	if err != nil || len(detail.Events) != 1 || detail.Events[0].Actor != own || detail.Events[0].ActorName != "task-worker" {
		t.Fatalf("bound author: %+v %v", detail.Events, err)
	}
	create.Project = id
	create.Conversation = foreign
	create.CWD = dir
	call(create, 403)
	create.Project = 0
	call(create, 409)
	create.Conversation = "missing-conversation"
	call(create, 409)
	create.Conversation = ""
	create.Project = id
	create.CWD = ""
	call(create, 200)
	detail, err = srv.db.Task(t.Context(), id, 2)
	if err != nil || detail.Events[0].Actor != "person" || detail.Events[0].ActorName != "person" {
		t.Fatalf("person author: %+v %v", detail.Events, err)
	}
	call(taskreg.Request{Operation: "list", Conversation: own, Text: "Work"}, 200)
	call(taskreg.Request{Operation: "show", Conversation: own, Number: 1}, 200)
	call(taskreg.Request{Operation: "update", Conversation: own, Number: 0, Edit: taskreg.Edit{Title: &title}}, 400)
	call(taskreg.Request{Operation: "handoff_write", Conversation: own, Handoff: taskreg.HandoffInput{Title: "Progress", Markdown: "Saved", Next: "Read review"}}, 200)
	call(taskreg.Request{Operation: "handoff_read", Conversation: own}, 200)
	var context taskreg.Context
	response = call(taskreg.Request{Operation: "context", CWD: dir}, 200)
	if err = json.Unmarshal(response, &context); err != nil || context.Handoff == nil || context.Handoff.Next != "Read review" || len(context.Queue.Tasks) != 2 || len(context.HandoffTasks) != 1 {
		t.Fatalf("hook context: %s %v", response, err)
	}
	for i := range 12 {
		queuedTitle := fmt.Sprintf("Queued work %d", i)
		priority := "HIGH"
		position := float64(12 - i)
		if i == 0 {
			priority = "LOW"
			position = -1
		}
		call(taskreg.Request{Operation: "create", Conversation: own, Edit: taskreg.Edit{Title: &queuedTitle, Priority: &priority, Order: &position}}, 200)
	}
	response = call(taskreg.Request{Operation: "context", CWD: dir}, 200)
	if err = json.Unmarshal(response, &context); err != nil || context.Queue.Total != 14 || len(context.Queue.Tasks) != 10 {
		t.Fatalf("bounded queue: %s %v", response, err)
	}
	for i, task := range context.Queue.Tasks {
		if task.Number != int64(14-i) {
			t.Fatalf("queue position %d: task %d has priority %s, want task %d", i, task.Number, task.Priority, 14-i)
		}
	}
	call(taskreg.Request{Operation: "switch", Conversation: own}, 403)
	page, err := srv.db.Tasks(t.Context(), store.TaskFilter{ProjectID: found.project.ID})
	if err != nil || page.Total != 0 {
		t.Fatalf("foreign caller wrote into file project: %+v %v", page, err)
	}
	public := srv.routes(gate{page: func(f http.HandlerFunc) http.Handler { return f }, stream: func(f http.HandlerFunc) http.Handler { return f }})
	w := httptest.NewRecorder()
	public.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/registry", strings.NewReader(`{}`)))
	if w.Code != 404 {
		t.Fatalf("registry exposed publicly: %d", w.Code)
	}
}

func TestTaskRegistrySwitchUsesTheFinalCollectorSnapshotPG(t *testing.T) {
	srv, root := profilesServer(t)
	id := fillMap(t, srv, root)
	dir := filepath.Join(root, "aacpanel")
	snapshot := taskreg.Snapshot{Root: dir, Present: true, TasksComplete: true, HandoffsComplete: true, Files: []taskreg.File{{Name: "tasks/7.json", Raw: `{"id":7,"title":"Last file task","status":"open"}`}}}
	agent := startAgent(t, map[string]any{"ok": true, "taskStore": snapshot})
	srv.chat = chat.New(agent.path)
	w := httptest.NewRecorder()
	raw, _ := json.Marshal(taskreg.Request{Operation: "switch", Project: id})
	srv.apiTaskRegistry(w, httptest.NewRequest("POST", "/api/tasks/registry", strings.NewReader(string(raw))))
	if w.Code != 200 {
		t.Fatalf("switch: %d %s", w.Code, w.Body.String())
	}
	req := <-agent.got
	if req.TaskStore == nil || req.TaskStore.CWD != dir {
		t.Fatalf("final import did not use map path: %+v", req)
	}
	detail, err := srv.db.Task(t.Context(), id, 7)
	if err != nil || detail.Task.Title != "Last file task" {
		t.Fatalf("final import missing: %+v %v", detail.Task, err)
	}
	writer, err := srv.db.TaskWriter(t.Context(), id)
	if err != nil || writer != taskreg.PanelWriter {
		t.Fatalf("writer: %s %v", writer, err)
	}
	if _, err = srv.importTaskStores(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-agent.got:
		t.Fatalf("panel project still read files: %+v", req)
	default:
	}
}
