package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func contourHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	contourDirs(t, dir)
	return dir
}

func TestRunPutsContourConfigIntoSession(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}})
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	machineClaude(t)
	conf := contourHome(t)

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel", ConfigDir: conf})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readFile(t, envLog(tmuxLog))); !strings.Contains(got, "CLAUDE_CONFIG_DIR="+conf) {
		t.Errorf("the session came up without the profile directory: %s", got)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("an honest route drew complaints: %v", rep.Warnings)
	}
}

func TestWrapperKeepsTheRouteToItself(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	conf := contourHome(t)

	wrapper := filepath.Join(t.TempDir(), "profile-claude")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), Spec{
		Dir: t.TempDir(), Session: "aacpanel", ClaudeBin: wrapper, ConfigDir: conf,
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(readFile(t, envLog(tmuxLog))); strings.Contains(got, "CLAUDE_CONFIG_DIR=") {
		t.Errorf("the panel set the route on top of the wrapper — now two things decide the route: %s", got)
	}
}

func TestRunRefusesMissingContourConfig(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	machineClaude(t)
	contourHome(t)

	missing := filepath.Join(t.TempDir(), "no-such-profile-dir")
	_, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel", ConfigDir: missing})
	if err == nil {
		t.Fatal("the session was started past the profile directory — that is a silent move into the personal account")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the refusal does not name the path, and that is the whole explanation: %v", err)
	}
	if _, err := os.ReadFile(tmuxLog); err == nil {
		t.Error("the tmux session was started after all: the refusal came after the conversation had started")
	}
}

func TestChildEnvContourConfigBeatsInherited(t *testing.T) {
	fakeProc(t)
	env, _ := childEnv([]string{"HOME=/home/u", "CLAUDE_CONFIG_DIR=/home/u/.claude"},
		Params{}, ":10", "", "/home/u/.claude-contours/work")
	joined := strings.Join(env, " ")
	if !strings.Contains(joined, "CLAUDE_CONFIG_DIR=/home/u/.claude-contours/work") {
		t.Errorf("the map lost to the inheritance: %v", env)
	}
	if strings.Contains(joined, "CLAUDE_CONFIG_DIR=/home/u/.claude ") {
		t.Errorf("the inherited route stayed next to our own: %v", env)
	}
}

func npmTmux(t *testing.T, proc, conf, name string, pid int) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	agentDir := filepath.Join(proc, fmt.Sprint(pid))
	stat := fmt.Sprintf("%d (node) S 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 7777\\n", pid)
	session := filepath.Join(conf, "sessions", fmt.Sprint(pid)+".json")
	body := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"new-session)\n" +
		"  mkdir -p " + agentDir + "\n" +
		"  printf 'node\\n' > " + agentDir + "/comm\n" +
		"  printf 'node\\0/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js\\0-n\\0" +
		name + "\\0' > " + agentDir + "/cmdline\n" +
		"  printf '' > " + agentDir + "/environ\n" +
		"  printf '" + stat + "' > " + agentDir + "/stat\n" +
		"  printf '{\"pid\":" + fmt.Sprint(pid) + ",\"name\":\"" + name +
		"\",\"kind\":\"interactive\",\"procStart\":\"7777\"}' > " + session + "\n" +
		"  ;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, script)
}

func TestRunFindsSessionWhoseProcessIsNode(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	conf := contourHome(t)
	npmTmux(t, proc, conf, "aacpanel", 200)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	machineClaude(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rep, err := Run(ctx, Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil {
		t.Fatalf("a live session was not recognised: %v", err)
	}
	if rep.Agent != 200 {
		t.Errorf("agent %d instead of 200 — the wrong process was recognised", rep.Agent)
	}

	name, err := freeName("aacpanel", takenNames())
	if err != nil {
		t.Fatal(err)
	}
	if name != "aacpanel-2" {
		t.Errorf("the next session was given the name %q — the same one a live session already has", name)
	}
	open := Open()
	if len(open) != 1 || open[0].Session != "aacpanel" {
		t.Errorf("list of open sessions: %+v", open)
	}
}

func TestDeadSessionFileHoldsNothing(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 200, comm: "node", args: []string{"node", "cli.js", "-n", "aacpanel"}, start: "1000"})
	conf := contourHome(t)
	file := filepath.Join(conf, "sessions", "200.json")
	body := `{"pid":200,"name":"aacpanel","kind":"interactive","procStart":"7777"}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = proc

	if taken := takenNames(); taken["aacpanel"] {
		t.Error("the name is held by the file of a dead session: the pid gets reused, and the name stays taken forever")
	}

	live := `{"pid":200,"name":"aacpanel","kind":"interactive","procStart":"1000"}`
	if err := os.WriteFile(file, []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
	if taken := takenNames(); !taken["aacpanel"] {
		t.Error("a live session does not hold its own name — the next one gets the same")
	}
}

func TestBackgroundSessionsDoNotHoldNames(t *testing.T) {
	fakeProc(t, fproc{pid: 200, comm: "node", args: []string{"node", "cli.js", "-n", "aacpanel"}, start: "1000"})
	conf := contourHome(t)
	body := `{"pid":200,"name":"aacpanel","kind":"bg","procStart":"1000"}`
	if err := os.WriteFile(filepath.Join(conf, "sessions", "200.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if taken := takenNames(); taken["aacpanel"] {
		t.Error("the name is held by a background fork — a live session then gets a suffix for nothing")
	}
}
