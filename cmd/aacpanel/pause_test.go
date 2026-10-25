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

func TestProjectPauseUsesTheMapAndResumesItsAlarmsPG(t *testing.T) {
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
	send := func(p action.Pause, want int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"kind": action.ProjectPause, "params": p})
		w := post(t, srv, string(raw))
		if w.Code != http.StatusOK {
			t.Fatalf("pause: %d %s", w.Code, w.Body.String())
		}
		var reply struct {
			Delivered int
			Failed    []map[string]string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Delivered != want || len(reply.Failed) != 0 {
			t.Fatalf("pause response: %s", w.Body.String())
		}
	}
	for _, scope := range []bool{false, true} {
		want := 2
		if scope {
			want = 3
		}
		send(action.Pause{Project: anchor.ID, Group: scope, Until: until.Format(time.RFC3339)}, want)
		var targets []string
		for range want {
			req := <-fake.got
			targets = append(targets, req.Target)
			if req.Kind != action.SessionSend || !strings.Contains(req.Text, `from="pause" until="`+until.Format(time.RFC3339)+`"`) ||
				!strings.Contains(req.Text, until.Format("15:04, 2 Jan 2006")) || !strings.Contains(req.Text, `name: "pause"`) || !strings.Contains(req.Text, "alarm_set") {
				t.Fatalf("pause instruction: %+v", req)
			}
		}
		if got := strings.Join(targets, ","); got != "lead,child" && !(scope && got == "lead,child,thread") {
			t.Fatalf("scope recipients: %s", got)
		}
	}
	send(action.Pause{Project: anchor.ID, Group: true, Resume: true}, 1)
	cancel, message := <-fake.got, <-fake.got
	if cancel.Kind != action.AlarmCancel || cancel.Target != "child" || cancel.Work == nil || cancel.Work.ID != "0123456789abcdef" {
		t.Fatalf("resume cancels the wrong alarm: %+v", cancel)
	}
	if message.Kind != action.SessionSend || message.Target != "child" || !strings.Contains(message.Text, `from="pause over"`) {
		t.Fatalf("resume message: %+v", message)
	}
	select {
	case req := <-fake.got:
		t.Fatalf("unexpected recipient: %+v", req)
	default:
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM actions WHERE kind='project.pause'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("three scope actions wrote %d journal rows", n)
	}
	srv.exec, _ = startFakeExec(t, action.Response{OK: false, Error: "session is gone"})
	raw, _ = json.Marshal(map[string]any{"kind": action.ProjectPause, "params": action.Pause{Project: anchor.ID, Until: until.Format(time.RFC3339)}})
	w := post(t, srv, string(raw))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"delivered":0`) || !strings.Contains(w.Body.String(), "session is gone") {
		t.Fatalf("refusals disappeared: %d %s", w.Code, w.Body.String())
	}
}
