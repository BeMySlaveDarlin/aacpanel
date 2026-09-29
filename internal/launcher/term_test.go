package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// recordingTmux answers every call and writes its arguments, one per line.
func recordingTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	bin := filepath.Join(dir, "tmux")
	script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do printf '%%s\\n' \"$a\" >> %q; done\n", log)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return log
}

func TestTermStartsTheShellAloneInItsPlaceOnItsSocket(t *testing.T) {
	fakeProc(t)
	log := recordingTmux(t)
	dir := t.TempDir()

	rep, err := Term(TermSpec{Dir: dir, Session: "t-1a2b3c4d", Socket: "aacp-test"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Session != "t-1a2b3c4d" || rep.Dir != dir {
		t.Errorf("the report %+v names another terminal", rep)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if !slices.Equal(argv[:2], []string{"-L", "aacp-test"}) {
		t.Errorf("tmux was started as %v — not on the socket of the panel's terminals", argv)
	}
	start := argv
	if cut := slices.Index(argv, ";"); cut >= 0 {
		start = argv[:cut]
	}
	if i := slices.Index(start, "-c"); i < 0 || i+1 >= len(start) || start[i+1] != dir {
		t.Errorf("the session was started as %v — not in its place", start)
	}
	if want := []string{"-L", "aacp-test", "new-session", "-d", "-s", "t-1a2b3c4d", "-c", dir}; !slices.Equal(start, want) {
		t.Errorf("the session was started as %v instead of %v — anything after the flags is a command "+
			"line, and the terminal runs the shell alone", start, want)
	}
	text := strings.Join(argv, " ")
	for _, want := range []string{
		"set-option -t =t-1a2b3c4d: " + TermPlaceOption + " " + dir,
		"set-option -w -t =t-1a2b3c4d: automatic-rename on",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the terminal was set up without %q: %v", want, argv)
		}
	}
}

func TestTermChecksItsSpec(t *testing.T) {
	fakeProc(t)
	log := recordingTmux(t)
	for name, spec := range map[string]TermSpec{
		"no name":                  {Dir: "/srv/proj/shop", Socket: "aacp-test"},
		"no socket of its own":     {Dir: "/srv/proj/shop", Session: "t-1a2b3c4d"},
		"a directory not absolute": {Dir: "shop", Session: "t-1a2b3c4d", Socket: "aacp-test"},
	} {
		if _, err := Term(spec); err == nil {
			t.Errorf("%s: the spec was accepted", name)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("tmux was called for a spec that was refused")
	}
}

func TestWindowAttachesOnTheSocketItNames(t *testing.T) {
	fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	log, _ := fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	t.Setenv(tmuxEnv, "/usr/bin/tmux")

	if _, err := Window(WindowSpec{Dir: t.TempDir(), Session: "t-1a2b3c4d", Socket: "aacpanel-term"}); err != nil {
		t.Fatal(err)
	}
	if args := waitFile(t, log); !strings.Contains(args, "/usr/bin/tmux -L aacpanel-term attach -t t-1a2b3c4d") {
		t.Errorf("the window attaches otherwise than to the terminal on its socket: %s", args)
	}
}

func TestLiveTermRunsTheShellWithNoCommandLine(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed — there is nothing to start a live terminal with")
	}
	fakeProc(t)
	// Every socket, the user's own server's included, lives in a directory of
	// the test's; short, since a unix socket path ends at 108 bytes.
	sockets, err := os.MkdirTemp("", "aacp-tmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", sockets)
	t.Setenv(tmuxEnv, bin)
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() {
		exec.Command(bin, "-L", "panel", "kill-server").Run()
		exec.Command(bin, "kill-server").Run()
		os.RemoveAll(sockets)
	})

	dir := t.TempDir()
	if _, err := Term(TermSpec{Dir: dir, Session: "t-1a2b3c4d", Socket: "panel"}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-L", "panel", "display-message", "-p", "-t", "=t-1a2b3c4d:",
		"#{pane_start_command}\t#{pane_current_path}\t#{"+TermPlaceOption+"}\t#{automatic-rename}\t#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("the terminal is not on its socket: %v", err)
	}
	f := strings.Split(strings.TrimRight(string(out), "\n"), "\t")
	if len(f) != 5 {
		t.Fatalf("tmux described the terminal as %q", out)
	}
	if f[0] != "" {
		t.Errorf("the terminal was started with the command line %q — only the shell runs there", f[0])
	}
	if f[1] != dir || f[2] != dir {
		t.Errorf("the terminal works in %q with the place %q, while it was started in %s", f[1], f[2], dir)
	}
	if f[3] != "1" {
		t.Errorf("the window does not name itself after what runs in it: automatic-rename %q", f[3])
	}
	pid, _ := strconv.Atoi(f[4])
	if comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err != nil || strings.TrimSpace(string(comm)) != "sh" {
		t.Errorf("the pane runs %q, not the shell of the user (%v)", comm, err)
	}
}
