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
	"aacpanel/internal/host"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestProfilesWithoutDB(t *testing.T) {
	srv := &Server{hostName: "STAND-01"}
	cases := map[string]struct {
		method string
		h      http.HandlerFunc
	}{
		"GET /api/profiles":                {http.MethodGet, srv.apiProfiles},
		"POST /api/profiles":               {http.MethodPost, srv.apiCreateProfile},
		"PATCH /api/profiles/1":            {http.MethodPatch, srv.apiUpdateProfile},
		"DELETE /api/profiles/1":           {http.MethodDelete, srv.apiDeleteProfile},
		"POST /api/profiles/1/groups":      {http.MethodPost, srv.apiCreateGroup},
		"PATCH /api/groups/1":              {http.MethodPatch, srv.apiUpdateGroup},
		"DELETE /api/groups/1":             {http.MethodDelete, srv.apiDeleteGroup},
		"POST /api/groups/1/projects":      {http.MethodPost, srv.apiCreateProject},
		"PATCH /api/projects/1":            {http.MethodPatch, srv.apiUpdateProject},
		"DELETE /api/projects/1":           {http.MethodDelete, srv.apiDeleteProject},
		"PUT /api/profiles/order":          {http.MethodPut, srv.apiReorderProfiles},
		"PUT /api/profiles/1/groups/order": {http.MethodPut, srv.apiReorderGroups},
		"PUT /api/groups/1/projects/order": {http.MethodPut, srv.apiReorderProjects},
		"POST /api/disk/hidden":            {http.MethodPost, srv.apiHideDir},
		"DELETE /api/disk/hidden":          {http.MethodDelete, srv.apiShowDir},
	}
	for name, c := range cases {
		path := name[strings.Index(name, " ")+1:]
		r := httptest.NewRequest(c.method, path, strings.NewReader(`{}`))
		r.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		c.h(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without a database gave %d, expected 503", name, w.Code)
		}
	}
}

func TestProfilesAPIPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var out map[string]any
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("%s %s: the response is not json: %s", method, path, w.Body.String())
			}
		}
		return w.Code, out
	}

	t.Run("an empty map is a list, not null", func(t *testing.T) {
		code, body := call(http.MethodGet, "/api/profiles", "")
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		list := treeOf(t, body)
		if list == nil {
			t.Error("an empty map came as null: .length of nothing breaks the render")
		}
	})

	var profileID, groupID, projectID int
	t.Run("a profile is created", func(t *testing.T) {
		code, body := call(http.MethodPost, "/api/profiles",
			`{"name":"personal","configDir":"`+root+`","launch":{"model":"opus"}}`)
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		profileID = idOf(t, body, "profile")
		if got := body["profile"].(map[string]any)["groups"]; got == nil {
			t.Error("the created profile has groups as null, not []")
		}
		if len(treeOf(t, body)) != 1 {
			t.Errorf("the create response carries no updated map: %v", body["profiles"])
		}
	})

	t.Run("a namesake is rejected with 409", func(t *testing.T) {
		code, _ := call(http.MethodPost, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)
		if code != http.StatusConflict {
			t.Errorf("status %d, expected 409: by it the panel marks the field as a taken name", code)
		}
	})

	t.Run("a contour directory outside the project roots is accepted", func(t *testing.T) {
		code, _ := call(http.MethodPost, "/api/profiles", `{"name":"outside roots","configDir":"/etc"}`)
		if code != http.StatusOK {
			t.Errorf("status %d, expected 200: the contour directory lies where claude created it, not in the project tree", code)
		}
	})

	t.Run("a contour directory that is not absolute — 400", func(t *testing.T) {
		code, _ := call(http.MethodPost, "/api/profiles", `{"name":"relative","configDir":"claude-home"}`)
		if code != http.StatusBadRequest {
			t.Errorf("status %d, expected 400: a relative path would resolve against the working directory of the service, and in the container it has its own", code)
		}
	})

	t.Run("a contour directory with a newline — 400", func(t *testing.T) {
		code, _ := call(http.MethodPost, "/api/profiles", `{"name":"newline","configDir":"/home/u/.claude\nrm -rf /"}`)
		if code != http.StatusBadRequest {
			t.Errorf("status %d, expected 400", code)
		}
	})

	t.Run("an unknown field is a typo, not silence", func(t *testing.T) {
		code, _ := call(http.MethodPatch, "/api/profiles/"+strconv.Itoa(profileID), `{"prefiks":"/opt"}`)
		if code != http.StatusBadRequest {
			t.Errorf("status %d, expected 400: otherwise a typo looks like a button that does not work", code)
		}
	})

	t.Run("no such profile — 404", func(t *testing.T) {
		code, _ := call(http.MethodPatch, "/api/profiles/"+strconv.Itoa(profileID+1000), `{"name":"none"}`)
		if code != http.StatusNotFound {
			t.Errorf("status %d, expected 404", code)
		}
	})

	t.Run("a group and a project are created", func(t *testing.T) {
		code, body := call(http.MethodPost, "/api/profiles/"+strconv.Itoa(profileID)+"/groups", `{"name":"services"}`)
		if code != http.StatusOK {
			t.Fatalf("group: status %d", code)
		}
		groupID = idOf(t, body, "group")

		code, _ = call(http.MethodPost, "/api/groups/"+strconv.Itoa(groupID)+"/projects",
			`{"name":"foreign","path":"/etc/passwd"}`)
		if code != http.StatusBadRequest {
			t.Errorf("a project with a path outside the roots gave %d, expected 400", code)
		}

		code, body = call(http.MethodPost, "/api/groups/"+strconv.Itoa(groupID)+"/projects",
			`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`"}`)
		if code != http.StatusOK {
			t.Fatalf("project: status %d", code)
		}
		projectID = idOf(t, body, "project")
	})

	t.Run("the map comes as a tree", func(t *testing.T) {
		_, body := call(http.MethodGet, "/api/profiles", "")
		p := treeOf(t, body)[0].(map[string]any)
		groups := p["groups"].([]any)
		if len(groups) != 1 {
			t.Fatalf("%d groups, expected one", len(groups))
		}
		projects := groups[0].(map[string]any)["projects"].([]any)
		if len(projects) != 1 {
			t.Fatalf("%d projects, expected one", len(projects))
		}
		if got := projects[0].(map[string]any)["path"]; got != filepath.Join(root, "aacpanel") {
			t.Errorf("project path %v", got)
		}
	})

	t.Run("a non-empty entry is not deleted", func(t *testing.T) {
		if code, _ := call(http.MethodDelete, "/api/profiles/"+strconv.Itoa(profileID), ""); code != http.StatusConflict {
			t.Errorf("a profile with a project was deleted with %d, expected 409", code)
		}
		if code, _ := call(http.MethodDelete, "/api/groups/"+strconv.Itoa(groupID), ""); code != http.StatusConflict {
			t.Errorf("a group with a project was deleted with %d, expected 409", code)
		}
	})

	t.Run("it is dismantled bottom up", func(t *testing.T) {
		for _, path := range []string{
			"/api/projects/" + strconv.Itoa(projectID),
			"/api/groups/" + strconv.Itoa(groupID),
			"/api/profiles/" + strconv.Itoa(profileID),
		} {
			code, body := call(http.MethodDelete, path, "")
			if code != http.StatusOK {
				t.Fatalf("DELETE %s: status %d", path, code)
			}
			if _, ok := body["profiles"]; !ok {
				t.Errorf("DELETE %s returned no map: the screen will not learn what happened", path)
			}
			_ = treeOf(t, body)
		}
	})
}

