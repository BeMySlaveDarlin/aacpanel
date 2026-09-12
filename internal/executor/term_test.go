package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"aacpanel/internal/termlink"
)

func TestPTYEndsWhenProcessExits(t *testing.T) {
	p, err := openPTY(80, 24)
	if err != nil {
		t.Fatalf("the pair did not open: %v", err)
	}
	defer p.Close()

	cmd := exec.Command("/bin/sh", "-c", "printf 'hello'")
	p.attach(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("the process did not start: %v", err)
	}
	p.closeSlave()

	done := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(p.master)
		done <- out
	}()

	select {
	case out := <-done:
		if !strings.Contains(string(out), "hello") {
			t.Errorf("%q came off the screen", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading did not end after the process died — our own side of the slave stayed open")
	}
	cmd.Wait()
}

func TestPTYSizeReachesProcess(t *testing.T) {
	if _, err := exec.LookPath("stty"); err != nil {
		t.Skip("no stty — there is nothing to check the window size with")
	}

	p, err := openPTY(133, 41)
	if err != nil {
		t.Fatalf("the pair did not open: %v", err)
	}
	defer p.Close()

	cmd := exec.Command("stty", "size")
	p.attach(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("the process did not start: %v", err)
	}
	p.closeSlave()

	out, _ := io.ReadAll(p.master)
	cmd.Wait()
	if got := strings.TrimSpace(string(out)); got != "41 133" {
		t.Errorf("the process sees a window of %q, while 133x41 was set", got)
	}
}

func TestPTYResizeTellsKernel(t *testing.T) {
	p, err := openPTY(80, 24)
	if err != nil {
		t.Fatalf("the pair did not open: %v", err)
	}
	defer p.Close()

	if err := p.resize(120, 40); err != nil {
		t.Fatalf("the resize did not go through: %v", err)
	}
	ws, err := unix.IoctlGetWinsize(int(p.master.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		t.Fatalf("the size cannot be read: %v", err)
	}
	if ws.Col != 120 || ws.Row != 40 {
		t.Errorf("the kernel holds %dx%d instead of 120x40", ws.Col, ws.Row)
	}

	if err := p.resize(0, 40); err == nil {
		t.Error("a zero width was accepted")
	}
}

func TestTerminalAttachesToPaneNotToName(t *testing.T) {
	log := fakeTmux(t, []string{"4242 real:1.2"}, "")
	procFS(t, fakeProc{pid: 4242, comm: "claude", args: []string{"claude", "-n", "aacpanel"}, start: "77"})
	sessionFiles(t, fakeSession{pid: 4242, name: "aacpanel", start: "77"})

	term, err := NewTermOpener().Open(context.Background(), "aacpanel", 100, 30)
	if err != nil {
		t.Fatalf("the terminal did not open: %v", err)
	}
	defer term.Close()

	io.ReadAll(term)

	argv := tmuxArgv(t, log)
	if !slices.Contains(argv, "attach-session") {
		t.Fatalf("attach-session was not called: %v", argv)
	}
	if !slices.Contains(argv, "real:1.2") {
		t.Errorf("attached to a pane other than the one tmux named: %v", argv)
	}
	if slices.Contains(argv, "aacpanel") {
		t.Errorf("the name from the request went into the tmux command line: %v", argv)
	}
	if !slices.Contains(argv, "aggressive-resize") {
		t.Errorf("aggressive resize was not turned on: %v", argv)
	}
	if !slices.Contains(argv, windowSizeSmallest) {
		t.Errorf("the size rule was not set on the window: %v", argv)
	}

	if got := term.Detail(); !strings.Contains(got, "aacpanel") || !strings.Contains(got, "real:1.2") {
		t.Errorf("the caption %q names neither the session nor the pane", got)
	}
	if term.Kind() != "tmux" {
		t.Errorf("the terminal called itself %q", term.Kind())
	}
}

func TestTerminalRefusesSessionOutsideTmux(t *testing.T) {
	fakeTmux(t, nil, "")
	procFS(t, fakeProc{pid: 4242, comm: "claude", args: []string{"claude", "-n", "aacpanel"}, start: "77"})
	sessionFiles(t, fakeSession{pid: 4242, name: "aacpanel", start: "77"})

	_, err := NewTermOpener().Open(context.Background(), "aacpanel", 100, 30)
	if err == nil {
		t.Fatal("attached to a session that is not in tmux")
	}
	if !strings.Contains(err.Error(), "aacpanel") {
		t.Errorf("the refusal does not name the session: %v", err)
	}
}

func TestTerminalRefusesUnknownSession(t *testing.T) {
	fakeTmux(t, []string{"4242 real:1.2"}, "")
	procFS(t, fakeProc{pid: 4242, comm: "claude", args: []string{"claude", "-n", "aacpanel"}, start: "77"})
	sessionFiles(t, fakeSession{pid: 4242, name: "aacpanel", start: "77"})

	_, err := NewTermOpener().Open(context.Background(), "shop", 100, 30)
	if err == nil {
		t.Fatal("a terminal opened to a session that does not exist")
	}
	if !strings.Contains(err.Error(), "aacpanel") {
		t.Errorf("the refusal does not show which sessions there are: %v", err)
	}
}

func TestTermSocketLivesNextToActions(t *testing.T) {
	if got := TermSocket("/run/user/1000/aacpanel-exec/sock"); got != "/run/user/1000/aacpanel-exec/term.sock" {
		t.Errorf("the terminal socket moved to %q", got)
	}
	if got := TermSocket(""); got != "" {
		t.Errorf("%q was built out of an empty path", got)
	}
}

func TestTermEnvCarriesTermAndLocale(t *testing.T) {
	env := termEnv()
	var term, lang string
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		switch name {
		case "TERM":
			term = value
		case "LANG":
			lang = value
		}
	}
	if term != "xterm-256color" {
		t.Errorf("TERM=%q — a TUI draws itself any old way with it", term)
	}
	if !strings.Contains(lang, "UTF-8") {
		t.Errorf("LANG=%q — non-ASCII text turns into question marks", lang)
	}
	if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "TMUX=") }) {
		t.Error("TMUX went to the client: it will decide it is nested in another session and refuse to attach")
	}
	_ = os.Getenv("PATH")
}

