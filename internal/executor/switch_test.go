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
	f.state.Said = true
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
	noSeen(t)
	fakeWindowTmux(t, []string{"1004 demo:0.0"}, nil, dir)
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
		// A command started with "!" lives inside claude, and its output
		// reaches the conversation only through the holder of this claude.
		{"a shell command still running", func(s *stream.State) {
			s.Shells = []stream.Shell{{UUID: "u2", Command: "make check"}}
		}, true, "1 shell command started from the panel"},
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
		s.Picked, s.Effort = "opus[1m]", "max"
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
		"permissionMode": "acceptEdits", "model": "opus[1m]", "effort": "max", "room": "work"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s reached the launcher as %v, expected %v", k, got[k], v)
		}
	}
	if _, ok := got["intent"]; ok {
		t.Error("the opening message went into a resumed conversation: it would read it as a new request")
	}
	for _, say := range []string{"moved to tmux", streamSID, "mode acceptEdits", "1 background task stopped: make check"} {
		if !strings.Contains(detail, say) {
			t.Errorf("the report %q does not say %q", detail, say)
		}
	}
}

func TestSwitchToStreamCarriesWhatTheConsoleShows(t *testing.T) {
	dir := consoleStand(t, "idle")
	models := os.Getenv(sessionModelsEnv)
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	seen := `{"at":1,"sessionId":"` + consoleSID + `","model":{"id":"claude-opus-5-5[1m]"},"effort":"xhigh"}`
	if err := os.WriteFile(filepath.Join(models, consoleSID+".json"), []byte(seen), 0o644); err != nil {
		t.Fatal(err)
	}
	collector := startFakeSeen(t)
	collector.mode = "auto"
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(signals.sent, ",") != "1004:terminated" {
		t.Errorf("signals %v, expected one TERM to the session in tmux", signals.sent)
	}
	got := launched(t, log)
	want := map[string]any{"_resume": consoleSID, "transport": "stream",
		"model": "claude-opus-5-5[1m]", "effort": "xhigh", "permissionMode": "auto"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s reached the launcher as %v, expected %v", k, got[k], v)
		}
	}
	if !strings.Contains(detail, "moved to the stream") {
		t.Errorf("the report %q does not say where the session went", detail)
	}
	if len(collector.since) != 1 || collector.since[0] != consoleStarted.UnixMilli() {
		t.Errorf("the collector was asked for the mode since %v, not since the console started", collector.since)
	}
}