func TestProfileModelsComeFromSnapshotPG(t *testing.T) {
	srv, root := profilesServer(t)
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"models":{"at":10,"contours":[
		{"profile":"personal","state":"ok","at":9,"models":[
			{"id":"claude-opus-5","name":"Claude Opus 5","window":1000000,"output":128000}
		]},
		{"profile":"work","state":"unknown","error":"no-credentials"}
	]}}`))
	mux := profilesMux(srv)
	profilePost(t, mux, "/api/profiles", `{"name":"work","configDir":"`+root+`"}`)

	body := profileGet(t, mux, "/api/profiles")
	catalog, ok := body["models"].(map[string]any)
	if !ok {
		t.Fatalf("the response has no model catalog: %v", body)
	}
	if catalog["state"] != "ok" {
		t.Fatalf("the catalog is unread although one contour returned it: %v", catalog)
	}
	rows, ok := catalog["models"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("the models did not arrive: %v", catalog)
	}
	row := rows[0].(map[string]any)
	if row["name"] != "Claude Opus 5" || row["window"] != float64(1000000) {
		t.Errorf("the model name or the window was lost on the way: %v", row)
	}
}

func TestProfileModelsUnknownWhenNobodyHasItPG(t *testing.T) {
	srv, root := profilesServer(t)
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"models":{"contours":[
		{"profile":"personal","state":"unknown","error":"no-credentials"}
	]}}`))
	mux := profilesMux(srv)
	profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)

	catalog, ok := profileGet(t, mux, "/api/profiles")["models"].(map[string]any)
	if !ok {
		t.Fatalf("there is no catalog field at all: the panel will not tell unread from empty")
	}
	if catalog["state"] != "unknown" {
		t.Errorf("the catalog state is not unknown: %v", catalog)
	}
	if _, ok := catalog["models"]; ok {
		t.Errorf("an unread catalog still brought a list of models: %v", catalog)
	}
}

