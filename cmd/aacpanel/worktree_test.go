package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
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
