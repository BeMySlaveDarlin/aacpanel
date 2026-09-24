package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

const consoleSID = "77777777-7777-4777-8777-777777777777"

func switchTo(to string, force bool, dir string) action.Request {
	r := req(action.SessionSwitch, "demo")
	r.Switch = &action.Switch{To: to, Force: force}
	r.Project = &action.Project{Path: dir, Session: "demo",
		Launch: json.RawMessage(`{"intent":"start the work","model":"opus","effort":"high","room":"work"}`)}
	return r
}

// streamStand is a session on the stream in a real project directory, with a
// holder answering the given state.
func streamStand(t *testing.T, st func(*stream.State), args ...string) (dir string, f *fakeHolder) {
	t.Helper()
	root, dir := launchDir(t, "proj")
	t.Setenv(projectRootsEnv, root)
	f = onTheStream(t, false)
	procFS(t, fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: dir, start: "5555",
		args: append([]string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", streamSID}, args...)})
	sessionFiles(t, fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID, cwd: dir})
	f.mu.Lock()
	st(&f.state)
	f.mu.Unlock()
	return dir, f
}

// consoleStarted is when the console of consoleStand started: a transcript
// line before it was written by another process of the conversation.
var consoleStarted = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

func consoleStand(t *testing.T, status string, args ...string) string {
	t.Helper()
	root, dir := launchDir(t, "proj")
	t.Setenv(projectRootsEnv, root)
	procFS(t, fakeProc{pid: 1004, comm: "claude", ppid: 1, cwd: dir, start: "1000",
		args: append([]string{"claude", "-n", "demo"}, args...)})
	sessionFiles(t, fakeSession{pid: 1004, name: "demo", start: "1000", sid: consoleSID, cwd: dir, status: status,
		startedAt: consoleStarted.UnixMilli()})
	conf := t.TempDir()
	t.Setenv(registry.HomeEnv, conf)
	t.Setenv(registry.RegistryEnv, "")
	t.Setenv(sessionModelsEnv, filepath.Join(conf, "session-models"))
	return dir
}

func launched(t *testing.T, log string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the launcher was not called: %v", err)
	}
	start := strings.Index(string(raw), "{")
	end := strings.LastIndex(string(raw), "}")
	if start < 0 || end < start {
		t.Fatalf("the launcher got no task: %s", raw)
	}
	var spec launcher.Spec
	if err := json.Unmarshal(raw[start:end+1], &spec); err != nil {
		t.Fatalf("the launcher task did not parse: %v: %s", err, raw)
	}
	var params map[string]any
	if err := json.Unmarshal(spec.Launch, &params); err != nil {
		t.Fatalf("the launch parameters did not parse: %v: %s", err, spec.Launch)
	}
	params["_resume"] = spec.Resume
	params["_session"] = spec.Session
	return params
}

func TestSwitchFromStreamStopsAtWhatItWouldLose(t *testing.T) {
	cases := []struct {
		name  string
		state func(*stream.State)
		force bool
		says  string
	}{
		{"a turn in progress", func(s *stream.State) { s.Busy = true }, true, "answering right now"},
		{"a request waiting for a person", func(s *stream.State) { s.Pending = []stream.Pending{bash("r1", "")} }, true, "waiting for an answer (Bash)"},
		{"messages in the queue", func(s *stream.State) { s.Queue = []stream.Queued{{UUID: "u1", Text: "next"}} }, true, "1 message in its queue"},
		{"background tasks nobody agreed to stop", func(s *stream.State) {
			s.Tasks = []stream.Task{{ID: "b1", Description: "make check"}}
		}, false, "1 background task that stop with the switch: make check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, _ := streamStand(t, c.state)
			log := fakeLauncher(t, launcher.Report{Session: "demo"})
			e, _ := newTest(t, "")
			signals := withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

			_, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, c.force, dir))
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the switch was not stopped with %q: %v", c.says, err)
			}
			if len(signals.sent) != 0 {
				t.Errorf("the session was signalled though the switch stopped: %v", signals.sent)
			}
			if _, err := os.Stat(log); err == nil {
				t.Error("the launcher was called though the switch stopped")
			}
		})
	}
}