func TestProfileAuthComesFromSnapshotPG(t *testing.T) {
	srv, root := profilesServer(t)
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"profiles":[
		{"name":"personal","auth":"builtin"},
		{"name":"work","auth":"token"},
		{"name":"acme","auth":"missing"}
	]}`))
	mux := profilesMux(srv)

	for _, name := range []string{"personal", "work", "acme", "unlisted"} {
		profilePost(t, mux, "/api/profiles", `{"name":"`+name+`","configDir":"`+root+`"}`)
	}

	t.Run("three states reach the tree", func(t *testing.T) {
		body := profileGet(t, mux, "/api/profiles")
		want := map[string]string{
			"personal": "builtin", "work": "token", "acme": "missing", "unlisted": "",
		}
		for _, item := range treeOf(t, body) {
			p := item.(map[string]any)
			name := p["name"].(string)
			got, _ := p["auth"].(string)
			if got != want[name] {
				t.Errorf("contour %s: auth %q, expected %q", name, got, want[name])
			}
		}
	})

	t.Run("the created profile has the same shape as in the tree", func(t *testing.T) {
		body := profilePost(t, mux, "/api/profiles", `{"name":"work-2","configDir":"`+root+`"}`)
		p, ok := body["profile"].(map[string]any)
		if !ok {
			t.Fatalf("the response has no profile: %v", body)
		}
		if _, ok := p["auth"]; !ok {
			t.Error("the created profile has no auth field at all")
		}
	})

	t.Run("the state of a known contour is in the echo too", func(t *testing.T) {
		var id int
		for _, item := range treeOf(t, profileGet(t, mux, "/api/profiles")) {
			p := item.(map[string]any)
			if p["name"] == "work" {
				id = int(p["id"].(float64))
			}
		}
		if id == 0 {
			t.Fatal("the work profile was not found")
		}
		body := profilePatch(t, mux, "/api/profiles/"+strconv.Itoa(id), `{"sort":7}`)
		if got := body["profile"].(map[string]any)["auth"]; got != "token" {
			t.Errorf("the update echo has auth %v, expected token", got)
		}
	})
}

func TestReorderAPIPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	profilePost(t, mux, "/api/profiles", `{"name":"a","configDir":"`+root+`"}`)
	profilePost(t, mux, "/api/profiles", `{"name":"b","configDir":"`+root+`"}`)

	before := treeOf(t, profileGet(t, mux, "/api/profiles"))
	if len(before) != 2 {
		t.Fatalf("%d profiles, expected 2", len(before))
	}
	aID := int(before[0].(map[string]any)["id"].(float64))
	bID := int(before[1].(map[string]any)["id"].(float64))

	after := treeOf(t, profilePut(t, mux, "/api/profiles/order",
		`{"ids":[`+strconv.Itoa(bID)+`,`+strconv.Itoa(aID)+`]}`))
	if got := int(after[0].(map[string]any)["id"].(float64)); got != bID {
		t.Fatalf("after the reorder the first id is %d, expected %d (b)", got, bID)
	}

	r := httptest.NewRequest(http.MethodPut, "/api/profiles/order",
		strings.NewReader(`{"ids":[`+strconv.Itoa(aID)+`]}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an incomplete list gave %d, expected 400: %s", w.Code, w.Body.String())
	}
}

func TestProfileAuthSilentWithoutSnapshotPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)

	t.Run("without a host reader", func(t *testing.T) {
		p := treeOf(t, profileGet(t, mux, "/api/profiles"))[0].(map[string]any)
		if got := p["auth"]; got != "" {
			t.Errorf("auth %v, expected empty", got)
		}
	})

	t.Run("there is no snapshot file", func(t *testing.T) {
		srv.host = host.NewReader(filepath.Join(t.TempDir(), "missing.json"))
		p := treeOf(t, profileGet(t, mux, "/api/profiles"))[0].(map[string]any)
		if got := p["auth"]; got != "" {
			t.Errorf("auth %v, expected empty", got)
		}
	})
}

func snapshotWith(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func profileGet(t *testing.T, mux *http.ServeMux, path string) map[string]any {
	t.Helper()
	return profileCall(t, mux, http.MethodGet, path, "")
}

func profilePost(t *testing.T, mux *http.ServeMux, path, body string) map[string]any {
	t.Helper()
	return profileCall(t, mux, http.MethodPost, path, body)
}

func profilePatch(t *testing.T, mux *http.ServeMux, path, body string) map[string]any {
	t.Helper()
	return profileCall(t, mux, http.MethodPatch, path, body)
}

func profilePut(t *testing.T, mux *http.ServeMux, path, body string) map[string]any {
	t.Helper()
	return profileCall(t, mux, http.MethodPut, path, body)
}

func profileCall(t *testing.T, mux *http.ServeMux, method, path, body string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: status %d, body %s", method, path, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: the response is not json: %s", method, path, w.Body.String())
	}
	return out
}

func TestEveryProfileRouteRegistered(t *testing.T) {
	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range profileRoutes {
		if !strings.Contains(string(src), `"`+route.pattern+`"`) {
			t.Errorf("route %q is not registered in routes.go", route.pattern)
		}
	}
}

