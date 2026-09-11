package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
)

func fakeTmux(t *testing.T, panes []string, screen string) string {
	t.Helper()
	return newTmuxStub(t, panes, screen).log
}

type tmuxStub struct {
	t   *testing.T
	dir string
	log string
}

func newTmuxStub(t *testing.T, panes []string, screen string) *tmuxStub {
	t.Helper()
	dir := t.TempDir()
	s := &tmuxStub{t: t, dir: dir, log: filepath.Join(dir, "argv")}
	bin := filepath.Join(dir, "tmux")

	s.reply("list-panes", strings.Join(panes, "\n"))
	s.reply("capture-pane", screen)

	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a" >> %q; done
printf -- '--\n' >> %q
f=%q/reply-"$1"
if [ -f "$f" ]; then cat "$f"; fi
exit 0
`, s.log, s.log, dir)

	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return s
}

func (s *tmuxStub) reply(cmd, body string) {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.dir, "reply-"+cmd), []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func tmuxArgv(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func TestTmuxPaneFoundByPID(t *testing.T) {
	fakeTmux(t, []string{"4100 work:0.0", "4200 aacpanel:1.2"}, "")
	procFS(t, fakeProc{pid: 4200, comm: "claude", ppid: 1})

	pane, err := tmuxPaneFor(t.Context(), 4200)
	if err != nil {
		t.Fatalf("the pane was not found: %v", err)
	}
	if pane.Target != "aacpanel:1.2" {
		t.Errorf("pane %q, expected aacpanel:1.2 — missing the pane means a reply in a neighbouring conversation", pane.Target)
	}
}

func TestTmuxPaneFoundThroughShell(t *testing.T) {
	fakeTmux(t, []string{"700 work:0.0"}, "")
	procFS(t,
		fakeProc{pid: 900, comm: "claude", ppid: 700},
		fakeProc{pid: 700, comm: "bash", ppid: 1},
	)

	pane, err := tmuxPaneFor(t.Context(), 900)
	if err != nil {
		t.Fatalf("the pane behind a shell was not found: %v", err)
	}
	if pane.Target != "work:0.0" {
		t.Errorf("pane %q, expected work:0.0", pane.Target)
	}
}

func TestTmuxPaneMissIsNamed(t *testing.T) {
	fakeTmux(t, []string{"4100 work:0.0"}, "")
	procFS(t, fakeProc{pid: 4200, comm: "claude", ppid: 1})

	_, err := tmuxPaneFor(t.Context(), 4200)
	if err == nil {
		t.Fatal("a pane of another was taken for ours — the reply would land in a conversation of another")
	}
	if !strings.Contains(err.Error(), "4200") {
		t.Errorf("the error %q does not name the process that was not found", err)
	}
}

func TestTmuxSendKeepsTextWhole(t *testing.T) {
	log := fakeTmux(t, nil, "")
	pane := tmuxPane{Target: "aacpanel:0.0"}

	const text = "-not a flag but a reply with spaces"
	if err := pane.send(t.Context(), text); err != nil {
		t.Fatalf("the input did not go out: %v", err)
	}

	argv := tmuxArgv(t, log)
	want := []string{"send-keys", "-t", "aacpanel:0.0", "-l", "--", text, "--"}
	if len(argv) != len(want) {
		t.Fatalf("tmux got %q, expected %q", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Errorf("argument %d = %q, expected %q", i, argv[i], want[i])
		}
	}
}

func TestTmuxSendRefusesNUL(t *testing.T) {
	fakeTmux(t, nil, "")
	pane := tmuxPane{Target: "aacpanel:0.0"}

	if err := pane.send(t.Context(), "head\x00tail"); err == nil {
		t.Fatal("a NUL byte was accepted: the tail of the reply would vanish silently")
	}
}

func TestTmuxScreenReadsPane(t *testing.T) {
	const screen = "❯ a reply\n"
	log := fakeTmux(t, nil, screen)
	pane := tmuxPane{Target: "aacpanel:1.2"}

	got, ok := pane.screen(t.Context())
	if !ok {
		t.Fatal("the screen was not read")
	}
	if got != screen {
		t.Errorf("screen %q, expected %q", got, screen)
	}
	argv := tmuxArgv(t, log)
	if len(argv) < 4 || argv[0] != "capture-pane" || argv[3] != "aacpanel:1.2" {
		t.Errorf("the wrong pane was captured: %q", argv)
	}
}

func TestTermForPrefersTmux(t *testing.T) {
	fakeBusctl(t, map[string]int{"/Sessions/1": 900})
	fakeTmux(t, []string{"900 aacpanel:0.0"}, "")
	procFS(t,
		fakeProc{pid: 900, comm: "claude", ppid: 800},
		fakeProc{pid: 800, comm: "konsole", ppid: 1},
	)

	term, err := termFor(t.Context(), 900)
	if err != nil {
		t.Fatalf("the terminal was not found: %v", err)
	}
	if term.kind() != "tmux" {
		t.Errorf("%s was picked, expected tmux: inside a window the input has to go to its own pane", term.kind())
	}
}

func TestTermForNamesBothReasons(t *testing.T) {
	fakeBusctl(t, map[string]int{"/Sessions/1": 111})
	fakeTmux(t, []string{"222 work:0.0"}, "")
	procFS(t, fakeProc{pid: 900, comm: "claude", ppid: 1})

	_, err := termFor(t.Context(), 900)
	if err == nil {
		t.Fatal("a terminal was found where there is none")
	}
	msg := err.Error()
	if !strings.Contains(msg, "konsole") || !strings.Contains(msg, "tmux") {
		t.Errorf("the error %q names only one of the two environments", msg)
	}
}

func TestLiveTmuxPaste(t *testing.T) {
	raw := os.Getenv("AACP_LIVE_PID")
	if raw == "" {
		t.Skip("no live session is named: AACP_LIVE_PID=<pid of claude in tmux>")
	}
	pid, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("AACP_LIVE_PID=%q — that is not a pid", raw)
	}
	text := os.Getenv("AACP_LIVE_TEXT")
	if text == "" {
		t.Fatal("AACP_LIVE_TEXT is empty: there is nothing to send")
	}

	pane, err := tmuxPaneFor(t.Context(), pid)
	if err != nil {
		t.Fatalf("the pane of the session was not found: %v", err)
	}
	t.Logf("pane: %s", pane.Target)

	start := time.Now()
	confirmed, err := pasteAndSend(t.Context(), pane, text, nil)
	t.Logf("pasteAndSend: confirmed=%v error=%v in %s", confirmed, err, time.Since(start).Round(time.Millisecond))
	if err != nil {
		t.Fatalf("the reply was not delivered: %v", err)
	}
	if !confirmed {
		t.Error("the send is unconfirmed — inside tmux the screen has to be readable always")
	}
}

var sessionKinds = []action.Kind{
	action.SessionOpen, action.SessionResume, action.SessionClose, action.SessionKill,
	action.SessionSend, action.SessionAnswer, action.SessionDismiss, action.SessionStop,
	action.SessionFile, action.SessionCommand, action.SessionPermit,
}

var windowKinds = []action.Kind{action.WindowOpen, action.WindowClose}

var workKinds = []action.Kind{action.TaskStop, action.AgentStop}

func tmuxHere(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return bin
}

func hasKind(list []action.Kind, want action.Kind) bool {
	for _, k := range list {
		if k == want {
			return true
		}
	}
	return false
}

func TestKindsDropSessionsWithoutTmux(t *testing.T) {
	e := &Executor{}

	t.Run("tmux is there — the list is complete", func(t *testing.T) {
		tmuxHere(t)
		if got := e.Kinds(); len(got) != len(action.Kinds) {
			t.Fatalf("%d actions out of %d are offered with a live tmux", len(got), len(action.Kinds))
		}
	})

	t.Run("no tmux — no sessions, the rest is in place", func(t *testing.T) {
		t.Setenv(tmuxEnv, filepath.Join(t.TempDir(), "no-such-binary"))
		got := e.Kinds()
		gone := append(append([]action.Kind{}, sessionKinds...), windowKinds...)
		gone = append(gone, workKinds...)
		for _, k := range gone {
			if hasKind(got, k) {
				t.Errorf("%s is offered without tmux — the button will answer with the text of an unrelated error", k)
			}
		}
		for _, k := range []action.Kind{action.ContainerStart, action.ContainerStop,
			action.ContainerRestart, action.StackUp, action.StackDown} {
			if !hasKind(got, k) {
				t.Errorf("%s disappeared along with the sessions", k)
			}
		}
		if !hasKind(got, action.ProjectCreate) {
			t.Error("project.create disappeared along with the sessions — there is nothing left to start a project with on such a machine")
		}
		if len(action.Kinds) != len(sessionKinds)+len(windowKinds)+len(workKinds)+6 {
			t.Errorf("action.Kinds changed: it holds %d kinds while the test knows %d",
				len(action.Kinds), len(sessionKinds)+len(windowKinds)+len(workKinds)+6)
		}
	})
}

func TestKindsAreRecountedOnEveryAsk(t *testing.T) {
	e := &Executor{}
	bin := tmuxHere(t)
	if len(e.Kinds()) != len(action.Kinds) {
		t.Fatal("with a live tmux the list is already incomplete on the first question")
	}

	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if got := e.Kinds(); len(got) != len(action.Kinds)-len(sessionKinds)-len(windowKinds)-len(workKinds) {
		t.Errorf("after tmux went missing %d actions are offered — the answer was remembered, not recomputed", len(got))
	}

	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := e.Kinds(); len(got) != len(action.Kinds) {
		t.Errorf("tmux is installed, yet still %d are offered — the panel will say «no» on a machine that is already fixed", len(got))
	}
}

func TestKindsDropWindowOpenWithoutTerminal(t *testing.T) {
	e := &Executor{}
	tmuxHere(t)

	t.Setenv("AACP_TERMINAL", "")
	got := e.Kinds()
	if hasKind(got, action.WindowOpen) {
		t.Error("window.open is offered with an empty AACP_TERMINAL — the button will answer «there is nothing to open a window with»")
	}
	if !hasKind(got, action.WindowClose) {
		t.Error("window.close disappeared along with the open — a window raised by hand has nothing left to close it")
	}
	for _, k := range sessionKinds {
		if !hasKind(got, k) {
			t.Errorf("%s disappeared because AACP_TERMINAL is empty", k)
		}
	}

	t.Setenv("AACP_TERMINAL", "konsole")
	if got := e.Kinds(); !hasKind(got, action.WindowOpen) {
		t.Error("the template is set, yet window.open did not come back — the answer was remembered, not recomputed")
	}
}