func tmuxCalls(t *testing.T, log string) [][]string {
	t.Helper()
	var out [][]string
	var cur []string
	for _, line := range tmuxArgv(t, log) {
		if line == "--" {
			out = append(out, cur)
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return out
}

func tmuxCall(calls [][]string, name string) []string {
	for _, c := range calls {
		if len(c) > 0 && c[0] == name {
			return c
		}
	}
	return nil
}

func openBridge(t *testing.T, stub *tmuxStub) termlink.Terminal {
	t.Helper()
	procFS(t, fakeProc{pid: 4242, comm: "claude", args: []string{"claude", "-n", "aacpanel"}, start: "77"})
	sessionFiles(t, fakeSession{pid: 4242, name: "aacpanel", start: "77"})

	term, err := NewTermOpener().Open(context.Background(), "aacpanel", 40, 32)
	if err != nil {
		t.Fatalf("the terminal did not open: %v", err)
	}
	io.ReadAll(term)
	return term
}

func TestBridgeDoesNotPinTheWindowBeforeAttaching(t *testing.T) {
	for _, tc := range []struct{ name, clients string }{
		{"the bridge alone", ""},
		{"a neighbour nearby", "/dev/pts/9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newTmuxStub(t, []string{"4242 real:1.2"}, "")
			stub.reply("display-message", "158x40")
			stub.reply("list-clients", tc.clients)

			openBridge(t, stub)

			calls := tmuxCalls(t, stub.log)
			var attached bool
			for _, c := range calls {
				if len(c) == 0 {
					continue
				}
				switch c[0] {
				case "resize-window":
					if !attached {
						t.Errorf("the window was pinned before the bridge (%v) — the phone will see half a line", c)
					}
				case "attach-session":
					attached = true
				}
			}
			if !attached {
				t.Fatalf("attach-session was not called at all: %v", calls)
			}
		})
	}
}

func TestBridgeGivesTheWindowBack(t *testing.T) {
	stub := newTmuxStub(t, []string{"4242 real:1.2"}, "")
	stub.reply("display-message", "158x40")

	term := openBridge(t, stub)

	stub.reply("display-message", "40x31")
	stub.reply("list-clients", "")
	term.Close()

	calls := tmuxCalls(t, stub.log)
	resize := tmuxCall(calls, "resize-window")
	if resize == nil {
		t.Fatalf("the window stayed phone-sized — resize-window was not called: %v", calls)
	}
	if !slices.Contains(resize, "real:1") {
		t.Errorf("the size was given back to a window other than the session one: %v", resize)
	}
	if !slices.Contains(resize, "158") || !slices.Contains(resize, "40") {
		t.Errorf("the size given back is not the one from before the bridge: %v", resize)
	}
	set := tmuxCall(calls, "set-option")
	var freed bool
	for _, c := range calls {
		if len(c) > 0 && c[0] == "set-option" && slices.Contains(c, "-uw") && slices.Contains(c, "window-size") {
			freed = true
		}
	}
	if !freed {
		t.Errorf("the window stayed in window-size manual: %v, the first set-option is %v", calls, set)
	}
}

func TestBridgeLeavesTheWindowToWhoeverWatches(t *testing.T) {
	stub := newTmuxStub(t, []string{"4242 real:1.2"}, "")
	stub.reply("display-message", "158x40")
	stub.reply("list-clients", "/dev/pts/9")

	term := openBridge(t, stub)

	stub.reply("display-message", "40x31")
	term.Close()

	if resize := tmuxCall(tmuxCalls(t, stub.log), "resize-window"); resize != nil {
		t.Errorf("the size was overridden with a live neighbour around: %v", resize)
	}
}

func TestBridgeKeepsTheWindowSizeOptionItFound(t *testing.T) {
	stub := newTmuxStub(t, []string{"4242 real:1.2"}, "")
	stub.reply("display-message", "158x40")
	stub.reply("show-options", "window-size largest")

	term := openBridge(t, stub)

	stub.reply("display-message", "40x31")
	stub.reply("list-clients", "")
	term.Close()

	var restored bool
	for _, c := range tmuxCalls(t, stub.log) {
		if len(c) > 0 && c[0] == "set-option" && slices.Contains(c, "window-size") && slices.Contains(c, "largest") {
			restored = true
		}
		if len(c) > 0 && c[0] == "set-option" && slices.Contains(c, "-uw") {
			t.Errorf("the own window-size value was cleared instead of given back: %v", c)
		}
	}
	if !restored {
		t.Error("the window lost its own window-size: it was not given back")
	}
}

func TestBridgeDoesNotResizeAWindowItNeverChanged(t *testing.T) {
	stub := newTmuxStub(t, []string{"4242 real:1.2"}, "")
	stub.reply("display-message", "158x40")
	stub.reply("list-clients", "")
	stub.reply("show-options", "window-size manual")

	term := openBridge(t, stub)
	term.Close()

	if resize := tmuxCall(tmuxCalls(t, stub.log), "resize-window"); resize != nil {
		t.Errorf("the window was resized though the bridge never changed it: %v", resize)
	}
}

func ownTmuxServer(t *testing.T) {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed — there is nothing to check a live bridge with")
	}
	// A socket of its own per test, not one per process: the cleanup of a test
	// kills the server on its socket, and a shared one is killed under whichever
	// test starts next — it meets a server on its way out and is told the server
	// exited unexpectedly.
	sock := fmt.Sprintf("aacp-term-%d-%s", os.Getpid(), strings.ReplaceAll(t.Name(), "/", "-"))
	wrapper := filepath.Join(t.TempDir(), "tmux")
	script := fmt.Sprintf("#!/bin/sh\nexec %q -L %s \"$@\"\n", bin, sock)
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command(bin, "-L", sock, "kill-server").Run() })
	t.Setenv(tmuxEnv, wrapper)
}