var profileRoutes = []struct {
	pattern string
	handler func(*Server) http.HandlerFunc
}{
	{"GET /api/profiles", func(s *Server) http.HandlerFunc { return s.apiProfiles }},
	{"GET /api/profiles/schema", func(s *Server) http.HandlerFunc { return s.apiProfilesSchema }},
	{"POST /api/profiles", func(s *Server) http.HandlerFunc { return s.apiCreateProfile }},
	{"PATCH /api/profiles/{id}", func(s *Server) http.HandlerFunc { return s.apiUpdateProfile }},
	{"DELETE /api/profiles/{id}", func(s *Server) http.HandlerFunc { return s.apiDeleteProfile }},
	{"PUT /api/profiles/order", func(s *Server) http.HandlerFunc { return s.apiReorderProfiles }},
	{"POST /api/profiles/{id}/groups", func(s *Server) http.HandlerFunc { return s.apiCreateGroup }},
	{"PUT /api/profiles/{id}/groups/order", func(s *Server) http.HandlerFunc { return s.apiReorderGroups }},
	{"PATCH /api/groups/{id}", func(s *Server) http.HandlerFunc { return s.apiUpdateGroup }},
	{"DELETE /api/groups/{id}", func(s *Server) http.HandlerFunc { return s.apiDeleteGroup }},
	{"POST /api/groups/{id}/projects", func(s *Server) http.HandlerFunc { return s.apiCreateProject }},
	{"PUT /api/groups/{id}/projects/order", func(s *Server) http.HandlerFunc { return s.apiReorderProjects }},
	{"PATCH /api/projects/{id}", func(s *Server) http.HandlerFunc { return s.apiUpdateProject }},
	{"DELETE /api/projects/{id}", func(s *Server) http.HandlerFunc { return s.apiDeleteProject }},
	{"POST /api/disk/hidden", func(s *Server) http.HandlerFunc { return s.apiHideDir }},
	{"DELETE /api/disk/hidden", func(s *Server) http.HandlerFunc { return s.apiShowDir }},
}

func profilesMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	for _, route := range profileRoutes {
		mux.HandleFunc(route.pattern, route.handler(s))
	}
	return mux
}

