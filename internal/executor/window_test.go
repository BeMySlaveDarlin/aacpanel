package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/launcher"
)

func fakeWindowTmux(t *testing.T, panes, clients []string, paneDir string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	bin := filepath.Join(dir, "tmux")

	write := func(name string, lines []string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	panesFile := write("panes", panes)
	clientsFile := write("clients", clients)
	dirFile := write("dir", []string{paneDir})
	lateFile := filepath.Join(dir, "late")
	countFile := filepath.Join(dir, "count")

	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a" >> %q; done
printf -- '--\n' >> %q
case "$1" in
list-panes) cat %q ;;
list-clients)
  n=$(cat %q 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > %q
  late=$(cat %q 2>/dev/null || echo 0)
  if [ "$n" -gt "$late" ]; then cat %q; fi ;;
display) cat %q ;;
esac
exit 0
`, log, log, panesFile, countFile, countFile, lateFile, clientsFile, dirFile)

	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, bin)
	return log
}

func windowStage(t *testing.T, clients []string) string {
	t.Helper()
	procFS(t,
		fakeProc{pid: 900, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 901, comm: "claude", args: []string{"claude"}, ppid: 4242, start: "77"},
		fakeProc{pid: 940, comm: "tmux", args: []string{"tmux", "attach"}, ppid: 900},
		fakeProc{pid: 950, comm: "tmux", args: []string{"tmux", "attach-session"}, ppid: os.Getpid()},
	)
	sessionFiles(t, fakeSession{pid: 901, name: "aacpanel", start: "77", status: "idle"})
	return fakeWindowTmux(t, []string{"901 aacpanel:0.0"}, clients, "/srv/proj")
}

func TestWindowStateSkipsThePanelBridge(t *testing.T) {
	windowStage(t, []string{"/dev/pts/9 950"})

	e := &Executor{}
	win, err := e.Window(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the state of the window could not be read: %v", err)
	}
	if win == nil || win.Open {
		t.Errorf("state %+v — the bridge of the panel was counted as a window", win)
	}
}

func TestWindowStateSeesForeignClient(t *testing.T) {
	windowStage(t, []string{"/dev/pts/6 940", "/dev/pts/9 950"})

	e := &Executor{}
	win, err := e.Window(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the state of the window could not be read: %v", err)
	}
	if win == nil || !win.Open {
		t.Errorf("state %+v — the window of the person went unseen", win)
	}
}

func TestWindowCloseDetachesForeignClientsOnly(t *testing.T) {
	log := windowStage(t, []string{"/dev/pts/6 940", "/dev/pts/9 950"})

	e := &Executor{}
	detail, err := e.windowClose(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the window did not close: %v", err)
	}
	if !strings.Contains(detail, "aacpanel") {
		t.Errorf("the reply %q does not name the session", detail)
	}

	argv := tmuxArgv(t, log)
	said := strings.Join(argv, " ")
	if !strings.Contains(said, "detach-client") || !strings.Contains(said, "/dev/pts/6") {
		t.Errorf("the window of the person was not detached: %v", argv)
	}
	if strings.Contains(said, "/dev/pts/9") {
		t.Errorf("the bridge of the panel was detached — the terminal screen would go dark under the hands: %v", argv)
	}
}

func TestWindowCloseSaysNothingToCloseInsteadOfFailing(t *testing.T) {
	log := windowStage(t, []string{"/dev/pts/9 950"})

	e := &Executor{}
	detail, err := e.windowClose(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("a refusal instead of a reply: %v", err)
	}
	if !strings.Contains(detail, "had no windows") {
		t.Errorf("the reply %q does not say there was nothing to close", detail)
	}
	if strings.Contains(strings.Join(tmuxArgv(t, log), " "), "detach-client") {
		t.Error("a detach went out after all — with a single bridge there is nobody to detach")
	}
}

func TestWindowOpenTakesDirFromTmux(t *testing.T) {
	windowStage(t, []string{"/dev/pts/9 940"})
	body, err := json.Marshal(launcher.Report{Session: "aacpanel", Dir: "/srv/proj", Konsole: 4321})
	if err != nil {
		t.Fatal(err)
	}
	spec := launcherScript(t, "{ echo \"$@\"; cat; } > %s\n"+
		"cat <<'END'\n"+string(body)+"\nEND\n")

	e := &Executor{}
	detail, err := e.windowOpen(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the window did not open: %v", err)
	}
	if !strings.Contains(detail, "aacpanel") {
		t.Errorf("the reply %q does not name the session", detail)
	}

	raw, err := os.ReadFile(spec)
	if err != nil {
		t.Fatalf("the launcher was not called: %v", err)
	}
	said := string(raw)
	if !strings.Contains(said, "-window") {
		t.Errorf("the launcher was called not in window mode: %s", said)
	}
	if !strings.Contains(said, `"dir":"/srv/proj"`) {
		t.Errorf("the directory of the window was taken from somewhere other than tmux: %s", said)
	}
	if !strings.Contains(said, `"session":"aacpanel"`) {
		t.Errorf("the session name did not reach the launcher: %s", said)
	}
}

func TestWindowOpenCarriesLauncherWarnings(t *testing.T) {
	windowStage(t, []string{"/dev/pts/9 940"})
	body, err := json.Marshal(launcher.Report{
		Session: "aacpanel", Konsole: 4321,
		Warnings: []string{"no graphical session found on DISPLAY=:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	launcherScript(t, "cat > %s\ncat <<'END'\n"+string(body)+"\nEND\n")

	e := &Executor{}
	detail, err := e.windowOpen(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the window did not open: %v", err)
	}
	if !strings.Contains(detail, "graphical session") {
		t.Errorf("the warning from the launcher was swallowed: %q", detail)
	}
}

func TestWindowRefusesSessionOutsideTmux(t *testing.T) {
	procFS(t,
		fakeProc{pid: 900, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 901, comm: "claude", args: []string{"claude"}, ppid: 900, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 901, name: "aacpanel", start: "77", status: "idle"})
	fakeWindowTmux(t, nil, nil, "")

	e := &Executor{}
	if _, err := e.windowOpen(t.Context(), "aacpanel"); err == nil ||
		!strings.Contains(err.Error(), "does not live in tmux") {
		t.Errorf("opening: %v — the reason is not named", err)
	}
	if _, err := e.windowClose(t.Context(), "aacpanel"); err == nil ||
		!strings.Contains(err.Error(), "does not live in tmux") {
		t.Errorf("closing: %v — the reason is not named", err)
	}
	if _, err := e.Window(t.Context(), "aacpanel"); err == nil {
		t.Error("the state of a window was read for a session that is not in tmux")
	}
}

func quickWindowWait(t *testing.T, wait time.Duration) {
	t.Helper()
	prevWait, prevPoll := windowWait, windowPoll
	windowWait, windowPoll = wait, 5*time.Millisecond
	t.Cleanup(func() { windowWait, windowPoll = prevWait, prevPoll })
}

func TestWindowOpenWaitsForTheClient(t *testing.T) {
	log := windowStage(t, []string{"/dev/pts/9 940"})
	if err := os.WriteFile(filepath.Join(filepath.Dir(log), "late"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	quickWindowWait(t, 2*time.Second)
	body, _ := json.Marshal(launcher.Report{Session: "aacpanel", Dir: "/srv/proj", Konsole: 4321})
	launcherScript(t, "cat > %s\ncat <<'END'\n"+string(body)+"\nEND\n")

	e := &Executor{}
	detail, err := e.windowOpen(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the window did not open: %v", err)
	}
	if strings.Contains(detail, "WARNING") {
		t.Errorf("the client attached on the third poll, yet the reply sounds the alarm: %q", detail)
	}
	raw, _ := os.ReadFile(log)
	if n := strings.Count(string(raw), "list-clients"); n < 3 {
		t.Errorf("list-clients was asked %d times — there was no waiting", n)
	}
}

func TestWindowOpenWarnsWhenNobodyAttaches(t *testing.T) {
	windowStage(t, nil)
	quickWindowWait(t, 40*time.Millisecond)
	body, _ := json.Marshal(launcher.Report{Session: "aacpanel", Dir: "/srv/proj", Konsole: 4321})
	launcherScript(t, "cat > %s\ncat <<'END'\n"+string(body)+"\nEND\n")

	e := &Executor{}
	detail, err := e.windowOpen(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("a missing client turned into a refusal: %v", err)
	}
	if !strings.Contains(detail, "nobody attached") {
		t.Errorf("the reply says nothing about the client being absent: %q", detail)
	}
}