func TestSwitchToConsoleResumesTheSameConversation(t *testing.T) {
	dir, holder := streamStand(t, func(s *stream.State) {
		s.Mode, s.StartMode = "acceptEdits", "default"
		s.Tasks = []stream.Task{{ID: "b1", Description: "make check"}}
	})
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, true, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(signals.sent) != 0 {
		t.Errorf("signals %v went to a session that ends cleanly when its input is closed", signals.sent)
	}
	if got := holder.asked(); len(got) != 1 || got[0].Op != stream.OpClose {
		t.Errorf("the holder was asked %+v, expected to close the input of its session", got)
	}
	got := launched(t, log)
	want := map[string]any{"_resume": streamSID, "_session": "demo", "transport": "tmux",
		"permissionMode": "acceptEdits", "model": "opus", "effort": "high", "room": "work"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s reached the launcher as %v, expected %v", k, got[k], v)
		}
	}
	if _, ok := got["intent"]; ok {
		t.Error("the opening message went into a resumed conversation: it would read it as a new request")
	}
	for _, say := range []string{"moved in the console", streamSID, "mode acceptEdits", "1 background task stopped: make check"} {
		if !strings.Contains(detail, say) {
			t.Errorf("the report %q does not say %q", detail, say)
		}
	}
}

func TestSwitchToStreamCarriesWhatTheConsoleShows(t *testing.T) {
	dir := consoleStand(t, "idle")
	conf := os.Getenv(registry.HomeEnv)
	models := os.Getenv(sessionModelsEnv)
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	seen := `{"at":1,"sessionId":"` + consoleSID + `","model":{"id":"claude-opus-5-5[1m]"},"effort":"xhigh"}`
	if err := os.WriteFile(filepath.Join(models, consoleSID+".json"), []byte(seen), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, conf,
		`{"type":"user","permissionMode":"default","timestamp":"2026-09-24T09:00:00Z"}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5"}}`,
		`{"type":"user","permissionMode":"auto","timestamp":"2026-09-24T10:05:00Z"}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5"}}`)
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(signals.sent, ",") != "1004:terminated" {
		t.Errorf("signals %v, expected one TERM to the console", signals.sent)
	}
	got := launched(t, log)
	want := map[string]any{"_resume": consoleSID, "transport": "stream",
		"model": "claude-opus-5-5[1m]", "effort": "xhigh", "permissionMode": "auto"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s reached the launcher as %v, expected %v", k, got[k], v)
		}
	}
	if !strings.Contains(detail, "moved in the feed") {
		t.Errorf("the report %q does not say where the session went", detail)
	}
}

func writeTranscript(t *testing.T, conf string, lines ...string) {
	t.Helper()
	slug := filepath.Join(conf, "projects", "-proj")
	if err := os.MkdirAll(slug, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(slug, consoleSID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A console that has not said a word since it started is in the mode it was
// started with; the mode in the transcript is of a process that came before.
func TestSwitchToStreamTakesTheStartModeOverAnOlderTranscript(t *testing.T) {
	dir := consoleStand(t, "idle", "--permission-mode", "acceptEdits")
	writeTranscript(t, os.Getenv(registry.HomeEnv),
		`{"type":"user","permissionMode":"auto","timestamp":"2026-09-24T09:59:00Z"}`)
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log)["permissionMode"]; got != "acceptEdits" {
		t.Errorf("the feed starts in mode %v, while the console was in acceptEdits", got)
	}
	if !strings.Contains(detail, "mode acceptEdits from its start") {
		t.Errorf("the report %q does not say where the mode came from", detail)
	}
}

// A stream session in the mode it started with is started on the other side
// with the same parameters: carrying the mode would put the protocol's name
// for it over the project's choice.
func TestSwitchToConsoleLeavesAnUnchangedModeToTheProject(t *testing.T) {
	dir, _ := streamStand(t, func(s *stream.State) { s.Mode, s.StartMode = "default", "default" })
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	if _, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, false, dir)); err != nil {
		t.Fatal(err)
	}
	if got, ok := launched(t, log)["permissionMode"]; ok {
		t.Errorf("an unchanged mode was carried over the project's parameters: %v", got)
	}
}