// A console that has not said a word since it started is in the mode it was
// started with: the collector names no mode of it, since one said before the
// start is of a process that came before.
func TestSwitchToStreamTakesTheStartModeWhenTheConsoleSaidNone(t *testing.T) {
	dir := consoleStand(t, "idle", "--permission-mode", "acceptEdits")
	startFakeSeen(t)
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log)["permissionMode"]; got != "acceptEdits" {
		t.Errorf("the stream starts in mode %v, while tmux was in acceptEdits", got)
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

// A stream session started in a named mode — by a switch from tmux, say —
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
		t.Errorf("tmux starts in mode %v, while the stream was started in acceptEdits", got)
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

// A console that a terminal outside the panel shows stays where it is: the
// switch would end the conversation under the eyes of whoever reads it there.
func TestSwitchToStreamStopsWhileATerminalShowsTheConsole(t *testing.T) {
	cases := []struct {
		name           string
		panes, clients []string
		says           string
	}{
		{"a window on the host", []string{"1004 demo:0.0"}, []string{"/dev/pts/7 4321"}, "close the window first"},
		{"a terminal of its own", []string{"2002 other:0.0"}, nil, "does not live in tmux"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := consoleStand(t, "idle")
			fakeWindowTmux(t, c.panes, c.clients, dir)
			log := fakeLauncher(t, launcher.Report{Session: "demo"})
			e, _ := newTest(t, "")
			signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

			_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the switch was not stopped with %q: %v", c.says, err)
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

// panelTermStand is the console of consoleStand typed into a terminal of the
// panel: claude under the shell of terminal t-1a2b3c4d on the panel's server,
// with the clients given attached to the terminal. Client 950 is a bridge of
// the panel, a child of the executor; 4321 is a window on the host.
func panelTermStand(t *testing.T, clients []string) (dir, log string) {
	t.Helper()
	dir = consoleStand(t, "idle")
	procFS(t,
		fakeProc{pid: 1004, comm: "claude", ppid: 700, cwd: dir, start: "1000", args: []string{"claude", "-n", "demo"}},
		fakeProc{pid: 700, comm: "bash", args: []string{"bash"}, ppid: 1},
		fakeProc{pid: 950, comm: "tmux", args: []string{"tmux", "attach-session"}, ppid: os.Getpid()},
		fakeProc{pid: 4321, comm: "tmux", args: []string{"tmux", "attach"}, ppid: 1},
	)
	log = twoServersTmux(t,
		tmuxSide{panes: []string{"3003 shop:0.0"}},
		tmuxSide{panes: []string{"700 t-1a2b3c4d:0.0"}, clients: clients, dir: dir})
	return dir, log
}

// A claude typed into a terminal of the panel is a console like one the panel
// started: it moves to the stream, closed the way a console is, and the shell
// of the terminal stays where it was. The panel showing the terminal does not
// hold the session there.
func TestSwitchToStreamFromATerminalOfThePanel(t *testing.T) {
	dir, tmuxLog := panelTermStand(t, []string{"/dev/pts/9 950"})
	startFakeSeen(t)
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(signals.sent, ",") != "1004:terminated" {
		t.Errorf("signals %v, expected one TERM to claude and nothing to the shell of the terminal", signals.sent)
	}
	if got := launched(t, log); got["transport"] != "stream" || got["_resume"] != consoleSID {
		t.Errorf("the launcher was asked for %v, expected conversation %s on the stream", got, consoleSID)
	}
	if !strings.Contains(detail, "moved to the stream") {
		t.Errorf("the report %q does not say where the session went", detail)
	}
	if tmuxCall(callsTo(t, tmuxLog, panelTmux), "list-clients") == nil {
		t.Error("whether a window shows the terminal was not asked of the panel's server")
	}
	for _, srv := range []tmuxServer{userTmux, panelTmux} {
		if kill := tmuxCall(callsTo(t, tmuxLog, srv), "kill-session"); kill != nil {
			t.Errorf("%s was asked to %v — the terminal of the panel is the person's shell and outlives the claude typed into it",
				srv, kill)
		}
	}
}

// A window on the host attached to the terminal of the panel shows the
// console as much as one attached to a session of the user's tmux: the switch
// would end the conversation under the eyes of whoever reads it there.
func TestSwitchToStreamStopsWhileAWindowShowsTheTerminalOfThePanel(t *testing.T) {
	dir, _ := panelTermStand(t, []string{"/dev/pts/9 950", "/dev/pts/7 4321"})
	log := fakeLauncher(t, launcher.Report{Session: "demo"})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
	if err == nil || !strings.Contains(err.Error(), "close the window first") {
		t.Fatalf("the switch was not stopped by the window on the terminal: %v", err)
	}
	if len(signals.sent) != 0 {
		t.Errorf("the console was signalled though the switch stopped: %v", signals.sent)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("the launcher was called though the switch stopped")
	}
}

// A window asked for with the switch opens onto the session the launcher
// started in tmux; one that does not open leaves the switch done.
func TestSwitchToConsoleOpensTheWindowItWasAskedFor(t *testing.T) {
	body, err := json.Marshal(launcher.Report{Session: "demo-2", Transport: launcher.TransportTmux})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, window, says string
	}{
		{"it opens", "", "a window to session demo-2 is open"},
		{"it does not", "echo 'no display to open it on' >&2; exit 1", "the window did not open"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, _ := streamStand(t, func(*stream.State) {})
			log := launcherScript(t, "{ echo \"$@\"; cat; echo; } >> %s\n"+
				"case \"$*\" in *-window*) "+c.window+" ;; esac\n"+
				"cat <<'END'\n"+string(body)+"\nEND\n")
			fakeWindowTmux(t, nil, []string{"/dev/pts/7 4321"}, dir)
			e, _ := newTest(t, "")
			withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

			r := switchTo(action.SwitchConsole, false, dir)
			r.Switch.Window = true
			detail, err := e.Execute(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			for _, say := range []string{"moved to tmux", c.says} {
				if !strings.Contains(detail, say) {
					t.Errorf("the report %q does not say %q", detail, say)
				}
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := `"dir":"` + dir + `","session":"demo-2"`
			if !strings.Contains(string(raw), windowFlag) || !strings.Contains(string(raw), want) {
				t.Errorf("the window opener was not asked for %s: %s", want, raw)
			}
		})
	}
}

func TestSwitchGoesOnlyToTheOtherSide(t *testing.T) {
	t.Run("tmux to tmux", func(t *testing.T) {
		dir := consoleStand(t, "idle")
		e, _ := newTest(t, "")
		_, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, false, dir))
		if err == nil || !strings.Contains(err.Error(), "in tmux already") {
			t.Fatalf("a session in tmux was moved to tmux: %v", err)
		}
	})
	t.Run("the stream to the stream", func(t *testing.T) {
		dir, _ := streamStand(t, func(*stream.State) {})
		e, _ := newTest(t, "")
		_, err := e.Execute(context.Background(), switchTo(action.SwitchStream, false, dir))
		if err == nil || !strings.Contains(err.Error(), "on the stream already") {
			t.Fatalf("a stream session was moved to the stream: %v", err)
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

// Closing a session on the stream ends its input, the way a finished run ends:
// a signal to a young session reads as a launch that failed and leaves a log.
func TestClosingAStreamSessionEndsItsInput(t *testing.T) {
	_, holder := streamStand(t, func(*stream.State) {})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	detail, err := e.Execute(context.Background(), req(action.SessionClose, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(signals.sent) != 0 {
		t.Errorf("signals %v went to a stream session that ends cleanly when its input is closed", signals.sent)
	}
	if got := holder.asked(); len(got) != 1 || got[0].Op != stream.OpClose {
		t.Errorf("the holder was asked %+v, expected to close the input of its session", got)
	}
	if !strings.Contains(detail, "closed gracefully") {
		t.Errorf("the report %q does not say the session closed", detail)
	}
}

// With the panel down, a session on the stream goes to tmux from what its
// holder keeps it was started with: the project, its launch and its contour —
// a session under another account would be another person's.
func TestSwitchToConsoleWithoutThePanelStartsFromWhatTheHolderKeeps(t *testing.T) {
	dir, f := streamStand(t, func(*stream.State) {})
	kept, err := json.Marshal(launcher.Spec{Dir: dir, Session: "demo", Launch: json.RawMessage(`{"model":"opus","room":"work"}`),
		ClaudeBin: "/opt/claude-work", ConfigDir: "/home/u/.claude-profiles/work"})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.state.Launched = kept
	f.mu.Unlock()
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	r := req(action.SessionSwitch, "demo")
	r.Switch = &action.Switch{To: action.SwitchConsole}
	if _, err := e.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var spec launcher.Spec
	body := raw[strings.Index(string(raw), "{") : strings.LastIndex(string(raw), "}")+1]
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if spec.Dir != dir || spec.ConfigDir != "/home/u/.claude-profiles/work" || spec.ClaudeBin != "/opt/claude-work" ||
		spec.Resume != streamSID {
		t.Errorf("the console was started with %+v", spec)
	}
	if got := launched(t, log); got["model"] != "opus" || got["transport"] != "tmux" {
		t.Errorf("the launch of the console is %v", got)
	}
}

// A holder that does not keep what the session was started with is not
// guessed around: a project found by its directory has no contour.
func TestSwitchToConsoleWithoutThePanelNeedsWhatTheHolderKeeps(t *testing.T) {
	streamStand(t, func(*stream.State) {})
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	r := req(action.SessionSwitch, "demo")
	r.Switch = &action.Switch{To: action.SwitchConsole}
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "does not keep") {
		t.Fatalf("a switch without the project and without what the holder keeps: %v", err)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("the launcher was called with a guessed project")
	}
}

// A session on the stream nobody has said a word in has no transcript: the
// console starts anew under its name rather than resuming what is not there,
// which would close the session and bring nothing up.
func TestSwitchToConsoleOfAConversationWithNoWordStartsAnew(t *testing.T) {
	dir, _ := streamStand(t, func(s *stream.State) { s.Said = false })
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	detail, err := e.Execute(context.Background(), switchTo(action.SwitchConsole, false, dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log)["_resume"]; got != "" {
		t.Errorf("a conversation with no word was resumed as %v", got)
	}
	if !strings.Contains(detail, "as a new conversation") {
		t.Errorf("the report %q does not say the conversation starts anew", detail)
	}
}