func profilesServer(t *testing.T) (*Server, string) {
	t.Helper()
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "Projects")
	if err := os.MkdirAll(filepath.Join(root, "aacpanel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := db.UseProjectRoots([]string{root}); err != nil {
		t.Fatal(err)
	}

	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"profile_projects", "profile_groups", "profiles"} {
		if _, err := pool.Exec(t.Context(), "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	exec, _ := startFakeExec(t, action.Response{OK: true})
	return &Server{db: db, exec: exec, hostName: "STAND-01"}, root
}

func treeOf(t *testing.T, body map[string]any) []any {
	t.Helper()
	list, ok := body["profiles"].([]any)
	if !ok {
		t.Fatalf("the response has no profiles field: %v", body)
	}
	return list
}

func idOf(t *testing.T, body map[string]any, field string) int {
	t.Helper()
	obj, ok := body[field].(map[string]any)
	if !ok {
		t.Fatalf("the response has no %q field: %v", field, body)
	}
	id, ok := obj["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("%q carries no id: %v", field, obj)
	}
	return int(id)
}

func TestProjectMoveGoesThroughTheProjectPatchPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	own := profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)
	ownID := idOf(t, own, "profile")
	other := profilePost(t, mux, "/api/profiles",
		`{"name":"work","configDir":"`+filepath.Join(root, "aacpanel")+`"}`)
	otherID := idOf(t, other, "profile")

	from := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(ownID)+"/groups", `{"name":"Services"}`), "group")
	to := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(ownID)+"/groups", `{"name":"Personal"}`), "group")
	foreign := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(otherID)+"/groups", `{"name":"CLIENT"}`), "group")

	project := idOf(t, profilePost(t, mux, "/api/groups/"+strconv.Itoa(from)+"/projects",
		`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`"}`), "project")
	path := "/api/projects/" + strconv.Itoa(project)

	t.Run("a move to a neighbouring group", func(t *testing.T) {
		body := profilePatch(t, mux, path, `{"groupId":`+strconv.Itoa(to)+`}`)
		if got := int(body["project"].(map[string]any)["groupId"].(float64)); got != to {
			t.Errorf("the project stayed in group %d, expected %d", got, to)
		}
		if where := groupOfProject(t, treeOf(t, body), project); where != to {
			t.Errorf("in the map from the response the project lies in group %d, expected %d", where, to)
		}
	})

	t.Run("a foreign contour — 400, not a silent move", func(t *testing.T) {
		code, _ := profileCode(t, mux, http.MethodPatch, path, `{"groupId":`+strconv.Itoa(foreign)+`}`)
		if code != http.StatusBadRequest {
			t.Errorf("a move into a foreign contour gave %d, expected 400: the contour decides "+
				"which token the session starts with", code)
		}
		body := profileGet(t, mux, "/api/profiles")
		if where := groupOfProject(t, treeOf(t, body), project); where != to {
			t.Errorf("after the refusal the project lies in group %d, and it lay in %d", where, to)
		}
	})

	t.Run("no such group — 404", func(t *testing.T) {
		code, _ := profileCode(t, mux, http.MethodPatch, path, `{"groupId":`+strconv.Itoa(foreign+1000)+`}`)
		if code != http.StatusNotFound {
			t.Errorf("a move into a group that does not exist gave %d, expected 404", code)
		}
	})
}

func profileCode(t *testing.T, mux *http.ServeMux, method, path, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func groupOfProject(t *testing.T, tree []any, id int) int {
	t.Helper()
	for _, p := range tree {
		for _, g := range p.(map[string]any)["groups"].([]any) {
			group := g.(map[string]any)
			for _, r := range group["projects"].([]any) {
				if int(r.(map[string]any)["id"].(float64)) == id {
					return int(group["id"].(float64))
				}
			}
		}
	}
	t.Fatalf("project %d is not in the map at all", id)
	return 0
}

func profileOfGroup(t *testing.T, tree []any, id int) int {
	t.Helper()
	for _, raw := range tree {
		p := raw.(map[string]any)
		for _, g := range p["groups"].([]any) {
			if int(g.(map[string]any)["id"].(float64)) == id {
				return int(p["id"].(float64))
			}
		}
	}
	t.Fatalf("group %d is not in the map at all", id)
	return 0
}

func TestContourMoveNeedsConsentPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	ownID := idOf(t, profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`), "profile")
	otherID := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"work","configDir":"`+filepath.Join(root, "aacpanel")+`"}`), "profile")

	from := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(ownID)+"/groups", `{"name":"Services"}`), "group")
	foreign := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(otherID)+"/groups", `{"name":"Backend"}`), "group")

	project := idOf(t, profilePost(t, mux, "/api/groups/"+strconv.Itoa(from)+"/projects",
		`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`"}`), "project")
	path := "/api/projects/" + strconv.Itoa(project)

	for _, tc := range []struct {
		what string
		body string
	}{
		{"without consent", `{"groupId":` + strconv.Itoa(foreign) + `}`},
		{"with moveProfile consent", `{"groupId":` + strconv.Itoa(foreign) + `,"moveProfile":true}`},
		{"with moveContour consent", `{"groupId":` + strconv.Itoa(foreign) + `,"moveContour":true}`},
	} {
		t.Run("a project is refused a foreign group "+tc.what, func(t *testing.T) {
			code, body := profileCode(t, mux, http.MethodPatch, path, tc.body)
			if code != http.StatusBadRequest {
				t.Errorf("a project move into a foreign contour gave %d, expected 400: the group "+
					"exists, and 404 would send one looking for a typo in the id", code)
			}
			for _, consent := range []string{"moveProfile", "moveContour"} {
				if strings.Contains(body, consent) {
					t.Errorf("the refusal names our own field (%q) instead of the profile: %s", consent, body)
				}
			}
			if !strings.Contains(body, "profile") {
				t.Errorf("the refusal does not name the reason: %s", body)
			}
		})
	}

	t.Run("the project stayed in its own group", func(t *testing.T) {
		body := profilePatch(t, mux, path, `{"name":"aacpanel"}`)
		if where := groupOfProject(t, treeOf(t, body), project); where != from {
			t.Errorf("after the refusals the project lies in group %d, and it lay in %d", where, from)
		}
	})

	t.Run("a group moves together with its projects", func(t *testing.T) {
		gpath := "/api/groups/" + strconv.Itoa(from)
		code, _ := profileCode(t, mux, http.MethodPatch, gpath, `{"profileId":`+strconv.Itoa(otherID)+`}`)
		if code != http.StatusBadRequest {
			t.Errorf("a group move without consent gave %d, expected 400", code)
		}

		body := profilePatch(t, mux, gpath,
			`{"profileId":`+strconv.Itoa(otherID)+`,"moveProfile":true}`)
		if where := groupOfProject(t, treeOf(t, body), project); where != from {
			t.Errorf("the project came off its group during the move: it lies in %d", where)
		}
		for _, raw := range treeOf(t, body) {
			p := raw.(map[string]any)
			if int(p["id"].(float64)) != otherID {
				continue
			}
			for _, g := range p["groups"].([]any) {
				if int(g.(map[string]any)["id"].(float64)) == from {
					return
				}
			}
		}
		t.Error("the group did not arrive in the target contour")
	})
}

func TestGroupMoveConsentAnswersToBothNamesPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	ownID := idOf(t, profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`), "profile")
	otherID := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"work","configDir":"`+filepath.Join(root, "aacpanel")+`"}`), "profile")
	group := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(ownID)+"/groups", `{"name":"Services"}`), "group")

	gpath := "/api/groups/" + strconv.Itoa(group)
	for _, tc := range []struct {
		consent string
		to      int
	}{
		{"moveProfile", otherID},
		{"moveContour", ownID},
	} {
		t.Run(tc.consent, func(t *testing.T) {
			body := profilePatch(t, mux, gpath,
				`{"profileId":`+strconv.Itoa(tc.to)+`,"`+tc.consent+`":true}`)
			if where := profileOfGroup(t, treeOf(t, body), group); where != tc.to {
				t.Errorf("with consent %q the group lies in profile %d, expected %d", tc.consent, where, tc.to)
			}
		})
	}
}

func TestCascadeNeedsExplicitFlagPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	del := func(path string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}

	profileID := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"contour","configDir":"`+root+`"}`), "profile")
	groupID := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(profileID)+"/groups",
		`{"name":"group"}`), "group")
	profilePost(t, mux, "/api/groups/"+strconv.Itoa(groupID)+"/projects",
		`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`"}`)

	gpath := "/api/groups/" + strconv.Itoa(groupID)
	if code := del(gpath); code != http.StatusConflict {
		t.Fatalf("a group with a project was deleted without the flag with %d, expected 409", code)
	}
	for _, q := range []string{"?cascade=true", "?cascade=yes", "?cascade=0", "?cascade="} {
		if code := del(gpath + q); code != http.StatusConflict {
			t.Errorf("%q was read as consent: status %d, expected 409", q, code)
		}
	}
	if code := del(gpath + "?cascade=1"); code != http.StatusOK {
		t.Fatalf("a cascade with the flag gave %d", code)
	}

	second := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(profileID)+"/groups",
		`{"name":"second"}`), "group")
	profilePost(t, mux, "/api/groups/"+strconv.Itoa(second)+"/projects",
		`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`"}`)

	ppath := "/api/profiles/" + strconv.Itoa(profileID)
	if code := del(ppath); code != http.StatusConflict {
		t.Fatalf("a contour with a project was deleted without the flag with %d, expected 409", code)
	}
	if code := del(ppath + "?cascade=1"); code != http.StatusOK {
		t.Fatalf("a contour cascade with the flag gave %d", code)
	}
	if list := treeOf(t, profileGet(t, mux, "/api/profiles")); len(list) != 0 {
		t.Errorf("%d contours are left on the map after the cascade", len(list))
	}
}

func TestProfileStateFollowsConfigDirPG(t *testing.T) {
	srv, root := profilesServer(t)
	work := filepath.Join(root, "work")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"profiles":[
		{"name":"personal","configDir":"`+root+`","auth":"builtin"},
		{"name":"work","configDir":"`+work+`","auth":"token","hooks":"diverged"}
	]}`))
	mux := profilesMux(srv)

	profilePost(t, mux, "/api/profiles", `{"name":"mine","configDir":"`+root+`/"}`)
	profilePost(t, mux, "/api/profiles", `{"name":"renamed","configDir":"`+work+`"}`)
	profilePost(t, mux, "/api/profiles", `{"name":"work","configDir":"`+filepath.Join(root, "elsewhere")+`"}`)

	want := map[string][2]string{
		"mine":    {"builtin", ""},
		"renamed": {"token", "diverged"},
		"work":    {"", ""},
	}
	for _, item := range treeOf(t, profileGet(t, mux, "/api/profiles")) {
		p := item.(map[string]any)
		name := p["name"].(string)
		auth, _ := p["auth"].(string)
		hooks, _ := p["hooks"].(string)
		if auth != want[name][0] || hooks != want[name][1] {
			t.Errorf("contour %s: auth %q, hooks %q; expected %q and %q",
				name, auth, hooks, want[name][0], want[name][1])
		}
	}
}

func TestProfileStateFallsBackToNameWithoutConfigDirPG(t *testing.T) {
	srv, root := profilesServer(t)
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"profiles":[
		{"name":"personal","auth":"builtin"}
	]}`))
	mux := profilesMux(srv)
	profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)

	p := treeOf(t, profileGet(t, mux, "/api/profiles"))[0].(map[string]any)
	if got := p["auth"]; got != "builtin" {
		t.Errorf("auth %v, expected builtin: without directories in the snapshot the name decides", got)
	}
}

