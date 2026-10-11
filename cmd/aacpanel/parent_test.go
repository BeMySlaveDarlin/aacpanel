package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

// otherProject adds a project of the given name beside the one fillMap lays
// out, in its group and with the same launch parameters, and returns its
// directory.
func otherProject(t *testing.T, srv *Server, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := srv.db.Profiles(t.Context())
	if err != nil || len(list) == 0 || len(list[0].Groups) == 0 {
		t.Fatalf("the map has no group to add a project to: %v", err)
	}
	if _, err := srv.db.CreateProject(t.Context(), list[0].Groups[0].ID, store.ProjectEdit{
		Name: &name, Path: &dir, Launch: json.RawMessage(`{"effort":"high"}`),
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// standServer is a panel with a map of two projects and these live sessions:
// lead in the project's own directory, kid in a worktree of it, grandkid
// opened by kid, seller in the other project, stray in a directory no project
// holds, and run, a claude a program of lead started inside its work.
func standServer(t *testing.T) (*Server, *fakeExec) {
	t.Helper()
	srv, root := hostServer(t, `{"at":1}`)
	fillMap(t, srv, root)
	repo, worktree := filepath.Join(root, "aacpanel"), filepath.Join(root, "aacpanel-fix")
	shop := otherProject(t, srv, root, "shop")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[`+
		`{"session":"lead","cwd":"`+repo+`","transport":"stream"},`+
		`{"session":"kid","cwd":"`+worktree+`"},`+
		`{"session":"grandkid","cwd":"`+repo+`","parent":{"session":"kid"}},`+
		`{"session":"seller","cwd":"`+shop+`"},`+
		`{"session":"stray","cwd":"`+filepath.Join(root, "stray")+`"},`+
		`{"session":"run","cwd":"`+repo+`","outside":true,"parent":{"session":"lead"}}],`+
		`"projects":{"at":1,"dirs":[{"path":"`+worktree+`","kind":"project","worktreeOf":"`+repo+`"}]}}`))
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session kid stands under lead now"})
	srv.exec, srv.auth = client, &auth.Service{}
	return srv, fake
}

// A live session is stood under another of its project — its own directory,
// one inside it, a worktree of it — and the executor hears which, with the
// journal.
func TestALiveSessionIsStoodUnderAnotherOfItsProjectPG(t *testing.T) {
	srv, fake := standServer(t)
	for _, c := range []struct{ target, parent string }{{"kid", "lead"}, {"lead", "kid"}} {
		w := post(t, srv, `{"kind":"session.parent","target":"`+c.target+`","params":{"parent":"`+c.parent+`"}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("%s under %s: status %d, body %s", c.target, c.parent, w.Code, w.Body.String())
		}
		var got action.Request
		select {
		case got = <-fake.got:
		case <-time.After(3 * time.Second):
			t.Fatal("the executor did not get the request")
		}
		if got.Kind != action.SessionParent || got.Target != c.target || got.Parent != c.parent {
			t.Errorf("the executor was asked %s %q under %q, meant %s under %s", got.Kind, got.Target, got.Parent,
				c.target, c.parent)
		}
		list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
		if err != nil || len(list) == 0 {
			t.Fatalf("the action did not reach the journal: %v", err)
		}
		if list[0].Kind != string(action.SessionParent) || list[0].Target != c.target ||
			list[0].Params["parent"] != c.parent {
			t.Errorf("the journal holds %s %q with %v", list[0].Kind, list[0].Target, list[0].Params)
		}
	}
}

// A session is stood under another only when both live in one project of
// the map, and the panel says why not before the executor is asked. A run
// inside the work of a session stands under that session and is stood under
// nothing else, and two sessions do not stand under each other.
func TestASessionIsStoodOnlyUnderALiveSessionOfItsProjectPG(t *testing.T) {
	srv, fake := standServer(t)
	for name, c := range map[string]struct{ target, params, says string }{
		"a parent of another project": {"kid", `{"parent":"seller"}`, "only within one project"},
		"a child of another project":  {"seller", `{"parent":"lead"}`, "only within one project"},
		"a child no project holds":    {"stray", `{"parent":"lead"}`, "no project of the map holds"},
		"a parent no project holds":   {"kid", `{"parent":"stray"}`, "only within one project"},
		"a parent that does not live": {"kid", `{"parent":"gone"}`, `no live session "gone"`},
		"a child that does not live":  {"gone", `{"parent":"lead"}`, `no live session "gone"`},
		"a run inside a session":      {"run", `{"parent":"kid"}`, "outside the panel"},
		"under its own child":         {"kid", `{"parent":"grandkid"}`, "under each other"},
		"under itself":                {"kid", `{"parent":"kid"}`, "under itself"},
		"no parent":                   {"kid", `{}`, "did not arrive as a name"},
		"a parent that is no name":    {"kid", `{"parent":7}`, "did not arrive as a name"},
		"a parent with a slash":       {"kid", `{"parent":"a/b"}`, "contains /"},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(t, srv, `{"kind":"session.parent","target":"`+c.target+`","params":`+c.params+`}`)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.says) {
				t.Errorf("status %d, body %q, meant 400 saying %q", w.Code, w.Body.String(), c.says)
			}
		})
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused stand went to the executor: %+v", got)
	default:
	}
}
