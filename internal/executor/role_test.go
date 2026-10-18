package executor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
)

// sessionPlace is where the server of a live session of the fake table
// places it: the account its file lies under and the directory it names.
func sessionPlace(t *testing.T, name string) mcp.Place {
	t.Helper()
	s, err := findOneLiveSession(name)
	if err != nil {
		t.Fatal(err)
	}
	return mcp.Place{ConfigDir: s.Config, Dir: s.CWD}
}

func roleReq(target, text string) action.Request {
	r := req(action.SessionRole, target)
	r.Role = &text
	return r
}

// The role the person gives a live session lands where the panel's server
// reads it and the collector takes it to the prompt stamp from — the account,
// the repository and the name of the session, by the person — on the stream
// and in tmux alike, and nothing goes into the conversation. The answer says
// the session hears it with its next prompt. An empty role clears it.
func TestTheRoleThePersonGivesIsKeptForTheSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f := onTheStream(t, false)
	e, _ := newTest(t, "")
	ctx := context.Background()

	detail, err := e.Execute(ctx, roleReq("demo", " the lead of the db split \n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "demo") || !strings.Contains(detail, "with its next prompt") {
		t.Errorf("the report %q", detail)
	}
	got := role.Read(role.Dir(), sessionPlace(t, "demo"), "demo")
	if got == nil || got.Role != "the lead of the db split" || got.By != role.ByPerson || got.Name != "demo" {
		t.Fatalf("the role on disk is %+v", got)
	}
	if asked := f.asked(); len(asked) != 0 {
		t.Errorf("the session was asked %+v to take a role", asked)
	}

	if _, err := e.Execute(ctx, roleReq("demo", "")); err != nil {
		t.Fatal(err)
	}
	if got := role.Read(role.Dir(), sessionPlace(t, "demo"), "demo"); got != nil {
		t.Errorf("the cleared role is still on disk: %+v", got)
	}

	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	if _, err := e.Execute(ctx, roleReq("term", "watches the work queue")); err != nil {
		t.Fatalf("a session in tmux took no role: %v", err)
	}
	if got := role.Read(role.Dir(), sessionPlace(t, "term"), "term"); got == nil || got.Role != "watches the work queue" {
		t.Errorf("the role of the session in tmux is %+v", got)
	}
	if _, err := e.Execute(ctx, roleReq("gone", "x")); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("a role for no live session: %v", err)
	}
}

// A session renamed by the panel takes its role along: the server finds it
// by the new name, and the old name has none.
func TestARenamedSessionTakesItsRoleAlong(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	onTheStream(t, false)
	e, _ := newTest(t, "")
	place := sessionPlace(t, "demo")
	if _, err := role.Keep(role.Dir(), place, "demo", "the db split", role.ByPerson, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := req(action.SessionRename, "demo")
	r.Rename = "demo-pilot"
	if _, err := e.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := role.Read(role.Dir(), place, "demo-pilot"); got == nil || got.Role != "the db split" || got.By != role.ByPerson {
		t.Errorf("the renamed session has the role %+v", got)
	}
	if got := role.Read(role.Dir(), place, "demo"); got != nil {
		t.Errorf("the old name kept the role %+v", got)
	}
}

// A role session_open gives a session of a project whose agent is codex is
// refused before a thread starts: a role is kept for claude.
func TestARoleIsNotGivenToANewThreadOfCodex(t *testing.T) {
	srv, e, p := onCodexContour(t)
	p.Role = &action.OpenRole{Text: "the db split", Opener: "lead"}
	if _, _, err := open(t, e, p, `{"agent":"codex"}`); err == nil || !strings.Contains(err.Error(), "role") {
		t.Errorf("a role went with a thread of codex: %v", err)
	}
	if n := len(srv.Calls("thread/start")); n != 0 {
		t.Errorf("a refused New started %d threads", n)
	}
}

// The role a session is opened or resumed with rides to the launcher with the
// rest of the project, which keeps it under the name the session comes up
// under; a resume without one hands the launcher none, and the session keeps
// the role its name has.
func TestTheRoleRidesToTheLauncher(t *testing.T) {
	const conversation = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"
	_, dir := launchDir(t, "aacpanel")
	for _, c := range []struct {
		kind   action.Kind
		resume string
		role   *action.OpenRole
		want   string
	}{
		{action.SessionOpen, "", &action.OpenRole{Text: "the db split", Opener: "lead"},
			`"role":{"text":"the db split","opener":"lead"}`},
		{action.SessionResume, conversation, &action.OpenRole{}, `"role":{"text":""}`},
		{action.SessionResume, conversation, nil, ""},
	} {
		trustFile(t, map[string]bool{dir: true})
		log := fakeLauncher(t, launcher.Report{Session: "aacpanel-2", Agent: 1235})
		e, _ := newTest(t, "")
		if _, err := e.Execute(context.Background(), openWith(c.kind,
			&action.Project{Path: dir, Session: "aacpanel", Role: c.role}, c.resume)); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		task := string(raw)
		if c.want != "" && !strings.Contains(task, c.want) || c.want == "" && strings.Contains(task, `"role"`) {
			t.Errorf("%s with %+v handed the launcher %s", c.kind, c.role, task)
		}
	}
}
