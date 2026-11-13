package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/host"
	"aacpanel/internal/taskreg"
)

func TestTaskWriterLookupResolvesMappedDirectoriesAndWorktreesPG(t *testing.T) {
	srv, root := profilesServer(t)
	id := fillMap(t, srv, root)
	dir := filepath.Join(root, "aacpanel")
	worktree := filepath.Join(root, "other-worktree")
	srv.host = host.NewReader(snapshotWith(t, fmt.Sprintf(`{"at":1,"projects":{"at":1,"dirs":[{"path":%q,"kind":"project","git":true,"worktreeOf":%q}]}}`, worktree, dir)))
	mux := srv.routes(srv.localGate())
	lookup := func(cwd string, want int) taskreg.Project {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"cwd": cwd})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/writer", strings.NewReader(string(raw))))
		if w.Code != want {
			t.Fatalf("lookup %s: %d %s", cwd, w.Code, w.Body.String())
		}
		var project taskreg.Project
		if want == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &project); err != nil {
				t.Fatal(err)
			}
		}
		return project
	}
	for _, writer := range []string{taskreg.FilesWriter, taskreg.PanelWriter} {
		if writer == taskreg.PanelWriter {
			if _, err := srv.db.SwitchTaskWriter(t.Context(), id, dir, taskreg.Snapshot{}); err != nil {
				t.Fatal(err)
			}
		}
		for _, cwd := range []string{dir, filepath.Join(dir, "internal"), worktree, filepath.Join(worktree, "src")} {
			if project := lookup(cwd, 200); project.ID != id || project.Name != "aacpanel" || project.Writer != writer {
				t.Fatalf("lookup %s: %+v", cwd, project)
			}
		}
	}
	if project := lookup(filepath.Join(root, "unmapped"), 200); project != (taskreg.Project{}) {
		t.Fatalf("unmapped directory gained a writer: %+v", project)
	}
	lookup("relative/path", 400)
	lookup("", 400)
	public := srv.routes(gate{page: func(f http.HandlerFunc) http.Handler { return f }, stream: func(f http.HandlerFunc) http.Handler { return f }})
	w := httptest.NewRecorder()
	public.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/writer", strings.NewReader(`{"cwd":"/srv/proj"}`)))
	if w.Code != 404 {
		t.Fatalf("writer lookup exposed publicly: %d", w.Code)
	}
}

func TestTaskRegistryDeletionRefusalReachesTheMapAPIPG(t *testing.T) {
	srv, root := profilesServer(t)
	id := fillMap(t, srv, root)
	dir := filepath.Join(root, "aacpanel")
	if _, err := srv.db.SwitchTaskWriter(t.Context(), id, dir, taskreg.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	title := "Keep this task"
	if _, err := srv.db.WriteTask(t.Context(), id, 0, taskreg.Actor{Conversation: "person", Name: "person"}, taskreg.Edit{Title: &title}); err != nil {
		t.Fatal(err)
	}
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	profile := profiles[0]
	group := profile.Groups[0]
	mux := profilesMux(srv)
	for _, path := range []string{fmt.Sprintf("/api/projects/%d", id), fmt.Sprintf("/api/groups/%d?cascade=1", group.ID), fmt.Sprintf("/api/profiles/%d?cascade=1", profile.ID)} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("DELETE", path, nil))
		if w.Code != 409 || !strings.Contains(w.Body.String(), `project "aacpanel" holds 1 tasks and 0 handoffs in the panel`) {
			t.Fatalf("%s hid the refusal: %d %s", path, w.Code, w.Body.String())
		}
	}
}
