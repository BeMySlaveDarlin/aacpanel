package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
if [ "$1" = -L ]; then shift 2; fi
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

// tmuxSide is what one server of twoServersTmux holds: the panes it lists, the
// clients it lists for any of its sessions and the directory of its panes. A
// side that is down answers the way tmux does with no server on the socket, and
// one that fails answers with its words.
type tmuxSide struct {
	down    bool
	fails   string
	panes   []string
	clients []string
	dir     string
}

// twoServersTmux plays the user's tmux and the server of the panel's terminals
// apart: a call with -L and the panel's socket is answered from the panel's
// side, any other from the user's. Every call is logged with the -L it went
// with.
func twoServersTmux(t *testing.T, user, panel tmuxSide) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	for name, side := range map[string]tmuxSide{"user": user, "panel": panel} {
		at := filepath.Join(dir, name)
		if err := os.Mkdir(at, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{
			"panes": strings.Join(side.panes, "\n"), "clients": strings.Join(side.clients, "\n"), "dir": side.dir,
		}
		if side.down {
			files["down"] = ""
		}
		if side.fails != "" {
			files["fails"] = side.fails
		}
		for file, body := range files {
			if err := os.WriteFile(filepath.Join(at, file), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a" >> %[1]q; done
printf -- '--\n' >> %[1]q
d=%[2]q/user
if [ "$1" = -L ]; then
	if [ "$2" = %[3]q ]; then d=%[2]q/panel; else d=%[2]q/none; fi
	shift 2
fi
if [ ! -d "$d" ] || [ -f "$d/down" ]; then echo "no server running on /tmp/tmux-1000/x" >&2; exit 1; fi
if [ -f "$d/fails" ]; then cat "$d/fails" >&2; exit 1; fi
case "$1" in
list-panes) cat "$d/panes" ;;
list-clients) cat "$d/clients" ;;
display) cat "$d/dir" ;;
esac
exit 0
`, log, dir, string(panelTmux))
	bin := filepath.Join(dir, "tmux")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return log
}

// callsTo are the calls the stub saw on one server, with the -L taken off.
func callsTo(t *testing.T, log string, srv tmuxServer) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range tmuxCalls(t, log) {
		on := userTmux
		if len(c) >= 2 && c[0] == "-L" {
			on, c = tmuxServer(c[1]), c[2:]
		}
		if on == srv {
			out = append(out, c)
		}
	}
	return out
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

// A claude typed into a terminal of the panel lives in a pane of the panel's
// own server: it is found there through the shell of the terminal, and what
// the panel types into it or reads off it goes to that server.
func TestTmuxPaneFoundInATerminalOfThePanel(t *testing.T) {
	log := twoServersTmux(t,
		tmuxSide{panes: []string{"4100 work:0.0"}},
		tmuxSide{panes: []string{"3300 t-0000beef:0.0", "700 t-1a2b3c4d:0.0"}})
	procFS(t,
		fakeProc{pid: 900, comm: "claude", ppid: 700},
		fakeProc{pid: 700, comm: "bash", ppid: 1},
	)

	pane, err := tmuxPaneFor(t.Context(), 900)
	if err != nil {
		t.Fatalf("the pane in a terminal of the panel was not found: %v", err)
	}
	if pane.Server != panelTmux || pane.Target != "t-1a2b3c4d:0.0" {
		t.Fatalf("the pane is %q on %s, expected t-1a2b3c4d:0.0 on %s", pane.Target, pane.Server, panelTmux)
	}
	if err := pane.send(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	if _, ok := pane.screen(t.Context()); !ok {
		t.Fatal("the screen of the pane was not read")
	}
	for _, cmd := range []string{"send-keys", "capture-pane"} {
		if got := tmuxCall(callsTo(t, log, panelTmux), cmd); got == nil || !slices.Contains(got, "t-1a2b3c4d:0.0") {
			t.Errorf("%s did not reach the pane on the panel's server: %v", cmd, got)
		}
		if got := tmuxCall(callsTo(t, log, userTmux), cmd); got != nil {
			t.Errorf("%s went to the user's server, where the pane is not: %v", cmd, got)
		}
	}
}

// The user's server holds the sessions the panel starts, and a pane found on
// it is the user's whatever the other server lists.
func TestTmuxPaneOnTheUsersServerIsTheUsers(t *testing.T) {
	twoServersTmux(t,
		tmuxSide{panes: []string{"900 demo:0.0"}},
		tmuxSide{panes: []string{"700 t-1a2b3c4d:0.0"}})
	procFS(t, fakeProc{pid: 900, comm: "claude", ppid: 1})

	pane, err := tmuxPaneFor(t.Context(), 900)
	if err != nil {
		t.Fatal(err)
	}
	if pane.Server != userTmux || pane.Target != "demo:0.0" {
		t.Errorf("the pane is %q on %s, expected demo:0.0 on the user's server", pane.Target, pane.Server)
	}
}

// A server that is not running holds no pane, and the other is looked at all
// the same. A server that failed to answer is named when the pane was found
// nowhere, since the pane may be on it.
func TestTmuxPaneLooksPastAServerThatIsDown(t *testing.T) {
	procFS(t,
		fakeProc{pid: 900, comm: "claude", ppid: 700},
		fakeProc{pid: 700, comm: "bash", ppid: 1},
	)
	t.Run("the user's server is down", func(t *testing.T) {
		twoServersTmux(t, tmuxSide{down: true}, tmuxSide{panes: []string{"700 t-1a2b3c4d:0.0"}})
		if pane, err := tmuxPaneFor(t.Context(), 900); err != nil || pane.Server != panelTmux {
			t.Errorf("the pane is %+v (%v): a stopped user's server hid the terminals of the panel", pane, err)
		}
	})
	t.Run("the panel's server is down", func(t *testing.T) {
		twoServersTmux(t, tmuxSide{panes: []string{"700 demo:0.0"}}, tmuxSide{down: true})
		if pane, err := tmuxPaneFor(t.Context(), 900); err != nil || pane.Server != userTmux {
			t.Errorf("the pane is %+v (%v)", pane, err)
		}
	})
	t.Run("both are down", func(t *testing.T) {
		twoServersTmux(t, tmuxSide{down: true}, tmuxSide{down: true})
		if _, err := tmuxPaneFor(t.Context(), 900); err == nil || !strings.Contains(err.Error(), "no tmux panes") {
			t.Errorf("with no server running the pane was looked for with %v", err)
		}
	})
	t.Run("one did not answer", func(t *testing.T) {
		twoServersTmux(t, tmuxSide{fails: "lost server"}, tmuxSide{panes: []string{"3300 t-0000beef:0.0"}})
		_, err := tmuxPaneFor(t.Context(), 900)
		if err == nil || !strings.Contains(err.Error(), "lost server") || !strings.Contains(err.Error(), "900") {
			t.Errorf("the error %v does not name the server that did not answer and the process", err)
		}
	})
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
	action.SessionOpen, action.SessionResume, action.SessionClose, action.SessionRestart,
	action.SessionKill, action.SessionSend, action.SessionAnswer, action.SessionDismiss, action.SessionStop,
	action.SessionBackground, action.SessionEscape, action.SessionFile, action.SessionCommand, action.SessionShell, action.SessionPermit, action.SessionSwitch, action.SessionUnqueue, action.SessionSet,
	action.SessionMcp, action.SessionRename, action.SessionRemote, action.SessionLetter,
}

var windowKinds = []action.Kind{action.WindowOpen, action.WindowClose}

var workKinds = []action.Kind{action.TaskStop, action.AgentStop}

var termKinds = []action.Kind{action.TermStart, action.TermClose, action.TermRename, action.TermConsole}

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
		gone = append(gone, termKinds...)
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
		if known := len(sessionKinds) + len(windowKinds) + len(workKinds) + len(termKinds) + 6; len(action.Kinds) != known {
			t.Errorf("action.Kinds changed: it holds %d kinds while the test knows %d", len(action.Kinds), known)
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
	if got := e.Kinds(); len(got) != len(action.Kinds)-len(sessionKinds)-len(windowKinds)-len(workKinds)-len(termKinds) {
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
	if hasKind(got, action.TermConsole) {
		t.Error("term.console is offered with an empty AACP_TERMINAL — it opens a window the same way")
	}
	for _, k := range []action.Kind{action.TermStart, action.TermClose, action.TermRename} {
		if !hasKind(got, k) {
			t.Errorf("%s disappeared because AACP_TERMINAL is empty — the terminal of the panel needs no window", k)
		}
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

// A brief of seventy answers is more text than tmux takes in one command: the
// send comes back "command too long" and not a character is typed. The text
// goes over in pieces, and a piece ends neither inside a rune nor inside an
// escape sequence — the first gives the terminal half a character, the second
// gives it the rest of the paste to read as commands.
func TestTmuxPiecesFitWhatTmuxTakes(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&body, "%02d a question about delivery · ünïcödé in the title\n   A · take it as it stands\n   note: the rule lives in two places\n", i)
	}
	text := clearLine + pasteStart + body.String() + pasteEnd + enterKey

	pieces := tmuxPieces(text)
	if len(pieces) < 2 {
		t.Fatalf("a text of %d bytes went over in %d piece — tmux refuses it whole", len(text), len(pieces))
	}
	if strings.Join(pieces, "") != text {
		t.Error("the pieces do not add up to the text that was sent")
	}
	for i, piece := range pieces {
		if len(piece) > tmuxMaxInput {
			t.Errorf("piece %d is %d bytes, over the %d tmux takes", i, len(piece), tmuxMaxInput)
		}
		if !utf8.ValidString(piece) {
			t.Errorf("piece %d ends inside a rune", i)
		}
		if esc := strings.LastIndexByte(piece, escByte); esc >= 0 && !wholeEscape(piece[esc:]) {
			t.Errorf("piece %d ends inside an escape sequence: %q", i, piece[esc:])
		}
	}
}

// The pieces reach tmux as they are: one send-keys each, in order, with the
// text behind the -- so that a reply beginning with a dash is a reply and not
// a flag.
func TestTmuxSendHandsALongTextOverInPieces(t *testing.T) {
	log := fakeTmux(t, nil, "")
	pane := tmuxPane{Target: "aacpanel:0.0"}

	text := "-" + strings.Repeat("a reply too long for one command ", 300)
	if err := pane.send(t.Context(), text); err != nil {
		t.Fatalf("the input did not go out: %v", err)
	}

	var got strings.Builder
	calls := 0
	for _, line := range tmuxArgv(t, log) {
		switch line {
		case "send-keys", "-t", "aacpanel:0.0", "-l":
			continue
		case "--":
			calls++
			continue
		}
		got.WriteString(line)
	}
	// Every call carries its own "--" flag and the stub writes one more after
	// the call, so a piece stands between two of them.
	if calls < 4 {
		t.Errorf("a text of %d bytes went out in %d markers — it was not cut at all", len(text), calls)
	}
	if got.String() != text {
		t.Errorf("tmux was handed %d bytes of the %d that were sent", got.Len(), len(text))
	}
}

// wholeEscape says whether a sequence that begins with ESC ends within s: a
// CSI runs until a byte between @ and ~, and a piece cut before that byte
// leaves the terminal waiting for the rest of it inside the text.
func wholeEscape(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return true
		}
	}
	return false
}
