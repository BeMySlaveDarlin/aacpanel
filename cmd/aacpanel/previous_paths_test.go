package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/chat"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestArchiveFindsPreviousProjectPaths(t *testing.T) {
	list := []store.Profile{{Groups: []store.ProfileGroup{{Projects: []store.ProfileProject{
		{ID: 1, Path: "/srv/proj/current", PreviousPaths: []string{"/srv/proj/former", "/srv/proj/claimed", "/outside/former"}},
		{ID: 2, Path: "/srv/proj/claimed"},
	}}}}}
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			rows := []chat.ArchiveRow{
				{Agent: agent, CWD: "/srv/proj/former"},
				{Agent: agent, CWD: "/srv/proj/former/.claude/worktrees/fix"},
				{Agent: agent, CWD: "/srv/proj/claimed/subdir"},
				{Agent: agent, CWD: "/srv/proj/former-other"},
				{Agent: agent, CWD: "/srv/proj/sibling/subdir"},
				{Agent: agent, CWD: "/outside/former/subdir"},
			}
			placeRows(rows, list, map[string]string{"/srv/proj/sibling": "/srv/proj/former"}, []string{"/srv/proj"})
			for i, want := range []int{1, 1, 2, 0, 1, 1} {
				got := 0
				if rows[i].Project != nil {
					got = rows[i].Project.ID
				}
				if got != want {
					t.Errorf("archive cwd %s belongs to project %d, want %d", rows[i].CWD, got, want)
				}
			}
		})
	}
}

func TestResumeReplacesPreviousProjectPathPG(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			srv, root := profilesServer(t)
			old, current := filepath.Join(root, "former"), filepath.Join(root, "current")
			previous := []string{old}
			name := "contour"
			profile, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{Name: &name, ConfigDir: &root})
			if err != nil {
				t.Fatal(err)
			}
			group, err := srv.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &name})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := srv.db.CreateProject(t.Context(), group.ID, store.ProjectEdit{Name: &name, Path: &current, PreviousPaths: &previous}); err != nil {
				t.Fatal(err)
			}
			const conversation = "55555555-4444-4333-8222-111111111111"
			cwd := old + "/.claude/worktrees/fix"
			if agent == "claude" {
				hostID, err := srv.db.HostID(t.Context(), srv.hostName)
				if err != nil {
					t.Fatal(err)
				}
				pool, err := srv.db.Pool()
				if err != nil {
					t.Fatal(err)
				}
				testdb.PartitionsBack(t, t.Context(), pool, "sessions_1m", 3*time.Hour)
				if _, err := pool.Exec(t.Context(), `INSERT INTO sessions_1m
                    (bucket,host_id,name,tokens_max,pct_avg,pct_max,messages_max,samples,session_id,cwd)
                    VALUES ($1,$2,'service',1000,10,20,5,6,$3,$4)`,
					time.Now().UTC().Add(-time.Hour).Truncate(time.Minute), hostID, conversation, cwd); err != nil {
					t.Fatal(err)
				}
			} else {
				agent := startAgent(t, map[string]any{"ok": true, "archive": chat.ArchivePage{Rows: []chat.ArchiveRow{{SessionID: conversation, Agent: "codex", CWD: cwd}}}})
				srv.chat = chat.New(agent.path)
			}
			client, fake := startFakeExec(t, action.Response{OK: true, Session: "service"})
			srv.exec, srv.auth = client, &auth.Service{}
			params := map[string]any{"session": conversation}
			if agent == "codex" {
				params["agent"], params["contour"] = "codex", name
			}
			raw, err := json.Marshal(map[string]any{"kind": "session.resume", "target": "service", "params": params})
			if err != nil {
				t.Fatal(err)
			}
			w := post(t, srv, string(raw))
			if w.Code != http.StatusOK {
				t.Fatalf("resume: %d %s", w.Code, w.Body.String())
			}
			got := <-fake.got
			want := current + "/.claude/worktrees/fix"
			if got.ResumeAt == nil || got.ResumeAt.Path != want || got.ResumeAt.Root != current {
				t.Fatalf("resume did not replace the former prefix: %+v", got)
			}
			if agent == "claude" && (got.Project == nil || got.Project.Path != want) {
				t.Fatalf("Claude launch still names its former directory: %+v", got.Project)
			}
			if agent == "codex" && got.Project != nil {
				t.Fatalf("the Codex resume inherited map launch parameters: %+v", got.Project)
			}
		})
	}
}

func TestProjectPreviousPathsAPIPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	c := idOf(t, profilePost(t, mux, "/api/profiles", fmt.Sprintf(`{"name":"contour","configDir":%q}`, root)), "profile")
	g := idOf(t, profilePost(t, mux, fmt.Sprintf("/api/profiles/%d/groups", c), `{"name":"services"}`), "group")
	p := idOf(t, profilePost(t, mux, fmt.Sprintf("/api/groups/%d/projects", g), fmt.Sprintf(`{"name":"service","path":%q}`, root+"/old")), "project")
	for _, tc := range []struct {
		body string
		want []string
		code int
	}{
		{fmt.Sprintf(`{"path":%q}`, root+"/new"), []string{root + "/old"}, 200},
		{`{"previousPaths":["/other/former"]}`, []string{"/other/former"}, 200},
		{`{"previousPaths":[]}`, []string{}, 200},
		{`{"previousPaths":["relative"]}`, nil, 400},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/projects/%d", p), strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatalf("PATCH %s: %d %s", tc.body, w.Code, w.Body.String())
		}
		if tc.code == 200 {
			var response struct {
				Project  store.ProfileProject `json:"project"`
				Profiles []store.Profile      `json:"profiles"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			for _, project := range []store.ProfileProject{response.Project, response.Profiles[0].Groups[0].Projects[0]} {
				got, _ := json.Marshal(project.PreviousPaths)
				want, _ := json.Marshal(tc.want)
				if string(got) != string(want) {
					t.Fatalf("API previous paths %s, want %s", got, want)
				}
			}
		}
	}
}
