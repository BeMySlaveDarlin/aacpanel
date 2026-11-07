package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/chat"
	"aacpanel/internal/store"
	"aacpanel/internal/taskreg"
)

func TestTaskHTTPImportsOnlyMappedProjectsAndServesReadsPG(t *testing.T) {
	db := journalDB(t)
	pool, _ := db.Pool()
	ctx := t.Context()
	var contour, group, project int
	if err := pool.QueryRow(ctx, `INSERT INTO profiles(name,config_dir) VALUES('task-http','/srv/account') RETURNING id`).Scan(&contour); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(t.Context(), `DELETE FROM profile_projects WHERE id=$1`, project)
		_, _ = pool.Exec(t.Context(), `DELETE FROM profile_groups WHERE id=$1`, group)
		_, _ = pool.Exec(t.Context(), `DELETE FROM profiles WHERE id=$1`, contour)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO profile_groups(profile_id,name) VALUES($1,'work') RETURNING id`, contour).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO profile_projects(group_id,name,path) VALUES($1,'registry','/srv/project') RETURNING id`, group).Scan(&project); err != nil {
		t.Fatal(err)
	}
	page := taskreg.Snapshot{Present: true, Root: "/srv/project", TasksComplete: true, HandoffsComplete: true,
		Files: []taskreg.File{{Name: "tasks/1.json", Raw: `{"id":1,"title":"LMS-42: import","status":"open","created":"2025-01-01","updated":"2025-01-02","tags":["backend"],"body":{"verify":"Read it."}}`}}, Next: "tasks/1.json"}
	last := page
	last.Files = []taskreg.File{{Name: "tasks/2.json", Raw: `{"id":2,"title":"Other work","status":"done","created":"2025-01-01","closed":"2025-01-02"}`}}
	last.Next = ""
	agent := startAgentSeq(t, map[string]any{"ok": true, "taskStore": page}, map[string]any{"ok": true, "taskStore": last})
	srv := &Server{db: db, chat: chat.New(agent.path)}
	mux := srv.routes(srv.localGate())
	request := func(method, path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	request("POST", fmt.Sprintf("/api/tasks/import?project=%d&cwd=/srv/outside", project), 200)
	for _, after := range []string{"", "tasks/1.json"} {
		req := <-agent.got
		if req.TaskStore == nil || req.TaskStore.CWD != "/srv/project" || req.TaskStore.After != after {
			t.Fatalf("map path or pagination lost: %+v", req)
		}
	}
	request("POST", fmt.Sprintf("/api/tasks/import?project=%d", project+100000), 404)
	select {
	case req := <-agent.got:
		t.Fatalf("unmapped project reached collector: %+v", req)
	default:
	}
	w := request("GET", fmt.Sprintf("/api/tasks?contour=%d&project=%d&status=open&tag=backend&q=Read&external=LMS-42", contour, project), 200)
	var list store.TaskPage
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Total != 1 {
		t.Fatalf("list: %s, %v", w.Body.String(), err)
	}
	w = request("GET", fmt.Sprintf("/api/projects/%d/tasks/1", project), 200)
	var detail taskreg.Detail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || detail.Task.Markdown != "## Verify\n\nRead it." || len(detail.Events) != 2 || detail.Handoffs == nil {
		t.Fatalf("detail: %s, %v", w.Body.String(), err)
	}
	w = request("GET", fmt.Sprintf("/api/tasks/period?contour=%d&from=2025-01-02&until=2025-01-03", contour), 200)
	var period store.TaskPeriod
	if err := json.Unmarshal(w.Body.Bytes(), &period); err != nil || period.TotalTasks != 2 || len(period.Groups) != 2 {
		t.Fatalf("period: %s, %v", w.Body.String(), err)
	}
	for _, path := range []string{"/api/tasks?project=bad", "/api/tasks?status=bad", "/api/tasks?offset=-1", "/api/tasks/period?contour=1&from=bad", "/api/tasks/period?contour=1", "/api/projects/bad/tasks/1", fmt.Sprintf("/api/projects/%d/tasks/bad", project)} {
		request("GET", path, 400)
	}
	request("GET", fmt.Sprintf("/api/projects/%d/tasks/999", project), 404)
	public := srv.routes(gate{page: func(f http.HandlerFunc) http.Handler { return f }, stream: func(f http.HandlerFunc) http.Handler { return f }})
	w = httptest.NewRecorder()
	public.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/import", nil))
	if w.Code != 404 {
		t.Fatalf("public import exposed: %d", w.Code)
	}
}

func TestTaskStoreRequiresCompletePagedRead(t *testing.T) {
	first := taskreg.Snapshot{Present: true, Root: "/srv/project", TasksComplete: true, Files: []taskreg.File{{Name: "tasks/1.json", Raw: `{}`}}, Next: "tasks/1.json"}
	agent := startAgentSeq(t, map[string]any{"ok": true, "taskStore": first}, map[string]any{"ok": false, "error": "the next page is unavailable"})
	if _, err := chat.New(agent.path).TaskStore(t.Context(), "/srv/project"); err == nil {
		t.Fatal("incomplete store read accepted")
	}
	stuck := startAgent(t, map[string]any{"ok": true, "taskStore": first})
	if _, err := chat.New(stuck.path).TaskStore(t.Context(), "/srv/project"); err == nil {
		t.Fatal("stuck cursor accepted")
	}
	old := startAgent(t, map[string]any{"ok": true, "items": []any{}})
	if _, err := chat.New(old.path).TaskStore(t.Context(), "/srv/project"); err == nil {
		t.Fatal("old collector accepted as an empty store")
	}
}

func TestTaskHTTPReportsUnavailableStorage(t *testing.T) {
	srv := &Server{}
	mux := srv.routes(srv.localGate())
	for _, request := range []struct{ method, path string }{{"GET", "/api/tasks"}, {"GET", "/api/tasks/period"}, {"GET", "/api/projects/1/tasks/1"}, {"POST", "/api/tasks/import"}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: %d", request.method, request.path, w.Code)
		}
	}
}
