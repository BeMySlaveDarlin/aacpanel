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
	"aacpanel/internal/testdb"
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
// lead in the project's own directory, kid in a worktree of it, running
// conversation kidConversation, grandkid opened by kid, seller in the other
// project, stray in a directory no project holds, and run, a claude a program
// of lead started inside its work.
func standServer(t *testing.T) (*Server, *fakeExec, string) {
	t.Helper()
	srv, root := hostServer(t, `{"at":1}`)
	fillMap(t, srv, root)
	repo, worktree := filepath.Join(root, "aacpanel"), filepath.Join(root, "aacpanel-fix")
	shop := otherProject(t, srv, root, "shop")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[`+
		`{"session":"lead","cwd":"`+repo+`","transport":"stream"},`+
		`{"session":"kid","cwd":"`+worktree+`","sessionId":"`+kidConversation+`"},`+
		`{"session":"grandkid","cwd":"`+repo+`","parent":{"session":"kid"}},`+
		`{"session":"seller","cwd":"`+shop+`"},`+
		`{"session":"stray","cwd":"`+filepath.Join(root, "stray")+`"},`+
		`{"session":"run","cwd":"`+repo+`","outside":true,"parent":{"session":"lead"}}],`+
		`"projects":{"at":1,"dirs":[{"path":"`+worktree+`","kind":"project","worktreeOf":"`+repo+`"}]}}`))
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session kid stands under lead now"})
	srv.exec, srv.auth = client, &auth.Service{}
	return srv, fake, root
}

const kidConversation = "33333333-3333-4333-8333-333333333333"

// archive puts a conversation into the archive of the panel's machine: it ran
// in cwd under name an hour ago.
func archive(t *testing.T, srv *Server, name, conversation, cwd string) {
	t.Helper()
	hostID, err := srv.db.HostID(t.Context(), srv.hostName)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	testdb.PartitionsBack(t, t.Context(), pool, "sessions_1m", 3*time.Hour)
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO sessions_1m (bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples, session_id, cwd)
		VALUES ($1, $2, $3, 1000, 10, 20, 5, 6, $4, $5) ON CONFLICT DO NOTHING`,
		time.Now().UTC().Add(-time.Hour).Truncate(time.Minute), hostID, name, conversation, cwd); err != nil {
		t.Fatal(err)
	}
}

