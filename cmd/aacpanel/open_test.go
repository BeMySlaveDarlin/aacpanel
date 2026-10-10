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

// New starts the agent picked on its sheet, for this once: the launch the
// executor is handed names the picked agent and keeps the rest of the map's
// choices, and the journal says which agent was asked for. Without a pick the
// project's own agent starts, as the map says.
func TestNewStartsTheAgentPickedOnItsSheetPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "started"})
	srv.exec, srv.auth = client, &auth.Service{}

	open := func(t *testing.T, params string) (map[string]any, map[string]any) {
		t.Helper()
		w := post(t, srv, `{"kind":"session.open","target":"aacpanel","params":{"project":`+strconv.Itoa(id)+params+`}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var got action.Request
		select {
		case got = <-fake.got:
		case <-time.After(3 * time.Second):
			t.Fatal("the executor did not get the request")
		}
		if got.Project == nil {
			t.Fatal("New reached the executor without its project")
		}
		var launch map[string]any
		if err := json.Unmarshal(got.Project.Launch, &launch); err != nil {
			t.Fatalf("the launch was not parsed: %v", err)
		}
		list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
		if err != nil || len(list) == 0 {
			t.Fatalf("the action did not reach the journal: %v", err)
		}
		return launch, list[0].Params
	}

	t.Run("codex over a claude project", func(t *testing.T) {
		launch, journal := open(t, `,"agent":"codex"`)
		if launch["agent"] != "codex" || launch["model"] != "opus" || launch["effort"] != "high" {
			t.Errorf("the executor was handed %v: codex with the rest of the map's launch was meant", launch)
		}
		if journal["agent"] != "codex" {
			t.Errorf("the journal holds %v: it does not say the press started codex", journal)
		}
	})

	if _, err := srv.db.UpdateProject(t.Context(), id, store.ProjectEdit{LaunchSet: map[string]any{
		"agent": "codex", "codexModel": "gpt-5.5",
	}}); err != nil {
		t.Fatal(err)
	}

	t.Run("claude over a codex project", func(t *testing.T) {
		launch, journal := open(t, `,"agent":"claude"`)
		if launch["agent"] != "claude" || launch["model"] != "opus" || launch["codexModel"] != "gpt-5.5" {
			t.Errorf("the executor was handed %v: claude with the rest of the map's launch was meant", launch)
		}
		if journal["agent"] != "claude" {
			t.Errorf("the journal holds %v: it does not say the press started claude", journal)
		}
	})

	t.Run("no pick starts the project's own", func(t *testing.T) {
		launch, journal := open(t, ``)
		if launch["agent"] != "codex" {
			t.Errorf("the executor was handed %v: the project's codex was meant", launch)
		}
		if _, ok := journal["agent"]; ok {
			t.Errorf("the journal holds %v: no agent was asked for", journal)
		}
	})
}

// An agent New cannot start is refused before the executor is asked: a word
// that is no agent, codex for a session the map does not hold — its daemon is
// the one of a project's contour — and any agent but codex named for a resume,
// which goes on a conversation of claude's otherwise. Claude for a session off
// the map goes on, as a New without a pick does.
func TestNewRefusesAnAgentItCannotStart(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session lab started"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	for name, c := range map[string]struct{ body, says string }{
		"a word that is no agent": {`{"kind":"session.open","target":"lab","params":{"agent":"gemini"}}`,
			"did not arrive as claude or codex"},
		"an agent that is no word": {`{"kind":"session.open","target":"lab","params":{"agent":7}}`,
			"did not arrive as claude or codex"},
		"codex off the map": {`{"kind":"session.open","target":"lab","params":{"agent":"codex"}}`,
			"codex starts only in a project of the map"},
		"a resume with an agent": {`{"kind":"session.resume","target":"lab","params":{"agent":"claude"}}`,
			"no other agent"},
		"a resume of codex without its contour": {`{"kind":"session.resume","target":"lab","params":{"agent":"codex",` +
			`"session":"019a2000-0000-7000-8000-00000000a1b2"}}`, "names the contour whose daemon keeps it"},
		"a resume of codex without its thread": {`{"kind":"session.resume","target":"lab","params":{"agent":"codex",` +
			`"contour":"acme"}}`, "without a conversation id"},
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

	w := post(t, srv, `{"kind":"session.open","target":"lab","params":{"agent":"claude"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("claude off the map: status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Target != "lab" || got.Project != nil {
			t.Errorf("claude off the map reached the executor as %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claude off the map did not reach the executor")
	}
}

// A resume of codex goes to the executor as the thread and the contour of its
// home, with no project and no archive of claude asked: the daemon of the home
// knows where the thread ran. The answer names the session it brought up and
// the contour it spends.
func TestAResumeOfCodexGoesToTheDaemonOfItsContour(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session codex-0000a1b2 resumed",
		Session: "codex-0000a1b2"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.resume","target":"shop","params":{"agent":"codex","contour":"acme",`+
		`"session":"019a2000-0000-7000-8000-00000000a1b2"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionResume || got.Agent != action.ResumeCodex || got.Contour != "acme" ||
			got.Resume != "019a2000-0000-7000-8000-00000000a1b2" || got.Project != nil {
			t.Errorf("the resume reached the executor as %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["session"] != "codex-0000a1b2" || out["contour"] != "acme" {
		t.Errorf("the answer is %v: it does not name the session and the contour", out)
	}
}

// The archive card of a codex thread is named after the directory the thread
// ran in, and a directory named in Cyrillic, with a space, is resumed too.
func TestAResumeOfCodexInADirectoryNamedInAnotherScript(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Session: "codex-0000a1b2"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	// "my project" in Russian.
	body, err := json.Marshal(map[string]any{"kind": "session.resume",
		"target": "\u043c\u043e\u0439 \u043f\u0440\u043e\u0435\u043a\u0442",
		"params": map[string]any{"agent": "codex", "contour": "acme", "session": "019a2000-0000-7000-8000-00000000a1b2"}})
	if err != nil {
		t.Fatal(err)
	}
	w := post(t, srv, string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Agent != action.ResumeCodex || got.Resume != "019a2000-0000-7000-8000-00000000a1b2" {
			t.Errorf("the resume reached the executor as %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
}
