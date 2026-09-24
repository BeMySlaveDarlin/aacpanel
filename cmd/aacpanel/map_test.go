package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func tree() []store.Profile {
	return []store.Profile{
		{
			ID: 1, Name: "personal", Launch: json.RawMessage(`{"model":"opus"}`),
			Groups: []store.ProfileGroup{
				{ID: 10, Name: "Services", Projects: []store.ProfileProject{
					{ID: 100, GroupID: 10, Name: "aacpanel", Path: "/srv/proj/Beta/service/aacpanel",
						Launch: json.RawMessage(`{"room":"home"}`)},
					{ID: 101, GroupID: 10, Name: "the main host session", Path: "/home/u", Session: "home"},
				}},
				{ID: 11, Name: "Empty"},
			},
		},
		{
			ID: 2, Name: "work",
			Groups: []store.ProfileGroup{
				{ID: 20, Name: "CLIENT", Projects: []store.ProfileProject{
					{ID: 200, GroupID: 20, Name: "the work contour panel", Path: "/srv/proj/Labs/aacpanel"},
				}},
			},
		},
		{ID: 3, Name: "empty"},
	}
}

func TestProfileMapShape(t *testing.T) {
	out := profileMap(tree())

	if len(out) != 2 {
		t.Fatalf("%d profiles, expected 2 (an empty profile does not go into the tree): %+v", len(out), out)
	}
	if out[0].Profile != "personal" || out[1].Profile != "work" {
		t.Errorf("the top level is not named by profile: %q, %q", out[0].Profile, out[1].Profile)
	}
	if len(out[0].Groups) != 1 {
		t.Errorf("an empty group got into the tree: %+v", out[0].Groups)
	}

	projects := out[0].Groups[0].Projects
	if len(projects) != 2 {
		t.Fatalf("%d projects, expected 2", len(projects))
	}
	if projects[0].Session != "aacpanel" {
		t.Errorf("the session name taken from the directory gave %q", projects[0].Session)
	}
	if projects[1].Session != "home" {
		t.Errorf("the explicit session name was lost: %q — the home session would be named after its directory", projects[1].Session)
	}
	if projects[0].ID != 100 {
		t.Errorf("the project id did not arrive: %+v", projects[0])
	}

	raw, err := json.Marshal(out[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"profile"`, `"groups"`, `"name"`, `"projects"`, `"path"`, `"session"`, `"comment"`, `"id"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("the tree has no %s field: %s", field, raw)
		}
	}
}

