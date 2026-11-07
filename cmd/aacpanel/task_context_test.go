package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/taskreg"
)

func TestTaskBoardKeepsLongBacklogsAndResolvesLiveAndArchivedPeoplePG(t *testing.T) {
	db := journalDB(t)
	pool, _ := db.Pool()
	ctx := t.Context()
	var contour, group, project int
	if err := pool.QueryRow(ctx, `INSERT INTO profiles(name,config_dir) VALUES('task-board','/srv/board-account') RETURNING id`).Scan(&contour); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM profile_projects WHERE id=$1`, project)
		_, _ = pool.Exec(context.Background(), `DELETE FROM profile_groups WHERE id=$1`, group)
		_, _ = pool.Exec(context.Background(), `DELETE FROM profiles WHERE id=$1`, contour)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO profile_groups(profile_id,name) VALUES($1,'work') RETURNING id`, contour).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO profile_projects(group_id,name,path) VALUES($1,'board','/srv/board') RETURNING id`, group).Scan(&project); err != nil {
		t.Fatal(err)
	}
	raw := `{"id":1,"title":"Active work","status":"open","created":"2025-01-01","updated":"2025-01-02","session":"22222222","touched_by":["11111111","22222222"],"body":"Load the document only when opened."}`
	if out, err := db.ImportTasks(ctx, project, "/srv/board", taskreg.Snapshot{Root: "/srv/board", Present: true, TasksComplete: true, Files: []taskreg.File{{Name: "tasks/1.json", Raw: raw}}}); err != nil || len(out.Errors) > 0 {
		t.Fatalf("import: %+v %v", out, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_tasks(project_id,number,title,status,source,source_json) SELECT $1,n,'Quiet work '||n,'open','{}','{}'::jsonb FROM generate_series(2,502) n`, project); err != nil {
		t.Fatal(err)
	}
	archived := chat.ArchivePage{Rows: []chat.ArchiveRow{{SessionID: "11111111-old", Name: "evirma-ser1063"}, {SessionID: "22222222-live", Name: "evirma-ser1063"}}, Total: 2, Limit: 100}
	withAuditor := archived
	withAuditor.Rows = append(append([]chat.ArchiveRow{}, archived.Rows...), chat.ArchiveRow{SessionID: "55555555-old", Name: "auditor"})
	withAuditor.Total = 3
	agent := startAgentSeq(t, map[string]any{"ok": true, "archive": archived}, map[string]any{"ok": true, "archive": withAuditor}, map[string]any{"ok": true, "archive": archived})
	now := time.Now().Unix()
	snapshot := fmt.Sprintf(`{"at":%d,"sessionsAt":%d,"sessions":[{"session":"evirma-ser1063","sessionId":"22222222-live","cwd":"/srv/board","contour":%d,"status":"busy"},{"session":"evirma-ser1064","sessionId":"33333333-live","cwd":"/srv/board","contour":%d,"status":"idle"},{"session":"other-ser999","sessionId":"44444444-live","cwd":"/srv/other","contour":999,"status":"busy"}]}`, now, now, contour, contour)
	srv := &Server{db: db, host: hostWith(t, snapshot), chat: chat.New(agent.path)}
	mux := srv.routes(srv.localGate())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/tasks/board?contour=%d", contour), nil))
	var board struct {
		Tasks  []taskreg.Task    `json:"tasks"`
		NoTask []taskreg.Unfiled `json:"noTask"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil || w.Code != 200 || len(board.Tasks) != 502 {
		t.Fatalf("board status=%d, tasks=%d, %v", w.Code, len(board.Tasks), err)
	}
	first := board.Tasks[0]
	if !first.InWork || first.LastActorName != "evirma-ser1063" || len(first.People) != 1 || len(first.People[0].Conversations) != 2 || len(first.ExternalRefs) != 1 || first.ExternalRefs[0] != "SER-1063" || string(first.Body) != "null" || string(first.Source) != "null" || first.Markdown != "" {
		t.Fatalf("board summary: %+v", first)
	}
	if len(board.NoTask) != 1 || board.NoTask[0].External != "SER-1064" {
		t.Fatalf("unfiled issues or scope: %+v", board.NoTask)
	}
	request := <-agent.got
	if request.Archive == nil || request.Archive.Profile != "/srv/board-account" || len(request.Archive.Conversations) != 2 {
		t.Fatalf("archive lookup: %+v", request)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_events(project_id,task_number,kind,at,precision,actor,origin) VALUES($1,1,'changed','2025-01-01T12:00:00Z','instant','55555555','panel')`, project); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/projects/%d/tasks/1", project), nil))
	var detail taskreg.Detail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || detail.Task.Markdown == "" || len(detail.Events) != 3 || detail.Events[2].ActorName != "evirma-ser1063" || detail.Events[1].ActorName != "auditor" || len(detail.Task.People) != 2 {
		t.Fatalf("detail: %s, %v", w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/tasks/period?contour=%d&from=2025-01-02&until=2025-01-03", contour), nil))
	var period struct {
		Groups []struct {
			External string `json:"external"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &period); err != nil || len(period.Groups) != 1 || period.Groups[0].External != "SER-1063" {
		t.Fatalf("report grouping: %s, %v", w.Body.String(), err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/tasks/board", 400}, {"/api/tasks/board?contour=99999", 404}} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
}
