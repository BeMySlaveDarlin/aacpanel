package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

// A conversation run in a git worktree kept beside its repository is resumed
// in that worktree — its transcript is kept by that directory — with the
// launch parameters of the repository's project. The map knows only the
// repository; the agent says which repository the worktree belongs to.
func TestResumeFromAWorktreeCarriesItsProjectPG(t *testing.T) {
	srv, root := profilesServer(t)
	repo := filepath.Join(root, "Acme", "acme")
	worktree := filepath.Join(root, "Acme", "acme-fix")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"projects":{"at":1,"roots":["`+root+`"],"depth":4,"dirs":[`+
		`{"path":"`+repo+`","kind":"project","git":true},`+
		`{"path":"`+worktree+`","kind":"project","git":true,"worktreeOf":"`+repo+`"}]}}`))

	name, group, project := "Acme", "Work", "acme"
	profile, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{
		Name: &name, ConfigDir: &root, Launch: json.RawMessage(`{"model":"opus"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := srv.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &group})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{
		Name: &project, Path: &repo, Launch: json.RawMessage(`{"effort":"high"}`),
	}); err != nil {
		t.Fatal(err)
	}

	const conversation = "99999999-9999-4999-8999-999999999999"
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
		VALUES ($1, $2, 'acme', 1000, 10, 20, 5, 6, $3, $4) ON CONFLICT DO NOTHING`,
		time.Now().UTC().Add(-time.Hour).Truncate(time.Minute), hostID, conversation, worktree); err != nil {
		t.Fatal(err)
	}
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "resumed"})
	srv.exec, srv.auth = client, &auth.Service{}

	w := post(t, srv, `{"kind":"session.resume","target":"acme","params":{"session":"`+conversation+`"}}`)
	if w.Code != 200 {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Resume != conversation {
		t.Errorf("conversation %q reached the executor", got.Resume)
	}
	if got.Project == nil {
		t.Fatal("the resume went without a project: the executor knows no project by the name of a worktree")
	}
	if got.Project.Path != worktree {
		t.Errorf("the conversation would be resumed in %s, while its transcript is kept by %s", got.Project.Path, worktree)
	}
	var launch map[string]any
	if err := json.Unmarshal(got.Project.Launch, &launch); err != nil || launch["model"] != "opus" || launch["effort"] != "high" {
		t.Errorf("the launch parameters of the repository's project did not come along: %s", got.Project.Launch)
	}
}

// A conversation run in a directory named in Cyrillic, with a space, is
// resumed from its archive card: the map holds the directory as a project with
// no session name of its own, so the session answers to the name of the
// directory, and the resume reaches the executor under that name.
func TestResumeFromADirectoryNamedInAnotherScriptPG(t *testing.T) {
	srv, root := profilesServer(t)
	// "my project" in Russian.
	const named = "\u043c\u043e\u0439 \u043f\u0440\u043e\u0435\u043a\u0442"
	dir := filepath.Join(root, named)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	contour, group := "Acme", "Work"
	profile, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{Name: &contour, ConfigDir: &root})
	if err != nil {
		t.Fatal(err)
	}
	g, err := srv.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &group})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{Name: &group, Path: &dir}); err != nil {
		t.Fatal(err)
	}

	const conversation = "99999999-9999-4999-8999-999999999998"
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
		time.Now().UTC().Add(-time.Hour).Truncate(time.Minute), hostID, named, conversation, dir); err != nil {
		t.Fatal(err)
	}
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "resumed"})
	srv.exec, srv.auth = client, &auth.Service{}

	body, err := json.Marshal(map[string]any{"kind": "session.resume", "target": named,
		"params": map[string]any{"session": conversation}})
	if err != nil {
		t.Fatal(err)
	}
	w := post(t, srv, string(body))
	if w.Code != 200 {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Resume != conversation || got.Target != named {
		t.Errorf("the executor got conversation %q of %q", got.Resume, got.Target)
	}
	if got.Project == nil || got.Project.Path != dir || got.Project.Session != named {
		t.Errorf("the resume went with project %+v, not the directory %s under its name", got.Project, dir)
	}
}

// A worktree the snapshot does not know — made a moment ago, or kept in a
// folder of worktrees the walk of the roots never enters — is still a worktree
// of its repository: the panel asks the collector, which names the main
// checkout first among the worktrees git lists. A session opens a child there
// at once, without working in it first, and a conversation that ran there is
// resumed as the repository's project. A directory the collector names as a
// repository of its own, or a collector that does not answer, leaves the
// directory outside the map, as before.
func TestAWorktreeTheSnapshotDoesNotKnowIsOfItsRepositoryPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	repo := filepath.Join(root, "aacpanel")
	folder := filepath.Join(root, "Labs")
	worktree := filepath.Join(folder, "worktrees", "aacpanel-fix")
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[{"session":"lead","cwd":"`+repo+`"}],`+
		`"projects":{"at":1,"dirs":[{"path":"`+folder+`","kind":"project"}]}}`))
	refs := func(top string, trees ...string) map[string]any {
		list := []map[string]any{}
		for _, p := range trees {
			list = append(list, map[string]any{"path": p})
		}
		return map[string]any{"ok": true, "op": "refs", "repo": map[string]any{"root": top, "branches": []any{},
			"worktrees": list}}
	}
	collector := startAgent(t, refs(worktree, repo, worktree))
	srv.chat = chat.New(collector.path)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "started"})
	srv.exec, srv.auth = client, &auth.Service{}

	const conversation = "99999999-9999-4999-8999-999999999997"
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
		VALUES ($1, $2, 'aacpanel-fix', 1000, 10, 20, 5, 6, $3, $4) ON CONFLICT DO NOTHING`,
		time.Now().UTC().Add(-time.Hour).Truncate(time.Minute), hostID, conversation, worktree); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, body, target, path, parent string }{
		{"a child opened in it", `{"kind":"session.open","params":{"path":"` + worktree + `","parent":"lead"}}`,
			"aacpanel", worktree, "lead"},
		{"a child opened deep inside it", `{"kind":"session.open","params":{"path":"` + worktree + `/web"}}`,
			"aacpanel", worktree + "/web", ""},
		{"a conversation that ran in it", `{"kind":"session.resume","target":"aacpanel-fix","params":{"session":"` +
			conversation + `"}}`, "aacpanel-fix", worktree, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := post(t, srv, c.body)
			if w.Code != 200 {
				t.Fatalf("status %d, body %s", w.Code, w.Body.String())
			}
			select {
			case asked := <-collector.got:
				if asked.Repo == nil || asked.Repo.Op != "refs" || !strings.HasPrefix(asked.Repo.Cwd, worktree) {
					t.Errorf("the collector was asked %+v, not for the worktrees of %s", asked.Repo, worktree)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the collector was not asked whose worktree the directory is")
			}
			var got action.Request
			select {
			case got = <-fake.got:
			case <-time.After(3 * time.Second):
				t.Fatal("the executor did not get the request")
			}
			if got.Project == nil || got.Project.Path != c.path || got.Project.Session != c.target ||
				got.Project.ConfigDir != root || got.Project.Parent != c.parent {
				t.Fatalf("the request reached the executor with %+v, meant %q in %s of the contour in %s under %q",
					got.Project, c.target, c.path, root, c.parent)
			}
			list, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
			if err != nil || len(list) == 0 || list[0].Params["project"] != float64(id) {
				t.Errorf("the journal holds %+v, meant project %d: %v", list, id, err)
			}
		})
	}

	for name, answer := range map[string]*fakeAgent{
		"a repository of its own": startAgent(t, refs(worktree, worktree)),
		"no answer":               {path: socketPath(t, "gone.sock")},
	} {
		t.Run("refused on "+name, func(t *testing.T) {
			srv.chat = chat.New(answer.path)
			w := post(t, srv, `{"kind":"session.open","params":{"path":"`+worktree+`"}}`)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "no project of the map holds "+worktree) {
				t.Errorf("status %d, body %q, meant a refusal for a directory outside the map", w.Code, w.Body.String())
			}
		})
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused open went to the executor: %+v", got)
	default:
	}
}