func liveTmuxSession(t *testing.T, name string, cols, rows int) string {
	t.Helper()
	if _, err := tmuxRun(t.Context(), "-f", "/dev/null", "new-session", "-d", "-s", name,
		"-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), "cat"); err != nil {
		t.Fatalf("the tmux server did not come up: %v", err)
	}
	panes, err := tmuxRun(t.Context(), "list-panes", "-a", "-F", paneFormat)
	if err != nil {
		t.Fatalf("the panes could not be asked for: %v", err)
	}
	owner, target, ok := strings.Cut(strings.TrimSpace(panes), " ")
	if !ok {
		t.Fatalf("the pane list arrived as %q", panes)
	}
	pid, err := strconv.Atoi(owner)
	if err != nil {
		t.Fatalf("the pid of the pane arrived as %q", owner)
	}
	procFS(t, fakeProc{pid: pid, comm: "claude", args: []string{"claude", "-n", name}, ppid: 1, start: "77"})
	sessionFiles(t, fakeSession{pid: pid, name: name, start: "77"})
	return target
}

func TestLiveWindowFollowsTheBridgeClient(t *testing.T) {
	ownTmuxServer(t)
	const (
		name = "probe"
		win  = name + ":0"
		cols = 50
		rows = 29
	)
	target := liveTmuxSession(t, name, 116, 110)

	before := tmuxWindowSize(t.Context(), win)
	if before != "116x110" {
		t.Fatalf("the window came up sized %q instead of 116x110", before)
	}

	term, err := NewTermOpener().Open(t.Context(), name, cols, rows)
	if err != nil {
		t.Fatalf("the bridge did not open to %s: %v", target, err)
	}
	go io.Copy(io.Discard, term)

	under := before
	for range 60 {
		if under = tmuxWindowSize(t.Context(), win); under != before {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	w, h, _ := strings.Cut(under, "x")
	if w != strconv.Itoa(cols) {
		t.Errorf("the window under the bridge is %s while the client has %d columns — the line is cut and the phone sees a part of it", under, cols)
	}
	if height, err := strconv.Atoi(h); err != nil || height > rows {
		t.Errorf("the window under the bridge is %s, taller than the client with %d rows", under, rows)
	}

	term.Close()
	if after := tmuxWindowSize(t.Context(), win); after != before {
		t.Errorf("the window stayed %s instead of %s — whoever opens the terminal next sees a size that is not theirs", after, before)
	}
	if opt := tmuxWindowSizeOption(t.Context(), win); opt != "" {
		t.Errorf("the window stayed in window-size %q: giving the size back did not clear its own option", opt)
	}
}

func TestLiveBridgeGivesTheWindowBack(t *testing.T) {
	name := os.Getenv("AACP_LIVE_SESSION")
	if name == "" {
		t.Skip("no live session is named: AACP_LIVE_SESSION=<name of the claude session in tmux>")
	}
	win := ""
	if s, err := findOneLiveSession(name); err == nil {
		if p, err := tmuxPaneFor(t.Context(), s.PID); err == nil {
			win = tmuxWindowOf(p.Target)
		}
	}
	if win == "" {
		t.Fatalf("the pane of session %s was not found", name)
	}
	before := tmuxWindowSize(t.Context(), win)
	t.Logf("window %s before the bridge: %s", win, before)

	term, err := NewTermOpener().Open(t.Context(), name, 40, 32)
	if err != nil {
		t.Fatalf("the bridge did not open: %v", err)
	}
	go io.Copy(io.Discard, term)
	time.Sleep(1500 * time.Millisecond)
	under := tmuxWindowSize(t.Context(), win)
	t.Logf("the window under the bridge: %s", under)

	term.Close()
	time.Sleep(500 * time.Millisecond)
	after := tmuxWindowSize(t.Context(), win)
	opt := tmuxWindowSizeOption(t.Context(), win)
	t.Logf("the window after the bridge: %s, window-size of the window: %q", after, opt)

	if after != before {
		t.Errorf("the window stayed %s instead of %s — the person who opens the terminal next sees a size that is not theirs", after, before)
	}
}

func bridgeSeesWholeWindow(t *testing.T, session string, cols, rows int) (bool, string) {
	t.Helper()
	out, err := tmuxRun(t.Context(), "list-clients", "-t", session, "-F",
		"#{client_width}x#{client_height} #{window_bigger} #{window_offset_x},#{window_offset_y}")
	if err != nil {
		t.Fatalf("the clients could not be asked for: %v", err)
	}
	want := fmt.Sprintf("%dx%d ", cols, rows)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, want) {
			return strings.Contains(line, " 0 "), line
		}
	}
	t.Fatalf("the bridge client %dx%d is not among %q", cols, rows, out)
	return false, ""
}