func TestLimitsCarryContourNumberPG(t *testing.T) {
	srv, root := profilesServer(t)
	work := filepath.Join(root, "work")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"limits":{
		"profile":"personal","configDir":"`+root+`","fiveHour":{"pct":12},
		"contours":[
			{"profile":"personal","configDir":"`+root+`","fiveHour":{"pct":12}},
			{"profile":"work","configDir":"`+work+`","fiveHour":{"pct":3}},
			{"profile":"acme","configDir":"`+filepath.Join(root, "acme")+`","fiveHour":{"pct":7}}
		]}}`))
	mux := profilesMux(srv)
	personal := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`"}`), "profile")
	working := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"renamed","configDir":"`+work+`"}`), "profile")

	limits := hostLimits(t, srv)
	rows, ok := limits["contours"].([]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("the limits carry not three contours: %v", limits["contours"])
	}
	by := map[string]map[string]any{}
	for _, row := range rows {
		c, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("a limits row is not an object: %v", row)
		}
		by[c["profile"].(string)] = c
	}

	if got := by["work"]["contour"]; got != float64(working) {
		t.Errorf("the renamed contour has id %v, expected %d — by name the panel "+
			"matches it with the map only until the first rename", got, working)
	}
	if got := by["personal"]["contour"]; got != float64(personal) {
		t.Errorf("the personal contour has id %v, expected %d", got, personal)
	}
	if _, has := by["acme"]["contour"]; has {
		t.Errorf("a contour outside the map was given an id: %v", by["acme"])
	}
	if got := limits["contour"]; got != float64(personal) {
		t.Errorf("the limits root carries id %v, expected %d", got, personal)
	}

	raw, err := json.Marshal(limits)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "configDir") || strings.Contains(string(raw), root) {
		t.Errorf("the limits still carry the address of the contour on the host: %s", raw)
	}
}

func TestLimitsWithoutConfigDirStayAsTheyArePG(t *testing.T) {
	srv, root := profilesServer(t)
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"limits":{
		"profile":"personal","fiveHour":{"pct":12},
		"contours":[{"profile":"personal","fiveHour":{"pct":12}}]}}`))
	mux := profilesMux(srv)
	profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)

	limits := hostLimits(t, srv)
	rows, ok := limits["contours"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("the limits carry not one contour: %v", limits["contours"])
	}
	c := rows[0].(map[string]any)
	if c["profile"] != "personal" {
		t.Errorf("the contour name was lost: %v", c)
	}
	if _, has := c["contour"]; has {
		t.Errorf("an id was given to a contour the collector did not name by directory: %v", c)
	}
}

func TestSessionsCarryContourNumberPG(t *testing.T) {
	srv, root := profilesServer(t)
	work := filepath.Join(root, "work")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[
		{"session":"home","sessionId":"a1","profile":"personal","configDir":"`+root+`"},
		{"session":"shop","sessionId":"b2","profile":"work","configDir":"`+work+`"},
		{"session":"gost","sessionId":"c3","profile":"acme","configDir":"`+filepath.Join(root, "acme")+`"},
		{"session":"old","sessionId":"d4","profile":"personal"}
	]}`))
	mux := profilesMux(srv)
	personal := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`"}`), "profile")
	working := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"renamed","configDir":"`+work+`"}`), "profile")

	by := map[string]map[string]any{}
	for _, row := range hostSessions(t, srv) {
		c, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("a session is not an object: %v", row)
		}
		by[c["session"].(string)] = c
	}

	if got := by["shop"]["contour"]; got != float64(working) {
		t.Errorf("the session of the renamed contour has id %v, expected %d — "+
			"by name it will not find its own page any more", got, working)
	}
	if got := by["home"]["contour"]; got != float64(personal) {
		t.Errorf("the personal session has id %v, expected %d", got, personal)
	}
	if _, has := by["gost"]["contour"]; has {
		t.Errorf("a session of a contour outside the map was given an id: %v", by["gost"])
	}
	if _, has := by["old"]["contour"]; has {
		t.Errorf("a session from the old collector was given an id: %v", by["old"])
	}

	if raw := hostBody(t, srv); strings.Contains(raw, root) {
		t.Errorf("the sessions still carry the address of the contour on the host: %s", raw)
	}
}

func hostSessions(t *testing.T, srv *Server) []any {
	t.Helper()
	var out struct {
		Sessions []any `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(hostBody(t, srv)), &out); err != nil {
		t.Fatalf("the snapshot was not parsed: %v", err)
	}
	if len(out.Sessions) == 0 {
		t.Fatal("the snapshot has no sessions")
	}
	return out.Sessions
}

