package main

import (
	"context"
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

func TestProjectPauseExecutesEveryStepThroughTheRealDeduplicatorPG(t *testing.T) {
	srv, _, project, _ := pauseServer(t, nil)
	socket := socketPath(t, "pause.sock")
	seen := make(chan action.Request, 8)
	exec := action.NewServer(socket, action.ExecutorFunc(func(_ context.Context, r action.Request) (string, error) { seen <- r; return "delivered", nil }), time.Second)
	ln, err := exec.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- exec.Run(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("the test executor did not stop")
		}
	})
	srv.exec = action.NewClient(socket, time.Second)
	sendPause(t, srv, action.Pause{Project: project.ID, Session: "child", Resume: true}, 1)
	if len(seen) != 2 {
		t.Fatalf("Continue executed %d steps; cancellation and message need distinct request IDs", len(seen))
	}
	first, second := <-seen, <-seen
	if first.Kind != action.AlarmCancel || second.Kind != action.SessionSend || first.ID == second.ID {
		t.Fatalf("Continue steps: %+v %+v", first, second)
	}
}

// pauseServer is a service over a map of two projects in one group, and a
// snapshot with a session of the first project, a session of its worktree that
// keeps a pause alarm, a codex thread of the second project, and two sessions
// the map does not hold; more adds the rows a test needs, given the directory
// of the second project.
func pauseServer(t *testing.T, more func(other string) []map[string]any) (*Server, *fakeExec, store.ProfileProject, time.Time) {
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
	if more != nil {
		rows = append(rows, more(path)...)
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

func pauseMessageRequest(t *testing.T, fake *fakeExec) action.Request {
	t.Helper()
	req := <-fake.got
	if req.Kind == action.AlarmCancel {
		if req.Work == nil || req.Work.ID == "" {
			t.Fatalf("legacy pause cancellation: %+v", req)
		}
		req = <-fake.got
	}
	return req
}

func pauseInstruction(t *testing.T, req action.Request, until time.Time) {
	t.Helper()
	if req.Kind != action.SessionSend || !strings.Contains(req.Text, `from="pause" until="`+until.Format(time.RFC3339)+`"`) ||
		!strings.Contains(req.Text, until.Format("15:04, 2 Jan 2006")) || !(strings.Contains(req.Text, "put this session to sleep") || (req.Target == "thread" && strings.Contains(req.Text, "alarm_set"))) {
		t.Fatalf("pause instruction: %+v", req)
	}
}

// noEndInstruction is the instruction of a pause with no end: a sender of its
// own and no moment in its envelope, and a repeat of the alarm that marks it.
func noEndInstruction(t *testing.T, req action.Request) {
	t.Helper()
	if req.Kind != action.SessionSend || !strings.Contains(req.Text, `<panel-message from="pause with no end">`) ||
		!strings.Contains(req.Text, "put this session to sleep") || strings.Contains(req.Text, "alarm_set") {
		t.Fatalf("the instruction of a pause with no end: %+v", req)
	}
}

func resumeOf(t *testing.T, fake *fakeExec, target, alarm string) {
	t.Helper()
	message := <-fake.got
	if message.Kind == action.AlarmCancel {
		if message.Target != target || message.Work == nil || message.Work.ID != alarm {
			t.Fatalf("resume cancels the wrong alarm: %+v", message)
		}
		message = <-fake.got
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
	srv, fake, anchor, until := pauseServer(t, nil)
	before := len(pauseJournal(t, srv))
	for _, scope := range []bool{false, true} {
		want := 2
		if scope {
			want = 3
		}
		sendPause(t, srv, action.Pause{Project: anchor.ID, Group: scope, Until: until.Format(time.RFC3339)}, want)
		var targets []string
		for range want {
			req := pauseMessageRequest(t, fake)
			targets = append(targets, req.Target)
			pauseInstruction(t, req, until)
		}
		if got := strings.Join(targets, ","); got != "lead,child" && !(scope && got == "lead,child,thread") {
			t.Fatalf("scope recipients: %s", got)
		}
	}
	sendPause(t, srv, action.Pause{Project: anchor.ID, Group: true, Resume: true}, 2)
	resumeOf(t, fake, "lead", "")
	resumeOf(t, fake, "child", "0123456789abcdef")
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
	srv, fake, anchor, until := pauseServer(t, nil)
	before := len(pauseJournal(t, srv))
	sendPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Until: until.Format(time.RFC3339)}, 1)
	req := pauseMessageRequest(t, fake)
	if req.Target != "child" {
		t.Fatalf("the pause of one session reached %s", req.Target)
	}
	pauseInstruction(t, req, until)
	nothingMore(t, fake)

	sendPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Resume: true}, 1)
	resumeOf(t, fake, "child", "0123456789abcdef")
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

// A pause goes one step down from its scope: to the sessions a session of it
// had the panel open, wherever they run, and it is lifted from them the same
// way. A session one of those opened, and a session nobody opened, are left
// alone. The worktree session opened a helper in the second project, and the
// helper opened one more.
func TestProjectPauseGoesOneStepDownPG(t *testing.T) {
	srv, fake, anchor, until := pauseServer(t, func(other string) []map[string]any {
		alarm := func(id, name string) pauseAlarm {
			return pauseAlarm{ID: id, Name: name, Next: time.Now().Add(time.Hour).Format(time.RFC3339)}
		}
		return []map[string]any{
			{"session": "helper", "sessionId": "33333333-3333-4333-8333-333333333333", "cwd": other,
				"parent": host.SessionRef{Session: "child"}, "pauseAlarm": alarm("1111111111111111", action.PauseNoEndAlarm)},
			{"session": "grandchild", "sessionId": "22222222-2222-4222-8222-222222222222", "cwd": other,
				"parent": host.SessionRef{Session: "helper"}, "pauseAlarm": alarm("2222222222222222", action.PauseAlarm)},
			{"session": "stranger", "sessionId": "11111111-1111-4111-8111-111111111111", "cwd": other,
				"pauseAlarm": alarm("3333333333333333", action.PauseAlarm)},
		}
	})
	recipients := func(n int, instruction func(action.Request)) string {
		var names []string
		for range n {
			req := pauseMessageRequest(t, fake)
			instruction(req)
			names = append(names, req.Target)
		}
		nothingMore(t, fake)
		return strings.Join(names, ",")
	}
	sendPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Until: until.Format(time.RFC3339)}, 2)
	if got := recipients(2, func(req action.Request) { pauseInstruction(t, req, until) }); got != "child,helper" {
		t.Fatalf("the pause of a session reached %s", got)
	}
	sendPause(t, srv, action.Pause{Project: anchor.ID, NoEnd: true}, 3)
	if got := recipients(3, func(req action.Request) { noEndInstruction(t, req) }); got != "lead,child,helper" {
		t.Fatalf("the pause of a project reached %s", got)
	}
	sendPause(t, srv, action.Pause{Project: anchor.ID, Session: "child", Resume: true}, 2)
	resumeOf(t, fake, "child", "0123456789abcdef")
	resumeOf(t, fake, "helper", "1111111111111111")
	nothingMore(t, fake)
	if code, body := postPause(t, srv, action.Pause{Project: anchor.ID, NoEnd: true, Until: until.Format(time.RFC3339)}); code != http.StatusBadRequest {
		t.Fatalf("a pause with no end and an end: %d %s", code, body)
	}
	nothingMore(t, fake)
}
