package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	stub := newTmuxStub(t, nil, "")
	stub.reply("list-sessions", strings.Join([]string{
		shopTerm,
		"t-0000beef\t1790680000\t1790680500\t0\t/home/u\tlogs\tbash\ttail",
		"scratch\t1790680000\t1790680500\t0\t/home/u\t\tbash\tbash",
		"t-00c0ffee\t1790680000\t1790680500\t0\t\t\tbash\tbash",
		"t-1A2B3C4D\t1790680000\t1790680500\t0\t/home/u\t\tbash\tbash",
	}, "\n")+"\n")

	list, err := (&Executor{}).Terms(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []action.Term{
		{ID: "t-1a2b3c4d", Place: "/srv/proj/shop", Name: "make", Command: "make",
			Activity: 1790700000, Created: 1790690000, Clients: 1},
		{ID: "t-0000beef", Place: "/home/u", Name: "logs", Command: "tail",
			Activity: 1790680500, Created: 1790680000},
	}
	if !reflect.DeepEqual(list, want) {
		t.Errorf("the terminals are listed as\n%+v\ninstead of\n%+v", list, want)
	}
	calls := panelCalls(t, stub.log)
	if len(calls) != 1 || calls[0][0] != "list-sessions" {
		t.Errorf("the list was asked with %v", calls)
	}
}

func TestPanelTermsWithoutAServerAreNone(t *testing.T) {
	for _, said := range []string{
		"no server running on /tmp/tmux-1000/aacpanel-term",
		"error connecting to /tmp/tmux-1000/aacpanel-term (No such file or directory)",
	} {
		failingTmux(t, said)
		list, err := panelTerms(t.Context())
		if err != nil || len(list) != 0 {
			t.Errorf("%q: %v, %v — a server nobody started yet is no terminals, not a failure", said, list, err)
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

func TestLivePanelTerminalLeavesTheUsersServerAlone(t *testing.T) {
	ownPanelTmux(t)
	if _, err := launcher.Term(launcher.TermSpec{Dir: t.TempDir(), Session: "t-0a1b2c3d", Socket: string(panelTmux)}); err != nil {
		t.Fatalf("the terminal did not start: %v", err)
	}
	if out, err := userTmux.run(context.Background(), "list-sessions"); err == nil {
		t.Errorf("the terminal of the panel landed on the user's own server: %s", out)
	}
}
