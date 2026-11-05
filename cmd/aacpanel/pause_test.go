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
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

// pauseServer is a service over a map of two projects in one group, and a
// snapshot with a session of the first project, a session of its worktree that
// keeps a pause alarm, a codex thread of the second project, and two sessions
// the map does not hold.
func pauseServer(t *testing.T) (*Server, *fakeExec, store.ProfileProject, time.Time) {
	t.Helper()
	srv, fake, dir := switchServer(t, "stream", "stream")
	profiles, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	group := profiles[0].Groups[0]
	anchor := group.Projects[0]
	name, path := "other", filepath.Join(filepath.Dir(dir), "other")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.CreateProject(t.Context(), group.ID, store.ProjectEdit{Name: &name, Path: &path}); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour).In(time.FixedZone("local", 7*3600)).Truncate(time.Second)
	rows := []map[string]any{
		{"session": "lead", "sessionId": switchSID, "cwd": dir},
		{"session": "child", "sessionId": "77777777-7777-4777-8777-777777777777", "cwd": dir + "/.claude/worktrees/check", "pauseAlarm": pauseAlarm{ID: "0123456789abcdef", Name: "pause", Next: until.Format(time.RFC3339)}},
		{"session": "thread", "sessionId": "66666666-6666-4666-8666-666666666666", "cwd": path, "agent": "codex"},
		{"session": "outside", "sessionId": "55555555-5555-4555-8555-555555555555", "cwd": dir, "outside": true},
		{"session": "unmapped", "sessionId": "44444444-4444-4444-8444-444444444444", "cwd": "/srv/unmapped"},
	}
	raw, err := json.Marshal(map[string]any{"sessions": rows})
	if err != nil {
		t.Fatal(err)
	}
	srv.host = host.NewReader(snapshotWith(t, string(raw)))
	return srv, fake, anchor, until
}

func postPause(t *testing.T, srv *Server, p action.Pause) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": action.ProjectPause, "params": p})
	w := post(t, srv, string(raw))
	return w.Code, w.Body.String()
}

func sendPause(t *testing.T, srv *Server, p action.Pause, want int) {
	t.Helper()
	code, body := postPause(t, srv, p)
	if code != http.StatusOK {
		t.Fatalf("pause: %d %s", code, body)
	}
	var reply struct {
		Delivered int
		Failed    []map[string]string
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Delivered != want || len(reply.Failed) != 0 {
		t.Fatalf("pause response: %s", body)
	}
}

func pauseInstruction(t *testing.T, req action.Request, until time.Time) {
	t.Helper()
	if req.Kind != action.SessionSend || !strings.Contains(req.Text, `from="pause" until="`+until.Format(time.RFC3339)+`"`) ||
		!strings.Contains(req.Text, until.Format("15:04, 2 Jan 2006")) || !strings.Contains(req.Text, `name: "pause"`) || !strings.Contains(req.Text, "alarm_set") {
		t.Fatalf("pause instruction: %+v", req)
	}
}

func resumeOf(t *testing.T, fake *fakeExec, target string) {
	t.Helper()
	cancel, message := <-fake.got, <-fake.got
	if cancel.Kind != action.AlarmCancel || cancel.Target != target || cancel.Work == nil || cancel.Work.ID != "0123456789abcdef" {
		t.Fatalf("resume cancels the wrong alarm: %+v", cancel)
	}
	if message.Kind != action.SessionSend || message.Target != target || !strings.Contains(message.Text, `from="pause over"`) {
		t.Fatalf("resume message: %+v", message)
	}
}

func nothingMore(t *testing.T, fake *fakeExec) {
	t.Helper()
	select {
	case req := <-fake.got:
		t.Fatalf("unexpected recipient: %+v", req)
	default:
	}
}

func pauseJournal(t *testing.T, srv *Server) []string {
	t.Helper()
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(t.Context(), `SELECT target FROM actions WHERE kind='project.pause' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatal(err)
		}
		out = append(out, target)
	}
	return out
}

func TestProjectPauseUsesTheMapAndResumesItsAlarmsPG(t *testing.T) {
	srv, fake, anchor, until := pauseServer(t)
	before := len(pauseJournal(t, srv))
	for _, scope := range []bool{false, true} {
		want := 2
		if scope {
			want = 3
		}
		sendPause(t, srv, action.Pause{Project: anchor.ID, Group: scope, Until: until.Format(time.RFC3339)}, want)
		var targets []string
		for range want {
			req := <-fake.got
			targets = append(targets, req.Target)
			pauseInstruction(t, req, until)
		}
		if got := strings.Join(targets, ","); got != "lead,child" && !(scope && got == "lead,child,thread") {
			t.Fatalf("scope recipients: %s", got)
		}
	}
	sendPause(t, srv, action.Pause{Project: anchor.ID, Group: true, Resume: true}, 1)
	resumeOf(t, fake, "child")
	nothingMore(t, fake)
	if n := len(pauseJournal(t, srv)) - before; n != 3 {
		t.Fatalf("three scope actions wrote %d journal rows", n)
	}
	srv.exec, _ = startFakeExec(t, action.Response{OK: false, Error: "session is gone"})
	code, body := postPause(t, srv, action.Pause{Project: anchor.ID, Until: until.Format(time.RFC3339)})
	if code != http.StatusOK || !strings.Contains(body, `"delivered":0`) || !strings.Contains(body, "session is gone") {
		t.Fatalf("refusals disappeared: %d %s", code, body)
	}
}

// One session of a project is paused and resumed by the same messages as
// the project, and nothing reaches its neighbours; a session the screen put
// in the wrong project is refused before anything is sent.
func TestProjectPauseOfOneSessionPG(t *testing.T) {
	srv, fake, anchor, until := pauseServer(t)
	before := len(pauseJournal(t, srv))
	sendPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Until: until.Format(time.RFC3339)}, 1)
	req := <-fake.got
	if req.Target != "child" {
		t.Fatalf("the pause of one session reached %s", req.Target)
	}
	pauseInstruction(t, req, until)
	nothingMore(t, fake)

	sendPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Resume: true}, 1)
	resumeOf(t, fake, "child")
	nothingMore(t, fake)

	if code, body := postPause(t, srv, action.Pause{Project: anchor.ID, Session: "lead", Resume: true}); code != http.StatusConflict {
		t.Fatalf("a resume of a session with no pause: %d %s", code, body)
	}
	for _, name := range []string{"thread", "unmapped"} {
		code, body := postPause(t, srv, action.Pause{Project: anchor.ID, Session: name, Until: until.Format(time.RFC3339)})
		if code != http.StatusBadRequest || !strings.Contains(body, "does not run in project") {
			t.Fatalf("a pause of %s outside the project: %d %s", name, code, body)
		}
	}
	if code, body := postPause(t, srv, action.Pause{Project: anchor.ID, Session: "gone", Until: until.Format(time.RFC3339)}); code != http.StatusConflict {
		t.Fatalf("a pause of a session that is not live: %d %s", code, body)
	}
	if code, body := postPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Group: true, Resume: true}); code != http.StatusBadRequest {
		t.Fatalf("a pause of one session with its group: %d %s", code, body)
	}
	nothingMore(t, fake)
	if got := strings.Join(pauseJournal(t, srv)[before:], ","); got != "child,child" {
		t.Fatalf("the journal names %q, want the session twice", got)
	}
}
