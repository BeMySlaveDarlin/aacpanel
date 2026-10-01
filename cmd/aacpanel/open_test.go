package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

// A session opening another names a directory: the panel finds the project of
// the map it belongs to — the project's own, one inside it, a worktree of it —
// and opens the session there with the launch parameters of the map, under the
// name the session gave or else the project's session name. The answer names
// the contour, so the session can tell its person which account the new one
// spends; a project opened by its id, as the screens open one, names it too.
func TestASessionOpensInADirectoryAsItsProjectPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	repo, worktree := filepath.Join(root, "aacpanel"), filepath.Join(root, "aacpanel-fix")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"projects":{"at":1,"dirs":[`+
		`{"path":"`+worktree+`","kind":"project","git":true,"worktreeOf":"`+repo+`"}]}}`))
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session aacpanel started"})
	srv.exec, srv.auth = client, &auth.Service{}

	for _, c := range []struct {
		name, body, target, path string
	}{
		{"the project's own directory", `{"kind":"session.open","params":{"path":"` + repo + `"}}`, "aacpanel", repo},
		{"a directory inside it, under a name of the session's",
			`{"kind":"session.open","target":"lab","params":{"path":"` + repo + `/web"}}`, "lab", repo + "/web"},
		{"a worktree beside it", `{"kind":"session.open","params":{"path":"` + worktree + `"}}`, "aacpanel", worktree},
		{"the project's id, as the screens send it",
			`{"kind":"session.open","target":"aacpanel","params":{"project":` + strconv.Itoa(id) + `}}`, "aacpanel", repo},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := post(t, srv, c.body)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
			var out struct {
				Detail  string `json:"detail"`
				Contour string `json:"contour"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Contour != "personal" ||
				out.Detail != "session aacpanel started" {
				t.Errorf("the panel answered %s: the session would not know the account it opened in", w.Body.String())
			}

			var got action.Request
			select {
			case got = <-fake.got:
			case <-time.After(3 * time.Second):
				t.Fatal("the executor did not get the request")
			}
			if got.Kind != action.SessionOpen || got.Target != c.target || got.Project == nil ||
				got.Project.Path != c.path || got.Project.Session != c.target || got.Project.ConfigDir != root {
				t.Fatalf("the open reached the executor as %q with %+v, meant %q in %s of the contour in %s",
					got.Target, got.Project, c.target, c.path, root)
			}
			var launch map[string]any
			if err := json.Unmarshal(got.Project.Launch, &launch); err != nil || launch["model"] != "opus" ||
				launch["effort"] != "high" {
				t.Errorf("the launch parameters of the map did not come along: %s", got.Project.Launch)
			}

			list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
			if err != nil || len(list) == 0 {
				t.Fatalf("the action did not reach the journal: %v", err)
			}
			if list[0].Target != c.target || list[0].Params["path"] != c.path || list[0].Params["project"] != float64(id) {
				t.Errorf("the journal holds %q with %v, meant %q with project %d in %s",
					list[0].Target, list[0].Params, c.target, id, c.path)
			}
		})
	}
}

// A directory no project of the map holds is not opened, not even under the
// name of a project: the panel would know neither the account to open it in
// nor its launch parameters. A directory that is no absolute path within the
// roots, or one named beside a project's id, is refused before the executor is
// asked as well.
func TestASessionOpensOnlyInADirectoryOfTheMapPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	repo, stray := filepath.Join(root, "aacpanel"), filepath.Join(root, "stray")
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session aacpanel started"})
	srv.exec, srv.auth = client, &auth.Service{}

	for name, c := range map[string]struct{ body, says string }{
		"a directory no project holds": {`{"kind":"session.open","params":{"path":"` + stray + `"}}`,
			"no project of the map holds " + stray},
		"a directory no project holds, under a project's name": {
			`{"kind":"session.open","target":"aacpanel","params":{"path":"` + stray + `"}}`,
			"no project of the map holds " + stray},
		"a relative directory": {`{"kind":"session.open","target":"aacpanel","params":{"path":"aacpanel"}}`,
			"not absolute"},
		"a directory outside the roots": {`{"kind":"session.open","params":{"path":"/etc"}}`,
			"outside the allowed roots"},
		"a directory that is no string": {`{"kind":"session.open","target":"aacpanel","params":{"path":7}}`,
			"did not arrive as a path"},
		"a directory beside an id": {`{"kind":"session.open","params":{"path":"` + repo + `","project":` +
			strconv.Itoa(id) + `}}`, "not by both"},
		"a name with a slash": {`{"kind":"session.open","target":"lab/fix","params":{"path":"` + repo + `"}}`,
			"contains /"},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(t, srv, c.body)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.says) {
				t.Errorf("status %d, body %q, meant 400 saying %q", w.Code, w.Body.String(), c.says)
			}
		})
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused open went to the executor: %+v", got)
	default:
	}
}

// Without the map there is no project to open a directory as, and the open is
// refused rather than handed to the executor by a name it would guess with.
func TestASessionOpensInADirectoryOnlyWithTheMap(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session lab started"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.open","target":"lab","params":{"path":"/srv/proj/lab"}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "database with the map is not configured") {
		t.Errorf("status %d, body %q", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		t.Fatalf("an open with no map went to the executor: %+v", got)
	default:
	}
}