func TestLocateProject(t *testing.T) {
	list := tree()

	t.Run("by id", func(t *testing.T) {
		found, err := locateProject(list, 200, "", "", nil, nil)
		if err != nil || found == nil {
			t.Fatalf("the project was not found by id: %v", err)
		}
		if found.project.Path != "/srv/proj/Labs/aacpanel" || found.profile.Name != "work" {
			t.Errorf("the wrong project was found: %+v", found)
		}
	})

	t.Run("an id absent from the map is a refusal, not a search further", func(t *testing.T) {
		if _, err := locateProject(list, 999, "", "aacpanel", nil, nil); err == nil {
			t.Error("an id that does not exist silently led to a search by name")
		}
	})

	t.Run("by working directory", func(t *testing.T) {
		found, err := locateProject(list, 0, "/srv/proj/Labs/aacpanel/", "", nil, nil)
		if err != nil || found == nil {
			t.Fatalf("the project was not found by directory: %v", err)
		}
		if found.project.ID != 200 {
			t.Errorf("by directory project %d was found", found.project.ID)
		}
	})

	t.Run("by session name", func(t *testing.T) {
		found, err := locateProject(list, 0, "", "home", nil, nil)
		if err != nil || found == nil {
			t.Fatalf("the project was not found by session name: %v", err)
		}
		if found.project.ID != 101 {
			t.Errorf("by name project %d was found", found.project.ID)
		}
	})

	t.Run("by directory name", func(t *testing.T) {
		found, err := locateProject(list, 0, "", "u", nil, nil)
		if err != nil || found == nil {
			t.Fatalf("the project was not found by directory name: %v", err)
		}
		if found.project.ID != 101 {
			t.Errorf("by directory name project %d was found", found.project.ID)
		}
	})

	t.Run("an ambiguous name is a refusal with a list", func(t *testing.T) {
		_, err := locateProject(list, 0, "", "aacpanel", nil, nil)
		if err == nil {
			t.Fatal("an ambiguous name opened the first project that turned up")
		}
		for _, want := range []string{"personal", "work"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal %q does not carry profile %q — there is nothing to choose from", err, want)
			}
		}
	})

	roots := []string{"/srv/proj", "/home/u"}
	worktrees := map[string]string{"/srv/proj/Labs/aacpanel-fix": "/srv/proj/Labs/aacpanel"}
	owner := []struct {
		name, cwd string
		want      int
	}{
		{"a worktree kept inside the project", "/srv/proj/Labs/aacpanel/.claude/worktrees/fix", 200},
		{"a directory inside the project", "/srv/proj/Beta/service/aacpanel/web", 100},
		{"a git worktree kept beside the repository", "/srv/proj/Labs/aacpanel-fix", 200},
		{"a directory inside such a worktree", "/srv/proj/Labs/aacpanel-fix/web", 200},
	}
	for _, c := range owner {
		t.Run(c.name+" belongs to its project and is resumed where it ran", func(t *testing.T) {
			found, err := locateProject(list, 0, c.cwd, "fix", worktrees, roots)
			if err != nil || found == nil {
				t.Fatalf("the project of %s was not found: %v", c.cwd, err)
			}
			if found.project.ID != c.want || found.at != c.cwd {
				t.Errorf("project %d at %q, expected %d at %q", found.project.ID, found.at, c.want, c.cwd)
			}
		})
	}

	t.Run("the home directory owns nothing under it", func(t *testing.T) {
		found, err := locateProject(list, 0, "/home/u/.local/state/bench/worktree", "worktree", worktrees, roots)
		if err != nil || found != nil {
			t.Errorf("a directory under home went to %+v (%v) — it would start in the home session's contour", found, err)
		}
	})

	t.Run("not found is not a refusal", func(t *testing.T) {
		found, err := locateProject(list, 0, "", "no-such-name", nil, nil)
		if err != nil || found != nil {
			t.Errorf("a name that was not found gave %v / %+v — a quiet do not know was expected", err, found)
		}
	})
}

func hostServer(t *testing.T, snapshot string) (*Server, string) {
	t.Helper()
	srv, root := profilesServer(t)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(snapshot), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.host = host.NewReader(path)
	return srv, root
}

