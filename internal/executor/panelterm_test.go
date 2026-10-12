package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

const shopTerm = "t-1a2b3c4d\t1790690000\t1790700000\t1\t/srv/proj/shop\t\tmake\tmake"

// panelCalls are the calls the stub saw on the panel's server, with -L and
// the socket taken off; a call to any other server fails the test.
func panelCalls(t *testing.T, log string) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range tmuxCalls(t, log) {
		if len(c) < 2 || c[0] != "-L" || c[1] != string(panelTmux) {
			t.Errorf("a call went to a server other than the panel's: %v", c)
			continue
		}
		out = append(out, c[2:])
	}
	return out
}

func failingTmux(t *testing.T, said string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tmux")
	script := fmt.Sprintf("#!/bin/sh\necho %q >&2\nexit 1\n", said)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
}

func TestPanelTermsAreListedFromTheirOwnSocket(t *testing.T) {
	termSocket(t, string(panelTmux))
	stub := newTmuxStub(t, nil, "")
	stub.reply("list-sessions", strings.Join([]string{
		shopTerm,
		"t-0000beef\t1790680000\t1790680500\t0\t/home/u\tlogs\tbash\ttail",
		"scratch\t1790680000\t1790680500\t0\t/home/u\t\tbash\tbash",
		"t-00c0ffee\t1790680000\t1790680500\t0\t\t\tbash\tbash",
		"t-1A2B3C4D\t1790680000\t1790680500\t0\t/home/u\t\tbash\tbash",
	}, "\n")+"\n")

	list, err := panelTerms(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []action.Term{
		{ID: "t-1a2b3c4d", Place: "/srv/proj/shop", Name: "make", Command: "make", Busy: true,
			Activity: 1790700000, Created: 1790690000, Clients: 1},
		{ID: "t-0000beef", Place: "/home/u", Name: "logs", Command: "tail", Busy: true,
			Activity: 1790680500, Created: 1790680000},
	}
	if !reflect.DeepEqual(list, want) {
		t.Errorf("the terminals are listed as\n%+v\ninstead of\n%+v", list, want)
	}
	calls := panelCalls(t, stub.log)
	if len(calls) == 0 || calls[0][0] != "list-sessions" {
		t.Fatalf("the list was asked with %v", calls)
	}
	for _, c := range calls[1:] {
		if c[0] != "capture-pane" {
			t.Errorf("after the list tmux was asked %v — one list-sessions for all, a screen for each", c)
		}
	}
}

// screensTmux plays the panel's tmux with a screen of its own for every
// terminal: it lists the sessions it is given, shows a terminal named in
// screens its screen, never answers for one named in hang, and refuses the
// screen of any other.
func screensTmux(t *testing.T, sessions []string, screens map[string]string, hang ...string) string {
	t.Helper()
	termSocket(t, string(panelTmux))
	for i := range sessions {
		fields := strings.SplitN(sessions[i], "\t", panelTermFields)
		sessions[i] = strings.Join(fields[:7], "\t") + fmt.Sprintf("\t$%d\t/srv/proj/shop\t\t1\t", i) + fields[7]
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sessions", strings.Join(sessions, "\n")+"\n")
	for id, screen := range screens {
		write("screen-"+id, screen)
	}
	for _, id := range hang {
		write("hang-"+id, "")
	}
	// The hang execs its sleep: a sleep under the shell would keep the pipe
	// open after the shell is killed, and the call would wait for it.
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a" >> %[1]q; done
printf -- '--\n' >> %[1]q
d=%[2]q
if [ "$1" = -L ]; then shift 2; fi
cmd=$1
t=
while [ $# -gt 0 ]; do
	if [ "$1" = -t ]; then t=$(printf '%%s' "$2" | tr -d '=:'); fi
	shift
done
case $cmd in
list-sessions) cat "$d/sessions" ;;
capture-pane)
	if [ -f "$d/hang-$t" ]; then exec sleep 5; fi
	if [ -f "$d/screen-$t" ]; then cat "$d/screen-$t"; exit 0; fi
	echo "can't find pane: $t" >&2
	exit 1 ;;
esac
`, log, dir)
	bin := filepath.Join(dir, "tmux")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return log
}

func termRow(id, command string) string {
	return id + "\t1790690000\t1790700000\t0\t/srv/proj/shop\t\t" + command + "\t" + command
}

func TestTermsTellWhetherSomethingRunsBesideTheShell(t *testing.T) {
	busy := map[string]bool{
		"bash": false, "zsh": false, "sh": false, "fish": false,
		"make": true, "htop": true, "sleep": true, "npm": true, "python3": true,
	}
	var rows []string
	of := map[string]string{}
	for command := range busy {
		id := fmt.Sprintf("t-%08x", len(rows)+1)
		of[id] = command
		rows = append(rows, termRow(id, command))
	}
	screensTmux(t, rows, nil)

	list, err := (&Executor{}).Terms(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(busy) {
		t.Fatalf("%d terminals listed of %d", len(list), len(busy))
	}
	for _, term := range list {
		if want := busy[of[term.ID]]; term.Busy != want {
			t.Errorf("with %q in the foreground the terminal is busy %v, expected %v", of[term.ID], term.Busy, want)
		}
	}
}

func TestTermsCarryTheLastLineOnTheirScreen(t *testing.T) {
	long := strings.Repeat("é", termLastRunes+40)
	var rows []string
	for i := 1; i <= 6; i++ {
		rows = append(rows, termRow(fmt.Sprintf("t-%08x", i), "bash"))
	}
	log := screensTmux(t, rows, map[string]string{
		"t-00000001": "u@h shop $ make check\nok   shop/cart    0.21s\n  ok   shop/store  15.90s  \n   \n\n\n",
		"t-00000002": "u@h ~ $ cat big\n" + long + "\n\n",
		"t-00000003": "\n\n   \n\n",
	}, "t-00000005")
	// A screen has a bound of its own, far under the one of any tmux call: the
	// one that never answers is given up on long before its sleep ends. The
	// bound stays roomy for a screen that does answer: a runner busy with other
	// tests is slow to start the fake tmux, and a screen cut short by a tight
	// bound reads as one that never came.
	prevCapture, prevTmux := termCaptureTimeout, tmuxTimeout
	termCaptureTimeout, tmuxTimeout = 500*time.Millisecond, time.Minute
	t.Cleanup(func() { termCaptureTimeout, tmuxTimeout = prevCapture, prevTmux })

	began := time.Now()
	list, err := (&Executor{}).Terms(t.Context())
	if err != nil {
		t.Fatalf("a screen that did not come dropped the list: %v", err)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Errorf("the list took %v — a screen that does not answer held it up", took)
	}
	last := map[string]string{}
	for _, term := range list {
		last[term.ID] = term.Last
	}
	want := map[string]string{
		"t-00000001": "ok   shop/store  15.90s",
		"t-00000002": string([]rune(long)[:termLastRunes]) + "…",
		"t-00000003": "",
		"t-00000004": "",
		"t-00000005": "",
		"t-00000006": "",
	}
	if !reflect.DeepEqual(last, want) {
		t.Errorf("the last lines are\n%q\ninstead of\n%q", last, want)
	}

	var captures [][]string
	for _, c := range panelCalls(t, log) {
		if c[0] == "capture-pane" {
			captures = append(captures, c)
		}
	}
	if len(captures) != 6 {
		t.Fatalf("%d screens were read for six terminals: %v", len(captures), captures)
	}
	if want := []string{"capture-pane", "-p", "-t", "=t-00000001:"}; !slices.Equal(captures[0], want) {
		t.Errorf("the screen was read with %v instead of %v — plain text of the exact session", captures[0], want)
	}
}

func TestPanelTermsWithoutAServerAreNone(t *testing.T) {
	for _, said := range []string{
		"no server running on /tmp/tmux-1000/aacpanel-term",
		"error connecting to /tmp/tmux-1000/aacpanel-term (No such file or directory)",
		"server exited unexpectedly",
	} {
		failingTmux(t, said)
		list, err := panelTerms(t.Context())
		if err != nil || len(list) != 0 {
			t.Errorf("%q: %v, %v — a server nobody started yet, or one that went as it was asked, is no terminals, not a failure", said, list, err)
		}
	}

	failingTmux(t, "protocol version mismatch")
	if _, err := panelTerms(t.Context()); err == nil {
		t.Error("a tmux that failed otherwise was taken for one with no terminals")
	}
}

// launcherTask is the task the fake launcher was handed on stdin.
func launcherTask(t *testing.T, log string, into any) bool {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "{") {
			if err := json.Unmarshal([]byte(line), into); err != nil {
				t.Fatalf("the task %q did not parse: %v", line, err)
			}
			return true
		}
	}
	return false
}

func TestTermStartOpensOnlyInAPlaceInsideTheRoots(t *testing.T) {
	root, home, outside := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(projectRootsEnv, root)
	t.Setenv("HOME", home)
	shop := filepath.Join(root, "shop")
	if err := os.Mkdir(shop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "away")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	refused := map[string]string{
		"outside the roots":           outside,
		"a link leading outside":      filepath.Join(root, "away"),
		"a file, not a directory":     filepath.Join(root, "notes.txt"),
		"a directory that is not":     filepath.Join(root, "missing"),
		"a relative path":             "shop",
		"a climb out of the root":     root + "/../" + filepath.Base(outside),
		"the parent of the home only": filepath.Dir(home),
	}
	for name, place := range refused {
		t.Run(name, func(t *testing.T) {
			log := fakeLauncher(t, launcher.Report{Session: "t-1a2b3c4d"})
			e, _ := newTest(t, "")
			_, err := e.Execute(t.Context(), action.Request{ID: "x1", Kind: action.TermStart, Target: "t-1a2b3c4d", Place: place})
			if err == nil {
				t.Fatalf("a terminal was started in %s", place)
			}
			if _, statErr := os.Stat(log); statErr == nil {
				t.Errorf("the launcher was called for %s before the refusal", place)
			}
		})
	}

	for name, place := range map[string]string{"a project": shop, "the home directory": home} {
		t.Run(name, func(t *testing.T) {
			log := fakeLauncher(t, launcher.Report{Session: "t-1a2b3c4d"})
			e, _ := newTest(t, "")
			detail, err := e.Execute(t.Context(), action.Request{ID: "x1", Kind: action.TermStart, Target: "t-1a2b3c4d", Place: place})
			if err != nil {
				t.Fatalf("no terminal in %s: %v", place, err)
			}
			if !strings.Contains(detail, "t-1a2b3c4d") || !strings.Contains(detail, place) {
				t.Errorf("the journal line %q names neither the terminal nor its place", detail)
			}
			var spec launcher.TermSpec
			if !launcherTask(t, log, &spec) {
				t.Fatal("the launcher was not handed the task")
			}
			want := launcher.TermSpec{Dir: place, Session: "t-1a2b3c4d", Socket: string(panelTmux)}
			if spec != want {
				t.Errorf("the launcher was handed %+v instead of %+v", spec, want)
			}
		})
	}
}

func TestTermActionsRefuseAnIDOffThePanel(t *testing.T) {
	for _, id := range []string{"t-00000000", "t-1A2B3C4D", "t-1a2b3c4", "shop", "=t-1a2b3c4d"} {
		for _, kind := range []action.Kind{action.TermClose, action.TermRename, action.TermConsole, action.TermStart} {
			if kind == action.TermStart && action.TermID(id) {
				continue
			}
			t.Run(string(kind)+" "+id, func(t *testing.T) {
				stub := newTmuxStub(t, nil, "")
				stub.reply("list-sessions", shopTerm+"\n")
				log := fakeLauncher(t, launcher.Report{Session: id})
				place := t.TempDir()
				t.Setenv(projectRootsEnv, place)
				e, _ := newTest(t, "")

				req := action.Request{ID: "x1", Kind: kind, Target: id, Rename: "build"}
				if kind == action.TermStart {
					req.Rename, req.Place = "", place
				}
				if _, err := e.Execute(t.Context(), req); err == nil {
					t.Fatalf("%s went through for %q", kind, id)
				}
				for _, c := range tmuxCalls(t, stub.log) {
					if !slices.Contains(c, "list-sessions") {
						t.Errorf("tmux was told to act for %q: %v", id, c)
					}
				}
				if _, err := os.Stat(log); err == nil {
					t.Errorf("the launcher was called for %q", id)
				}
			})
		}
	}
}

// The collector knows a claude typed into a terminal of the panel by the
// socket its server was started on. A name out of step with the executor's
// marks every such claude as out of the panel's reach on the screens, while
// the executor would reach it.
func TestThePanelTerminalSocketIsTheCollectors(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "agent", "ctx.py"))
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, s := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(s, "PANEL_TMUX = ") {
			line = s
			break
		}
	}
	if line == "" {
		t.Fatal("no PANEL_TMUX found in agent/ctx.py — the socket of the terminals was renamed there")
	}
	if want := `PANEL_TMUX = "` + panelTermSocket + `"`; line != want {
		t.Errorf("the collector has %s, the executor %q", line, panelTermSocket)
	}
}

func TestTermCloseKillsTheSessionOnThePanelSocket(t *testing.T) {
	stub := newTmuxStub(t, nil, "")
	stub.reply("list-sessions", shopTerm+"\n")
	e, _ := newTest(t, "")

	if _, err := e.Execute(t.Context(), req(action.TermClose, "t-1a2b3c4d")); err != nil {
		t.Fatal(err)
	}
	kill := tmuxCall(panelCalls(t, stub.log), "kill-session")
	if !slices.Equal(kill, []string{"kill-session", "-t", "=t-1a2b3c4d"}) {
		t.Errorf("the terminal was closed with %v — not the exact session on the panel's server", kill)
	}
}

func TestTermRenameNamesItAndStopsItsWindowNamingItself(t *testing.T) {
	stub := newTmuxStub(t, nil, "")
	stub.reply("list-sessions", shopTerm+"\n")
	e, _ := newTest(t, "")

	r := req(action.TermRename, "t-1a2b3c4d")
	r.Rename = "-a #{b} c"
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"set-option", "-t", "=t-1a2b3c4d:", termNameOption, "-a #{b} c",
		";", "set-option", "-w", "-t", "=t-1a2b3c4d:", "automatic-rename", "off",
	}
	if got := tmuxCall(panelCalls(t, stub.log), "set-option"); !slices.Equal(got, want) {
		t.Errorf("the rename went as %v instead of %v", got, want)
	}
}

func TestTermConsoleOpensAWindowOnThePanelSocket(t *testing.T) {
	stub := newTmuxStub(t, nil, "")
	stub.reply("list-sessions", shopTerm+"\n")
	stub.reply("list-clients", "/dev/pts/9 99999\n")
	log := fakeLauncher(t, launcher.Report{Session: "t-1a2b3c4d", Konsole: 1})
	e, _ := newTest(t, "")

	detail, err := e.Execute(t.Context(), req(action.TermConsole, "t-1a2b3c4d"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail, "WARNING") {
		t.Errorf("the window was not seen attaching: %s", detail)
	}
	var spec launcher.WindowSpec
	if !launcherTask(t, log, &spec) {
		t.Fatal("the launcher was not handed the window")
	}
	want := launcher.WindowSpec{Dir: "/srv/proj/shop", Session: "t-1a2b3c4d", Socket: string(panelTmux)}
	if spec != want {
		t.Errorf("the window was asked for as %+v instead of %+v", spec, want)
	}
	if clients := tmuxCall(panelCalls(t, stub.log), "list-clients"); clients == nil {
		t.Error("the window was waited for on a server other than the panel's")
	}
}

// ownPanelTmux gives a test a tmux of its own for the terminals of the panel:
// every socket, the user's own server's included, lives in a directory of the
// test's, so nothing it starts can reach a server of the machine, and the
// directory goes with the test.
func ownPanelTmux(t *testing.T) {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed — there is nothing to check a live terminal with")
	}
	// Short, and not t.TempDir(): a unix socket path ends at 108 bytes.
	dir, err := os.MkdirTemp("", "aacp-tmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv(tmuxEnv, bin)
	t.Setenv("SHELL", "/bin/sh")
	prev := panelTmux
	panelTmux = "panel"
	t.Cleanup(func() {
		exec.Command(bin, "-L", string(panelTmux), "kill-server").Run()
		exec.Command(bin, "kill-server").Run()
		panelTmux = prev
		os.RemoveAll(dir)
	})
}

func TestLivePanelTerminalFromStartToClose(t *testing.T) {
	ownPanelTmux(t)
	place := t.TempDir()
	const id = "t-0a1b2c3d"

	if _, err := launcher.Term(launcher.TermSpec{Dir: place, Session: id, Socket: string(panelTmux)}); err != nil {
		t.Fatalf("the terminal did not start: %v", err)
	}
	list, err := panelTerms(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("the terminals are %+v (%v) — one was started", list, err)
	}
	if got := list[0]; got.ID != id || got.Place != place || got.Name == "" || got.Created == 0 {
		t.Errorf("the terminal is listed as %+v", got)
	}

	term, err := NewTermOpener().OpenTerm(t.Context(), id, 60, 20)
	if err != nil {
		t.Fatalf("the bridge did not open: %v", err)
	}
	screen := make(chan string, 1)
	go func() {
		var seen strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			seen.Write(buf[:n])
			if strings.Contains(seen.String(), "hi-42") || err != nil {
				screen <- seen.String()
				return
			}
		}
	}()
	if _, err := term.Write([]byte("echo hi-$((6*7))\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-screen:
		if !strings.Contains(got, "hi-42") {
			t.Fatalf("the shell did not answer through the bridge: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the shell did not answer through the bridge in five seconds")
	}
	if list, _ := panelTerms(t.Context()); len(list) != 1 || list[0].Clients != 1 {
		t.Errorf("with the bridge attached the terminal is listed as %+v", list)
	}

	e := &Executor{}
	if _, err := e.termRename(t.Context(), id, "build"); err != nil {
		t.Fatal(err)
	}
	if list, _ := panelTerms(t.Context()); len(list) != 1 || list[0].Name != "build" {
		t.Errorf("after the rename the terminal is listed as %+v", list)
	}
	if rule, _ := panelTmux.run(t.Context(), "show-options", "-w", "-v", "-t", "="+id+":", "automatic-rename"); strings.TrimSpace(rule) != "off" {
		t.Errorf("the window goes on naming itself after the rename: %q", rule)
	}

	term.Close()
	if _, err := e.termClose(t.Context(), "t-00000000"); err == nil {
		t.Error("a terminal that is not there was closed")
	}
	if _, err := e.termClose(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if list, err := panelTerms(t.Context()); err != nil || len(list) != 0 {
		t.Errorf("after the close the terminals are %+v (%v)", list, err)
	}
}

func TestLivePanelTerminalTellsWhatRunsAndItsLastLine(t *testing.T) {
	ownPanelTmux(t)
	const id = "t-0a1b2c3d"
	if _, err := launcher.Term(launcher.TermSpec{Dir: t.TempDir(), Session: id, Socket: string(panelTmux)}); err != nil {
		t.Fatalf("the terminal did not start: %v", err)
	}
	e := &Executor{}
	// seen waits for the terminal to be listed as ok takes it.
	seen := func(ok func(action.Term) bool) action.Term {
		t.Helper()
		var last action.Term
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			list, err := e.Terms(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(list) == 1 {
				last = list[0]
				if ok(last) {
					return last
				}
			}
		}
		t.Fatalf("the terminal stayed listed as %+v", last)
		return last
	}

	// The command in the foreground and the screen are read apart, and the
	// shell's own start can run a command of its profile, such as locale, as
	// its prompt shows, so the shell is waited for until it is listed idle: one
	// that stays busy at its prompt fails here.
	seen(func(term action.Term) bool { return term.Last != "" && !term.Busy })
	if _, err := panelTmux.run(t.Context(), "send-keys", "-t", "="+id+":",
		"printf 'one\\ntwo\\n'; sleep 30", "Enter"); err != nil {
		t.Fatal(err)
	}
	// The command in the foreground and the screen are read apart, so the
	// terminal is waited for until both have caught up.
	running := seen(func(term action.Term) bool { return term.Busy && term.Last == "two" })
	if running.Command != "sleep" {
		t.Errorf("the busy terminal runs %q, expected sleep", running.Command)
	}
}

func TestLivePanelTerminalLeavesTheUsersServerAlone(t *testing.T) {
	ownPanelTmux(t)
	if _, err := launcher.Term(launcher.TermSpec{Dir: t.TempDir(), Session: "t-0a1b2c3d", Socket: string(panelTmux)}); err != nil {
		t.Fatalf("the terminal did not start: %v", err)
	}
	if out, err := userTmux.run(context.Background(), "list-sessions"); err == nil {
		t.Errorf("the terminal of the panel landed on the user's own server: %s", out)
	}
}

// What a person types into a live terminal of the panel is found in the
// terminal's pane on the panel's server, through its shell, while the user's
// own server is not running at all.
func TestLivePanelTerminalHoldsWhatIsTypedIntoIt(t *testing.T) {
	ownPanelTmux(t)
	const id = "t-0a1b2c3d"
	if _, err := launcher.Term(launcher.TermSpec{Dir: t.TempDir(), Session: id, Socket: string(panelTmux)}); err != nil {
		t.Fatalf("the terminal did not start: %v", err)
	}
	out, err := panelTmux.run(t.Context(), "list-panes", "-t", "="+id+":", "-F", "#{pane_pid}")
	if err != nil {
		t.Fatal(err)
	}
	shell, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("the shell of the terminal is %q", out)
	}
	if _, err := panelTmux.run(t.Context(), "send-keys", "-t", "="+id+":", "sleep 30", "Enter"); err != nil {
		t.Fatal(err)
	}
	typed := 0
	for end := time.Now().Add(5 * time.Second); typed == 0 && time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		dirs, _ := os.ReadDir("/proc")
		for _, d := range dirs {
			pid, err := strconv.Atoi(d.Name())
			if err != nil {
				continue
			}
			if parent, ok := procParent(pid); ok && parent == shell {
				typed = pid
				break
			}
		}
	}
	if typed == 0 {
		t.Fatal("the shell of the terminal started nothing in five seconds")
	}

	pane, err := tmuxPaneFor(t.Context(), typed)
	if err != nil {
		t.Fatalf("the pane of what was typed into the terminal was not found: %v", err)
	}
	if pane.Server != panelTmux || tmuxSessionOf(pane.Target) != id {
		t.Errorf("what was typed into terminal %s is in %q on %s", id, pane.Target, pane.Server)
	}
}

// termSocket gives the fake command an owned socket to discover without ever
// looking in the user's real tmux directory. Its path must fit a Unix socket.
func termSocket(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv(sessionsEnv, t.TempDir())
	sockets := filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.Mkdir(sockets, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sockets, name)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return path
}

func TestTmuxTermsDiscoverOwnedServersAndResolveOnlyLiveIDs(t *testing.T) {
	ownPanelTmux(t)
	sessionFiles(t)
	e := &Executor{}
	start := func(server tmuxServer, name string) {
		t.Helper()
		if _, err := server.run(t.Context(), "-f", "/dev/null", "new-session", "-d", "-s", name, "/bin/sh"); err != nil {
			t.Fatal(err)
		}
	}
	start("default", "same")
	start("x", "same")
	t.Cleanup(func() { tmuxServer("x").run(context.Background(), "kill-server") })
	if _, err := launcher.Term(launcher.TermSpec{Dir: t.TempDir(), Session: "t-0a1b2c3d", Socket: string(panelTmux)}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(os.Getenv("TMUX_TMPDIR"), fmt.Sprintf("tmux-%d", os.Getuid()))
	dead := filepath.Join(dir, "dead")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: dead, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	listener.Close()
	if err := os.Symlink(filepath.Join(dir, "x"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	list, err := e.Terms(t.Context())
	if err != nil || len(list) != 3 {
		t.Fatalf("terminals: %+v (%v)", list, err)
	}
	byServer := map[string]action.Term{}
	for _, term := range list {
		byServer[term.Server] = term
	}
	for _, name := range []string{"default", "x"} {
		term := byServer[name]
		if !term.Foreign || term.Last != "" || term.Name != "same" || !action.TmuxID(term.ID) {
			t.Errorf("%s: %+v", name, term)
		}
		found, err := findTmuxTerm(t.Context(), term.ID)
		if err != nil || found.Server != name {
			t.Fatalf("id did not resolve: %+v %v", found, err)
		}
	}
	if byServer["default"].ID == byServer["x"].ID {
		t.Fatal("equal session names on different servers share an id")
	}
	if byServer[""].ID != "t-0a1b2c3d" || byServer[""].Foreign {
		t.Fatal("the panel terminal lost its identity")
	}
	for _, id := range []string{"tm-" + strings.Repeat("0", 64), "../x", "same", "tm-not-hex"} {
		if _, err := e.tmuxClose(t.Context(), id); err == nil {
			t.Errorf("forged id %q closed a terminal", id)
		}
		if term, err := NewTermOpener().OpenTerm(t.Context(), id, 60, 20); err == nil {
			term.Close()
			t.Errorf("forged id %q attached", id)
		}
	}
	// A bridge types into the foreign server selected by the enumerated id.
	term, err := NewTermOpener().OpenTerm(t.Context(), byServer["x"].ID, 60, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	seen := make(chan bool, 1)
	go func() {
		var out strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			out.Write(buf[:n])
			if strings.Contains(out.String(), "foreign-42") {
				seen <- true
				return
			}
			if err != nil {
				seen <- false
				return
			}
		}
	}()
	if _, err := term.Write([]byte("echo foreign-$((6*7))\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-seen:
		if !ok {
			t.Error("the foreign terminal did not answer")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the foreign bridge did not type")
	}
	term.Close()
	if _, err := e.Execute(t.Context(), req(action.TmuxClose, byServer["x"].ID)); err != nil {
		t.Fatal(err)
	}
	start("x", "same")
	if _, err := findTmuxTerm(t.Context(), byServer["x"].ID); err == nil {
		t.Error("a vanished id resolved to a replacement server")
	}
	if _, err := tmuxServer("default").run(t.Context(), "has-session", "-t", "=same"); err != nil {
		t.Fatal("closing x also closed default")
	}
}

func TestTmuxTermsFindClaudeInInactivePanesAndRefuseBareClose(t *testing.T) {
	ownPanelTmux(t)
	for _, server := range []tmuxServer{"default", panelTmux} {
		name := "arbitrary-name"
		if server == panelTmux {
			name = "t-0a1b2c3d"
		}
		if _, err := server.run(t.Context(), "-f", "/dev/null", "new-session", "-d", "-s", name, "/bin/sh"); err != nil {
			t.Fatal(err)
		}
		pidText, err := server.run(t.Context(), "display-message", "-p", "-t", "="+name+":", "#{pane_pid}")
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(pidText))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.run(t.Context(), "new-window", "-t", "="+name, "/bin/sh"); err != nil {
			t.Fatal(err)
		}
		if server == panelTmux {
			if _, err := server.run(t.Context(), "set-option", "-t", "="+name+":", launcher.TermPlaceOption, "/srv/proj/shop"); err != nil {
				t.Fatal(err)
			}
		}
		procFS(t, fakeProc{pid: 800001, ppid: pid, comm: "claude", start: "1000"})
		sessionFiles(t, fakeSession{pid: 800001, name: "known-conversation", start: "1000", sid: "conversation-id"})
		list, err := (&Executor{}).Terms(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var found action.Term
		for _, term := range list {
			if term.Session == "known-conversation" {
				found = term
			}
		}
		if found.ID == "" || found.SessionID != "conversation-id" {
			t.Fatalf("Claude not recognized by its pane: %+v", list)
		}
		kind := action.TmuxClose
		if !found.Foreign {
			kind = action.TermClose
		}
		if _, err := (&Executor{}).Execute(t.Context(), req(kind, found.ID)); err == nil || !strings.Contains(err.Error(), "session.close") {
			t.Fatalf("Claude accepted bare close: %v", err)
		}
		if _, err := server.run(t.Context(), "has-session", "-t", "="+name); err != nil {
			t.Fatal("Claude's tmux session was killed")
		}
	}
}

func TestTmuxListingBoundsAServerAndRejectsForeignOwnership(t *testing.T) {
	path := termSocket(t, "hang")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid++
	if ownedTmuxEntry(tmuxOtherOwner{info, &stat}) {
		t.Fatal("a socket owned by another user was accepted")
	}
	bin := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	quickWaits(t, &termListTimeout)
	began := time.Now()
	if list, err := (&Executor{}).Terms(t.Context()); err != nil || len(list) != 0 {
		t.Fatalf("hung server: %+v %v", list, err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("a hung server held the listing")
	}
}

type tmuxOtherOwner struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (s tmuxOtherOwner) Sys() any { return s.stat }