// A live session is stood under another of its project — its own directory,
// one inside it, a worktree of it — and the executor hears which, with the
// journal.
func TestALiveSessionIsStoodUnderAnotherOfItsProjectPG(t *testing.T) {
	srv, fake, _ := standServer(t)
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
	srv, fake, _ := standServer(t)
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

// A live session the panel stands under another is taken from under it: the
// executor hears the session alone, and the journal names the session it
// stood under, which the snapshot knows now and not after.
func TestALiveSessionIsTakenFromUnderItsParentPG(t *testing.T) {
	srv, fake, _ := standServer(t)
	w := post(t, srv, `{"kind":"session.detach","target":"grandkid","params":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var got action.Request
	select {
	case got = <-fake.got:
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if got.Kind != action.SessionDetach || got.Target != "grandkid" || got.Parent != "" {
		t.Errorf("the executor was asked %s %q with parent %q, meant session.detach of grandkid", got.Kind, got.Target,
			got.Parent)
	}
	list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
	if err != nil || len(list) == 0 {
		t.Fatalf("the action did not reach the journal: %v", err)
	}
	if list[0].Kind != string(action.SessionDetach) || list[0].Target != "grandkid" || list[0].Params["from"] != "kid" {
		t.Errorf("the journal holds %s %q with %v", list[0].Kind, list[0].Target, list[0].Params)
	}
}

// Only a session the panel stands under another is taken from under it, and
// the panel says why not before the executor is asked: a session standing on
// its own already, one that does not live, and a run inside the work of a
// session, which stands under that session for as long as it runs.
func TestOnlyASessionUnderAnotherIsTakenFromUnderItPG(t *testing.T) {
	srv, fake, _ := standServer(t)
	for name, c := range map[string]struct{ target, says string }{
		"a session on its own":   {"lead", "stands under no session already"},
		"a session that is not":  {"gone", `no live session "gone"`},
		"a run inside a session": {"run", "outside the panel"},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(t, srv, `{"kind":"session.detach","target":"`+c.target+`","params":{}}`)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.says) {
				t.Errorf("status %d, body %q, meant 400 saying %q", w.Code, w.Body.String(), c.says)
			}
		})
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused detach went to the executor: %+v", got)
	default:
	}
}

// A conversation a session resumes through session_open stands under that
// session by the rule of an open: while the two are of one project — the
// conversation ran in the project's directory, and the caller lives in a
// worktree of it — and on its own otherwise. It goes on where it ran, whatever
// directory of its project the session names, and under the name the session
// gives or else the one it ran under. The Resume of the screens names no
// parent and no name, and comes back under the name it ran under as well, so
// it finds the role that name has. A role the session gives goes along as
// set by the session that asked, whichever project that one is of.
func TestAResumeASessionAsksForStandsUnderItPG(t *testing.T) {
	srv, fake, root := standServer(t)
	repo, shop := filepath.Join(root, "aacpanel"), filepath.Join(root, "shop")
	const task, sold = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	archive(t, srv, "task-7", task, repo)
	archive(t, srv, "shop-fix", sold, shop)
	const opener = "9e3f0a4b-5c6d-4e7f-8a9b-0c1d2e3f4a5b"

	asked := func(conversation, params string) string {
		return `{"kind":"session.resume","params":{"session":"` + conversation + `","openedBy":"` + opener + `"` +
			params + `}}`
	}
	for _, c := range []struct {
		name, body, session, parent, path, by, logged string
		role                                          *action.OpenRole
	}{
		{"by a session of its project", asked(task, `,"name":"","parent":"kid"`), "task-7", "kid", repo, opener,
			"task-7", nil},
		{"under a name of the session's", asked(task, `,"name":"fix-2","parent":"kid"`), "fix-2", "kid", repo, opener,
			"fix-2", nil},
		{"naming a directory of its project", asked(task, `,"name":"","parent":"kid","path":"`+repo+`/web"`),
			"task-7", "kid", repo, opener, "task-7", nil},
		{"by a session of another project", asked(task, `,"name":"","parent":"seller"`), "task-7", "", repo, opener,
			"task-7", nil},
		{"of another project", asked(sold, `,"name":"","parent":"kid"`), "shop-fix", "", shop, opener, "shop-fix", nil},
		{"with a role", asked(task, `,"name":"","parent":"kid","role":"the db split"`), "task-7", "kid", repo, opener,
			"task-7", &action.OpenRole{Text: "the db split", Opener: "kid"}},
		{"with a role by a session of another project", asked(task, `,"name":"","parent":"seller","role":""`),
			"task-7", "", repo, opener, "task-7", &action.OpenRole{Opener: "seller"}},
		// The journal keeps the row the screen pressed, as it always has.
		{"from the screens", `{"kind":"session.resume","target":"task-7","params":{"session":"` + task + `"}}`,
			"task-7", "", repo, "", "task-7", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := post(t, srv, c.body)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
			var got action.Request
			select {
			case got = <-fake.got:
			case <-time.After(3 * time.Second):
				t.Fatal("the executor did not get the request")
			}
			if got.Kind != action.SessionResume || got.Project == nil || got.Project.Session != c.session ||
				got.Target != c.session || got.Project.Parent != c.parent || got.Project.Path != c.path ||
				got.Project.OpenedBy != c.by {
				t.Fatalf("the resume reached the executor as %q with %+v, meant %s in %s under %q opened by %q",
					got.Target, got.Project, c.session, c.path, c.parent, c.by)
			}
			if (got.Project.Role == nil) != (c.role == nil) || c.role != nil && *got.Project.Role != *c.role {
				t.Errorf("the resume carried the role %+v, meant %+v", got.Project.Role, c.role)
			}
			list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
			if err != nil || len(list) == 0 {
				t.Fatalf("the action did not reach the journal: %v", err)
			}
			if parent, named := list[0].Params["parent"]; list[0].Target != c.logged ||
				named != (c.parent != "") || named && parent != c.parent {
				t.Errorf("the journal holds %q with %v, meant %s under %q", list[0].Target, list[0].Params, c.logged,
					c.parent)
			}
			if role, given := list[0].Params["role"]; given != (c.role != nil) || given && role != c.role.Text {
				t.Errorf("the journal holds the role %v, meant %+v", list[0].Params, c.role)
			}
		})
	}
}

// A resume a session asks for is refused before the executor is asked when
// the conversation lives — two processes would write one transcript — and
// when the directory it names is of another project than the one the
// conversation ran in, or of none.
func TestAResumeASessionAsksForIsRefusedPG(t *testing.T) {
	srv, fake, root := standServer(t)
	repo, worktree := filepath.Join(root, "aacpanel"), filepath.Join(root, "aacpanel-fix")
	const task = "11111111-1111-4111-8111-111111111111"
	archive(t, srv, "task-7", task, repo)
	archive(t, srv, "kid", kidConversation, worktree)

	for name, c := range map[string]struct {
		body   string
		status int
		says   string
	}{
		"a live conversation": {`{"kind":"session.resume","params":{"session":"` + kidConversation +
			`","name":"","parent":"lead"}}`, http.StatusConflict, "live session kid"},
		"a directory of another project": {`{"kind":"session.resume","params":{"session":"` + task +
			`","name":"","parent":"lead","path":"` + filepath.Join(root, "shop") + `"}}`, http.StatusBadRequest,
			"only in the project it ran in"},
		"a directory no project holds": {`{"kind":"session.resume","params":{"session":"` + task +
			`","name":"","path":"` + filepath.Join(root, "stray") + `"}}`, http.StatusBadRequest,
			"no project of the map holds"},
		"a name that is no name": {`{"kind":"session.resume","params":{"session":"` + task + `","name":7}}`,
			http.StatusBadRequest, "did not arrive as a name"},
		"a name of an open": {`{"kind":"session.open","params":{"path":"` + repo + `","name":"lab"}}`,
			http.StatusBadRequest, "did not arrive as a name of a session.resume"},
		"a role longer than a role": {`{"kind":"session.resume","params":{"session":"` + task + `","name":"","role":"` +
			strings.Repeat("é", 501) + `"}}`, http.StatusBadRequest, "at most 500"},
		"a role that is no text": {`{"kind":"session.open","params":{"path":"` + repo + `","role":7}}`,
			http.StatusBadRequest, "did not arrive as the text"},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(t, srv, c.body)
			if w.Code != c.status || !strings.Contains(w.Body.String(), c.says) {
				t.Errorf("status %d, body %q, meant %d saying %q", w.Code, w.Body.String(), c.status, c.says)
			}
		})
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused resume went to the executor: %+v", got)
	default:
	}
}