func attachNeighbour(t *testing.T, target string, cols, rows uint16) func() {
	t.Helper()
	pty, err := openPTY(cols, rows)
	if err != nil {
		t.Fatalf("the pty of the neighbour did not open: %v", err)
	}
	cmd := exec.Command(tmuxBin(), "attach-session", "-t", target)
	cmd.Env = termEnv()
	pty.attach(cmd)
	if err := cmd.Start(); err != nil {
		pty.Close()
		t.Fatalf("the neighbour did not attach to %s: %v", target, err)
	}
	pty.closeSlave()
	go io.Copy(io.Discard, pty.master)
	t.Cleanup(func() {
		pty.Close()
		cmd.Wait()
	})
	return func() {
		if _, err := pty.master.Write([]byte(" ")); err != nil {
			t.Fatalf("the neighbour did not press a key: %v", err)
		}
	}
}

func TestLiveBridgeSeesTheWholeWindowUnderALiveNeighbour(t *testing.T) {
	ownTmuxServer(t)
	const (
		name = "probe"
		win  = name + ":0"
		cols = 60
		rows = 30
	)
	target := liveTmuxSession(t, name, 120, 50)

	press := attachNeighbour(t, target, 120, 50)
	alone := ""
	for range 60 {
		if alone = tmuxWindowSize(t.Context(), win); alone != "" && alone != "120x50" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	term, err := NewTermOpener().Open(t.Context(), name, cols, rows)
	if err != nil {
		t.Fatalf("the bridge did not open to %s: %v", target, err)
	}
	go io.Copy(io.Discard, term)
	for range 60 {
		if tmuxWindowSize(t.Context(), win) != alone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	press()
	time.Sleep(700 * time.Millisecond)

	whole, line := bridgeSeesWholeWindow(t, name, cols, rows)
	if !whole {
		t.Errorf("the bridge sees only part of window %s: %s — the person in the panel reads a strip, "+
			"and the session mark «not everything is shown» is carried off its edge",
			tmuxWindowSize(t.Context(), win), line)
	}

	term.Close()
	if opt := tmuxWindowSizeOption(t.Context(), win); opt != "" {
		t.Errorf("the window stayed in window-size %q: the bridge did not clear its own rule", opt)
	}
	back := ""
	for range 60 {
		if back = tmuxWindowSize(t.Context(), win); back == alone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if back != alone {
		t.Errorf("the window stayed %s instead of %s — the neighbour is looking at the size of a bridge that left", back, alone)
	}
}

func TestLiveBridgeLeavesAPinnedWindowAlone(t *testing.T) {
	ownTmuxServer(t)
	const (
		name = "probe"
		win  = name + ":0"
	)
	target := liveTmuxSession(t, name, 120, 50)
	if _, err := tmuxRun(t.Context(), "set-option", "-g", "window-size", "manual"); err != nil {
		t.Fatalf("the window did not get pinned: %v", err)
	}

	term, err := NewTermOpener().Open(t.Context(), name, 60, 30)
	if err != nil {
		t.Fatalf("the bridge did not open to %s: %v", target, err)
	}
	go io.Copy(io.Discard, term)
	time.Sleep(700 * time.Millisecond)

	if rule := tmuxWindowSizeRule(t.Context(), win); rule != "manual" {
		t.Errorf("the window went into window-size %q — the bridge overrode the decision of the owner about the size", rule)
	}
	if size := tmuxWindowSize(t.Context(), win); size != "120x50" {
		t.Errorf("the pinned window became %s instead of 120x50", size)
	}
	term.Close()
	if opt := tmuxWindowSizeOption(t.Context(), win); opt != "" {
		t.Errorf("an own option %q landed on the window on top of the ban set globally", opt)
	}
}