func hostBody(t *testing.T, srv *Server) string {
	t.Helper()
	w := httptest.NewRecorder()
	srv.apiHost(w, httptest.NewRequest(http.MethodGet, "/api/host", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func hostLimits(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	srv.apiHost(w, httptest.NewRequest(http.MethodGet, "/api/host", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var out struct {
		Limits map[string]any `json:"limits"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("the snapshot was not parsed: %v", err)
	}
	if out.Limits == nil {
		t.Fatalf("the snapshot has no limits: %s", w.Body.String())
	}
	return out.Limits
}

func TestPersonalProfileKeepsItsNamePG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	personal := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`"}`), "profile")
	other := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"work","configDir":"`+filepath.Join(root, "work")+`"}`), "profile")

	t.Run("a rename is rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPatch, "/api/profiles/"+strconv.Itoa(personal),
			strings.NewReader(`{"name":"mine"}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusConflict {
			t.Fatalf("status %d, expected 409: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "personal") {
			t.Errorf("the refusal does not name the contour: %s", w.Body.String())
		}
	})

	t.Run("the name stayed in place", func(t *testing.T) {
		for _, item := range treeOf(t, profileGet(t, mux, "/api/profiles")) {
			p := item.(map[string]any)
			if int(p["id"].(float64)) == personal && p["name"] != "personal" {
				t.Errorf("the name of the personal contour became %v", p["name"])
			}
		}
	})

	t.Run("the other fields are editable", func(t *testing.T) {
		profilePatch(t, mux, "/api/profiles/"+strconv.Itoa(personal), `{"sort":3}`)
		profilePatch(t, mux, "/api/profiles/"+strconv.Itoa(personal), `{"name":"personal"}`)
	})

	t.Run("another name is not locked", func(t *testing.T) {
		profilePatch(t, mux, "/api/profiles/"+strconv.Itoa(other), `{"name":"job"}`)
	})
}

func TestContourPicksTurnMapIDIntoAddressPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	conf := filepath.Join(root, ".claude")
	withDir := idOf(t, profilePost(t, mux, "/api/profiles",
		`{"name":"personal","configDir":"`+conf+`"}`), "profile")

	t.Run("the directory when it is there", func(t *testing.T) {
		got, err := srv.contourPicks(t.Context(), []string{strconv.Itoa(withDir)})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != conf {
			t.Errorf("got %v, expected the directory %q", got, conf)
		}
	})

	t.Run("an unknown id is a refusal, not the personal contour", func(t *testing.T) {
		if _, err := srv.contourPicks(t.Context(), []string{strconv.Itoa(withDir + 1000)}); err == nil {
			t.Error("the id is not in the map, and there is no refusal")
		}
		if _, err := srv.contourPicks(t.Context(), []string{"personal"}); err == nil {
			t.Error("a name instead of an id passed as an id")
		}
	})

	t.Run("without ids it asks nothing", func(t *testing.T) {
		got, err := srv.contourPicks(t.Context(), nil)
		if err != nil || got != nil {
			t.Errorf("got %v, %v; expected empty and no error", got, err)
		}
	})
}

func TestProjectAddMakesTheDirectoryTogetherWithTheMapEntryPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	profile := idOf(t, profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`), "profile")
	group := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"Personal"}`), "group")
	projects := "/api/groups/" + strconv.Itoa(group) + "/projects"

	t.Run("the directory is asked of the host", func(t *testing.T) {
		exec, fake := startFakeExec(t, action.Response{OK: true})
		srv.exec = exec

		body := profilePost(t, mux, projects, `{"name":"panel","path":"`+filepath.Join(root, "panel")+`/"}`)
		id := idOf(t, body, "project")

		var req action.Request
		select {
		case req = <-fake.got:
		default:
			t.Fatal("nothing went to the host — there is nobody to create the directory")
		}
		if req.Kind != action.ProjectCreate {
			t.Errorf("action %q went to the host, expected %q", req.Kind, action.ProjectCreate)
		}
		if want := filepath.Join(root, "panel"); req.Target != want {
			t.Errorf("path %q went to the host, and the map holds %q", req.Target, want)
		}
		if got := body["project"].(map[string]any)["path"].(string); got != req.Target {
			t.Errorf("the map has path %q, and %q went to the host", got, req.Target)
		}
		if where := groupOfProject(t, treeOf(t, body), id); where != group {
			t.Errorf("the project landed in group %d, expected %d", where, group)
		}
	})

	t.Run("the host refused — no entry is left", func(t *testing.T) {
		exec, _ := startFakeExec(t, action.Response{OK: false, Error: "/srv/proj/panel is not a directory"})
		srv.exec = exec

		code, text := profileCode(t, mux, http.MethodPost, projects,
			`{"name":"taken","path":"`+filepath.Join(root, "taken")+`"}`)
		if code == http.StatusOK {
			t.Fatal("the host refusal was taken for success")
		}
		if !strings.Contains(text, "not a directory") {
			t.Errorf("the response %q does not name the reason for the refusal", strings.TrimSpace(text))
		}
		if !strings.Contains(text, "rolled back") {
			t.Errorf("the response %q does not say that no entry is left", strings.TrimSpace(text))
		}
		for _, p := range treeOf(t, profileGet(t, mux, "/api/profiles")) {
			for _, g := range p.(map[string]any)["groups"].([]any) {
				for _, r := range g.(map[string]any)["projects"].([]any) {
					if r.(map[string]any)["name"] == "taken" {
						t.Fatal("the project stayed in the map without a directory — exactly the half we were getting away from")
					}
				}
			}
		}
	})

	t.Run("no executor — a refusal, not a half", func(t *testing.T) {
		srv.exec = nil
		code, text := profileCode(t, mux, http.MethodPost, projects,
			`{"name":"nohost","path":"`+filepath.Join(root, "nohost")+`"}`)
		if code != http.StatusServiceUnavailable {
			t.Errorf("without an executor the status is %d, expected 503: the button goes dark with a reason", code)
		}
		if !strings.Contains(text, "executor") {
			t.Errorf("the response %q does not name the reason", strings.TrimSpace(text))
		}
		for _, p := range treeOf(t, profileGet(t, mux, "/api/profiles")) {
			for _, g := range p.(map[string]any)["groups"].([]any) {
				for _, r := range g.(map[string]any)["projects"].([]any) {
					if r.(map[string]any)["name"] == "nohost" {
						t.Fatal("a project without a directory landed in the map after all")
					}
				}
			}
		}
	})
}