func hostMap(t *testing.T, srv *Server) []profileNode {
	t.Helper()
	w := httptest.NewRecorder()
	srv.apiHost(w, httptest.NewRequest(http.MethodGet, "/api/host", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var out struct {
		Profiles []profileNode `json:"profileMap"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("the snapshot was not parsed: %v", err)
	}
	return out.Profiles
}

func TestHostMapComesFromDBPG(t *testing.T) {
	const snapshot = `{"at":1}`

	t.Run("an empty map means no field at all", func(t *testing.T) {
		srv, _ := hostServer(t, snapshot)
		if profiles := hostMap(t, srv); len(profiles) != 0 {
			t.Fatalf("with an empty map the snapshot still carries something: %+v", profiles)
		}
	})

	t.Run("a filled map gives the tree", func(t *testing.T) {
		srv, root := hostServer(t, snapshot)
		id := fillMap(t, srv, root)

		profiles := hostMap(t, srv)
		if len(profiles) != 1 {
			t.Fatalf("%d profiles, expected one: %+v", len(profiles), profiles)
		}
		if profiles[0].Profile != "personal" {
			t.Errorf("the top level is named %q, not by profile", profiles[0].Profile)
		}
		if len(profiles[0].Groups) != 1 || len(profiles[0].Groups[0].Projects) != 1 {
			t.Fatalf("the tree is wrong: %+v", profiles)
		}
		got := profiles[0].Groups[0].Projects[0]
		if got.ID != id {
			t.Errorf("project id %d, expected %d — the panel will not be able to ask to open it", got.ID, id)
		}
		if got.Session != "aacpanel" {
			t.Errorf("session name %q — by it the screen matches live sessions with projects", got.Session)
		}
	})
}

func fillMap(t *testing.T, srv *Server, root string) int {
	t.Helper()
	name, dir := "personal", filepath.Join(root, "aacpanel")
	profile, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{
		Name: &name, ConfigDir: &root, Launch: json.RawMessage(`{"model":"opus"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	group := "Services"
	g, err := srv.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &group})
	if err != nil {
		t.Fatal(err)
	}
	project := "aacpanel"
	p, err := srv.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{
		Name: &project, Path: &dir, Launch: json.RawMessage(`{"room":"work"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestSessionOpenCarriesProjectToExecutorPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session aacpanel started"})
	srv.exec, srv.auth = client, &auth.Service{}

	w := post(t, srv, `{"kind":"session.open","target":"aacpanel","params":{"project":`+strconv.Itoa(id)+`}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	got := <-fake.got
	if got.Project == nil {
		t.Fatal("a name without a project went to the executor — there is nothing left on the host to find it with")
	}
	if want := filepath.Join(root, "aacpanel"); got.Project.Path != want {
		t.Errorf("path %q, expected %q", got.Project.Path, want)
	}
	if got.Project.Session != "aacpanel" {
		t.Errorf("session name %q", got.Project.Session)
	}
	var launch map[string]any
	if err := json.Unmarshal(got.Project.Launch, &launch); err != nil {
		t.Fatalf("the launch parameters were not parsed: %v", err)
	}
	if launch["model"] != "opus" {
		t.Errorf("the profile parameters were not poured in: %v", launch)
	}
	if launch["room"] != "work" {
		t.Errorf("the project parameters were lost: %v", launch)
	}

	list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("the action did not reach the action log")
	}
	if raw, _ := json.Marshal(list[0].Params); !strings.Contains(string(raw), filepath.Join(root, "aacpanel")) {
		t.Errorf("the action log has no path: %s", raw)
	}
}

func TestProjectPathIsCheckedBeforeExecutorPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv.exec, srv.auth = client, &auth.Service{}

	if err := srv.db.UseProjectRoots([]string{filepath.Join(t.TempDir(), "other")}); err != nil {
		t.Fatal(err)
	}

	w := post(t, srv, `{"kind":"session.open","target":"aacpanel","params":{"project":`+strconv.Itoa(id)+`}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, expected 400: %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		t.Fatalf("the request went to the executor after all: %+v", got)
	default:
	}
}

func TestProfileMapPutsDefaultProfileFirst(t *testing.T) {
	work := store.Profile{
		ID: 1, Name: "work", Prefix: "/srv/proj/Labs", Sort: 0,
		Groups: []store.ProfileGroup{{ID: 10, Name: "CLIENT", Projects: []store.ProfileProject{
			{ID: 100, GroupID: 10, Name: "the work contour panel", Path: "/srv/proj/Labs/aacpanel"},
		}}},
	}
	other := store.Profile{
		ID: 2, Name: "acme", Prefix: "/srv/proj/Acme", Sort: 1,
		Groups: []store.ProfileGroup{{ID: 20, Name: "Harness", Projects: []store.ProfileProject{
			{ID: 200, GroupID: 20, Name: "harness", Path: "/srv/proj/Acme/harness"},
		}}},
	}
	home := store.Profile{
		ID: 3, Name: "personal", Sort: 2,
		Groups: []store.ProfileGroup{{ID: 30, Name: "Services", Projects: []store.ProfileProject{
			{ID: 300, GroupID: 30, Name: "the main host session", Path: "/home/u", Session: "home"},
		}}},
	}

	list := []store.Profile{work, other, home}
	out := profileMap(list)
	if len(out) != 3 {
		t.Fatalf("%d profiles, expected 3: %+v", len(out), out)
	}
	if out[0].Profile != "personal" {
		t.Errorf("the first profile is %q: sessions whose contour is not recognised land on it, "+
			"and the main host session would go to the wrong profile", out[0].Profile)
	}
	if out[1].Profile != "work" || out[2].Profile != "acme" {
		t.Errorf("the order of the other profiles was rearranged: %q, %q", out[1].Profile, out[2].Profile)
	}
	if list[0].Name != "work" || list[2].Name != "personal" {
		t.Errorf("the map was rearranged in place: %q … %q", list[0].Name, list[2].Name)
	}
}

func TestProfileMapAddressesContourByID(t *testing.T) {
	list := tree()
	list[0].ConfigDir = "/home/u/.claude"
	out := profileMap(list)
	if len(out) == 0 {
		t.Fatal("the tree is empty")
	}
	if out[0].ID != list[0].ID {
		t.Errorf("a node without the id of its map entry: %+v", out[0])
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), ".claude") {
		t.Errorf("the config directory went out into the tree: %s", raw)
	}
}