// A stream session started in a named mode — by a switch from a console, say —
// keeps it on the way back, changed or not.
func TestSwitchToConsoleCarriesTheModeTheStartNamed(t *testing.T) {
	dir, _ := streamStand(t, func(s *stream.State) { s.Mode, s.StartMode = "acceptEdits", "acceptEdits" },
		"--permission-mode", "acceptEdits")
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	if _, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, false, dir)); err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log)["permissionMode"]; got != "acceptEdits" {
		t.Errorf("the console starts in mode %v, while the feed was started in acceptEdits", got)
	}
}

func TestSwitchFromConsoleStopsOnATurnOrADialog(t *testing.T) {
	for status, says := range map[string]string{"busy": "answering right now", "waiting": "waiting for an answer in a dialog"} {
		t.Run(status, func(t *testing.T) {
			dir := consoleStand(t, status)
			log := fakeLauncher(t, launcher.Report{Session: "demo"})
			e, _ := newTest(t, "")
			signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

			_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, true, dir))
			if err == nil || !strings.Contains(err.Error(), says) {
				t.Fatalf("the switch was not stopped with %q: %v", says, err)
			}
			if len(signals.sent) != 0 {
				t.Errorf("the console was signalled though the switch stopped: %v", signals.sent)
			}
			if _, err := os.Stat(log); err == nil {
				t.Error("the launcher was called though the switch stopped")
			}
		})
	}
}

func TestSwitchGoesOnlyToTheOtherSide(t *testing.T) {
	t.Run("a console to the console", func(t *testing.T) {
		dir := consoleStand(t, "idle")
		e, _ := newTest(t, "")
		_, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, false, dir))
		if err == nil || !strings.Contains(err.Error(), "in the console already") {
			t.Fatalf("a console was moved to the console: %v", err)
		}
	})
	t.Run("the stream to the feed", func(t *testing.T) {
		dir, _ := streamStand(t, func(*stream.State) {})
		e, _ := newTest(t, "")
		_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
		if err == nil || !strings.Contains(err.Error(), "in the feed already") {
			t.Fatalf("a stream session was moved to the feed: %v", err)
		}
	})
	t.Run("a project somewhere else", func(t *testing.T) {
		consoleStand(t, "idle")
		_, other := launchDir(t, "other")
		e, _ := newTest(t, "")
		_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, other))
		if err == nil || !strings.Contains(err.Error(), "resume it somewhere else") {
			t.Fatalf("a session was resumed in another directory: %v", err)
		}
	})
}

func TestSwitchThatDidNotComeUpSaysTheConversationIsWhole(t *testing.T) {
	dir := consoleStand(t, "idle")
	failLauncher(t, "no tmux on this host")
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err == nil {
		t.Fatal("a launch that failed passed for a switch")
	}
	for _, say := range []string{"closed", "no tmux on this host", "resume it from the archive"} {
		if !strings.Contains(err.Error(), say) {
			t.Errorf("the error %q does not say %q", err, say)
		}
	}
}

func TestSwitchSignalsAStreamSessionThatWouldNotClose(t *testing.T) {
	dir, holder := streamStand(t, func(*stream.State) {})
	holder.mu.Lock()
	holder.fails = map[string]string{stream.OpClose: "the input is already closed"}
	holder.mu.Unlock()
	fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	if _, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, false, dir)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(signals.sent, ",") != "5001:terminated" {
		t.Errorf("signals %v, expected one TERM once the holder could not close its session", signals.sent)
	}
}
