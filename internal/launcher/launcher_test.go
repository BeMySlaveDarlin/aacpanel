package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fproc struct {
	pid   int
	comm  string
	args  []string
	ppid  int
	env   []string
	start string
}

func fakeProc(t *testing.T, procs ...fproc) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range procs {
		dir := filepath.Join(root, fmt.Sprint(p.pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(name, body string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("comm", p.comm+"\n")
		write("cmdline", strings.Join(p.args, "\x00")+"\x00")
		write("environ", strings.Join(p.env, "\x00")+"\x00")
		start := p.start
		if start == "" {
			start = "1000"
		}
		fields := []string{"S", fmt.Sprint(p.ppid)}
		for len(fields) < 19 {
			fields = append(fields, "0")
		}
		fields = append(fields, start)
		write("stat", fmt.Sprintf("%d (%s) %s\n", p.pid, p.comm, strings.Join(fields, " ")))
	}
	t.Setenv(procEnv, root)
	return root
}

// liveBus is an address a session bus really answers at. The graphical session
// hands its address down to the child, and a start that got only a dead path
// says so in the report — so a test that expects no complaints has to give the
// session a socket that is really there.
func liveBus(t *testing.T) string {
	t.Helper()
	// Not t.TempDir(): a directory named after the test function puts the
	// socket path over the length AF_UNIX takes.
	dir, err := os.MkdirTemp("", "bus")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "socket")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return "unix:path=" + path
}

func contourDirs(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv(configEnv, strings.Join(dirs, string(os.PathListSeparator)))
	t.Setenv(registryEnv, filepath.Join(t.TempDir(), "no-registry.conf"))
}

func TestClaudeArgsFromLaunchParams(t *testing.T) {
	cases := []struct {
		name   string
		launch string
		resume string
		want   []string
	}{
		{
			name:   "empty — only the name, and no remote control unasked",
			launch: `{}`,
			want:   []string{"-n", "home"},
		},
		{
			name:   "remote control switched on",
			launch: `{"remoteControl":true}`,
			want:   []string{"-n", "home", "--remote-control", "home"},
		},
		{
			name:   "the whole map",
			launch: `{"remoteControl":true,"model":"opus","effort":"high","permissionMode":"plan","args":["--add-dir","/opt/x"]}`,
			want: []string{"-n", "home", "--remote-control", "home", "--model", "opus",
				"--effort", "high", "--permission-mode", "plan", "--add-dir", "/opt/x"},
		},
		{
			name:   "remote control switched off explicitly",
			launch: `{"remoteControl":false}`,
			want:   []string{"-n", "home"},
		},
		{
			name:   "resuming a conversation",
			launch: `{}`,
			resume: "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1",
			want:   []string{"-n", "home", "--resume", "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, warns := parseParams(json.RawMessage(c.launch))
			if len(warns) != 0 {
				t.Fatalf("honest parameters drew complaints: %v", warns)
			}
			got := claudeArgs("home", c.resume, p)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("claude arguments:\n got %v\n want %v", got, c.want)
			}
		})
	}
}

func TestParseParamsNamesWhatItCannotUse(t *testing.T) {
	cases := map[string]string{
		"typo in the key name":         `{"permission_mode":"plan"}`,
		"model is not a string":        `{"model":42}`,
		"remote control is not yes/no": `{"remoteControl":"yes"}`,
		"args are not a list":          `{"args":"--add-dir /opt/x"}`,
		"env is not an object":         `{"env":["FOO=bar"]}`,
		"not an object at all":         `[1,2,3]`,
	}
	for name, launch := range cases {
		t.Run(name, func(t *testing.T) {
			_, warns := parseParams(json.RawMessage(launch))
			if len(warns) == 0 {
				t.Errorf("parameters %s accepted silently — the session comes up as the wrong one", launch)
			}
		})
	}

	p, warns := parseParams(json.RawMessage(`{"model":42,"effort":"high"}`))
	if p.Effort != "high" {
		t.Errorf("a sound parameter was lost because of its neighbour: %+v", p)
	}
	if len(warns) == 0 {
		t.Error("the broken parameter was not named")
	}
}

// Claude drops an effort it does not take at launch and starts at its
// default, with a line on a terminal nobody reads; the launcher names it and
// passes nothing, and ultracode, the one the composer offers, is among them.
func TestParseParamsNamesAnEffortClaudeDoesNotTakeAtLaunch(t *testing.T) {
	for _, effort := range []string{"ultracode", "extreme"} {
		p, warns := parseParams(json.RawMessage(`{"effort":"` + effort + `"}`))
		if len(warns) == 0 {
			t.Errorf("effort %q accepted silently", effort)
		}
		if p.Effort != "" || slices.Contains(claudeArgs("home", "", p), "--effort") ||
			slices.Contains(streamArgs("home", "id", "", p), "--effort") {
			t.Errorf("effort %q went on to claude: %+v", effort, p)
		}
	}
	p, warns := parseParams(json.RawMessage(`{"effort":"max"}`))
	if len(warns) != 0 || p.Effort != "max" {
		t.Errorf("max was turned away: %+v %v", p, warns)
	}
}

func TestChildEnvKeepsProfileRoute(t *testing.T) {
	fakeProc(t)
	own := []string{
		"HOME=/home/u",
		"CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=abc",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_PROFILE=work",
		"CLAUDE_CONFIG_DIR=/home/u/.claude-contours/work",
		"CLAUDE_CODE_OAUTH_TOKEN=secret",
		"NOTIFY_SOCKET=/run/systemd/notify",
	}
	env, _ := childEnv(own, Params{}, ":10", "", "")
	has := func(name string) bool {
		for _, kv := range env {
			if strings.HasPrefix(kv, name+"=") {
				return true
			}
		}
		return false
	}

	for _, name := range routeVars {
		if !has(name) {
			t.Errorf("%s was stripped together with the inheritance — the session moves into another account: %v", name, env)
		}
	}
	for _, name := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_ENTRYPOINT"} {
		if has(name) {
			t.Errorf("%s reached the session — the transcript will not be written: %v", name, env)
		}
	}
	if has("NOTIFY_SOCKET") {
		t.Errorf("a unit variable reached the new process: %v", env)
	}
}

func TestLeaksIgnoreProfileRoute(t *testing.T) {
	fakeProc(t, fproc{pid: 200, comm: "claude", env: []string{
		"CLAUDE_PROFILE=work",
		"CLAUDE_CONFIG_DIR=/home/u/.claude-contours/work",
		"CLAUDE_CODE_OAUTH_TOKEN=secret",
		"HOME=/home/u",
	}})
	if got := leaks(200); len(got) != 0 {
		t.Errorf("the profile route was counted as a leak: %v", got)
	}

	fakeProc(t, fproc{pid: 200, comm: "claude", env: []string{
		"CLAUDE_PROFILE=work", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=abc",
	}})
	got := leaks(200)
	if strings.Join(got, ",") != "CLAUDECODE,CLAUDE_CODE_SESSION_ID" {
		t.Errorf("the real leak was not found, or not named in full: %v", got)
	}
}

func TestBusWarningLooksBehindTheAddress(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(plain, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		addr  string
		quiet bool
	}{
		{name: "a socket is really there", addr: liveBus(t), quiet: true},
		{name: "the guid follows the path", addr: liveBus(t) + ",guid=ca2eaaa9b2173bf54d49ed506aa5ebe5", quiet: true},
		{name: "no bus is named at all", addr: "", quiet: true},
		{name: "an address that is not a path", addr: "unix:abstract=/tmp/dbus-3T9CkQ", quiet: true},
		{name: "the path the address names is gone", addr: "unix:path=" + filepath.Join(dir, "gone")},
		{name: "the path leads to a file, not a socket", addr: "unix:path=" + plain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warn := checkBus(c.addr)
			if c.quiet && warn != "" {
				t.Errorf("a bus that answers drew a complaint: %s", warn)
			}
			if !c.quiet && warn == "" {
				t.Errorf("nothing was said about %s — a session on such a bus hangs where it looks like it works", c.addr)
			}
		})
	}
}

func TestChildEnvTakesGraphicalSession(t *testing.T) {
	ours, theirs := liveBus(t), liveBus(t)
	fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{
			"DISPLAY=:0", "DBUS_SESSION_BUS_ADDRESS=" + theirs,
		}},
		fproc{pid: 11, comm: "kwin_x11", env: []string{
			"DISPLAY=:10.0",
			"DBUS_SESSION_BUS_ADDRESS=" + ours,
			"XAUTHORITY=/home/u/.Xauthority",
			"KDE_FULL_SESSION=true",
			"SSH_AUTH_SOCK=/eavesdropped",
		}},
	)
	env, warns := childEnv([]string{"DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}, Params{}, ":10", "", "")
	joined := strings.Join(env, " ")

	if !strings.Contains(joined, "DBUS_SESSION_BUS_ADDRESS="+ours) {
		t.Errorf("the bus stayed the caller's — Chrome hangs in such a session: %v", env)
	}
	if strings.Contains(joined, theirs) {
		t.Errorf("the environment was taken from a session on another screen: %v", env)
	}
	if !strings.Contains(joined, "KDE_FULL_SESSION=true") {
		t.Errorf("the graphical session environment did not arrive: %v", env)
	}
	if strings.Contains(joined, "SSH_AUTH_SOCK=/eavesdropped") {
		t.Errorf("more than needed was taken from another process: %v", env)
	}
	if len(warns) != 0 {
		t.Errorf("a graphical session whose bus answers drew complaints: %v", warns)
	}
}

func TestChildEnvKeepsADeadBusOutOfTheSession(t *testing.T) {
	dead := "unix:path=" + filepath.Join(t.TempDir(), "gone")
	named := func(t *testing.T, env []string) {
		t.Helper()
		for _, kv := range env {
			if strings.HasPrefix(kv, "DBUS_SESSION_BUS_ADDRESS=") {
				t.Errorf("the session was handed %q — clients hang on a path that answers nobody", kv)
			}
		}
	}
	told := func(t *testing.T, warns []string) {
		t.Helper()
		for _, w := range warns {
			if strings.Contains(w, dead) && strings.Contains(w, "--password-store=basic") {
				return
			}
		}
		t.Errorf("warnings %v neither name the dead bus nor say what the session now needs", warns)
	}

	t.Run("the graphical session hands down a path that is gone", func(t *testing.T) {
		fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{
			"DISPLAY=:10", "KDE_FULL_SESSION=true", "DBUS_SESSION_BUS_ADDRESS=" + dead,
		}})
		env, warns := childEnv([]string{"DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}, Params{}, ":10", "", "")
		named(t, env)
		told(t, warns)
	})

	t.Run("no graphical session was found and the caller's own bus is gone", func(t *testing.T) {
		fakeProc(t)
		env, warns := childEnv([]string{"DBUS_SESSION_BUS_ADDRESS=" + dead}, Params{}, ":10", "", "")
		named(t, env)
		told(t, warns)
	})
}

func TestChildEnvSaysWhenNoGraphicalSession(t *testing.T) {
	fakeProc(t)
	_, warns := childEnv(nil, Params{}, ":10", "", "")
	if len(warns) == 0 {
		t.Fatal("a missing graphical session passed silently — a start from a unit then looks normal")
	}
	if !strings.Contains(strings.Join(warns, " "), ":10") {
		t.Errorf("warning %v does not name the screen", warns)
	}
}

func TestChildEnvKeepsQuietWhereNoWindowsAreOpened(t *testing.T) {
	fakeProc(t)
	t.Setenv(terminalEnv, "")
	_, warns := childEnv(nil, Params{}, ":0", "", "")
	for _, w := range warns {
		if strings.Contains(w, "graphical session") {
			t.Errorf("no windows are opened, yet the graphical session is mentioned: %q", w)
		}
	}
}

func TestChildEnvLaunchEnvWins(t *testing.T) {
	fakeProc(t)
	env, _ := childEnv([]string{"HOME=/home/u"}, Params{Env: map[string]string{
		"CLAUDE_CODE_TMPDIR": "/mnt/data/scratch",
		"FOO":                "bar",
	}}, ":10", "", "")
	joined := strings.Join(env, " ")
	if !strings.Contains(joined, "CLAUDE_CODE_TMPDIR=/mnt/data/scratch") {
		t.Errorf("what the map set lost to the default: %v", env)
	}
	if !strings.Contains(joined, "FOO=bar") {
		t.Errorf("the variable from the map did not arrive: %v", env)
	}

	env, _ = childEnv([]string{"HOME=/home/u"}, Params{}, ":10", "", "")
	if !strings.Contains(strings.Join(env, " "), "CLAUDE_CODE_TMPDIR=/home/u/.cache/claude-tmp") {
		t.Errorf("the scratchpad stayed in /tmp: %v", env)
	}
}

func TestFreeNameIgnoresPhantomKonsole(t *testing.T) {
	fakeProc(t,
		fproc{pid: 100, comm: "konsole", args: []string{
			"konsole", "--workdir", "/srv/proj/aacpanel", "-e", "/bin/claude", "-n", "aacpanel",
		}},
		fproc{pid: 300, comm: "claude", args: []string{"claude", "-p", "-n", "aacpanel"}},
	)
	if name, err := freeName("aacpanel", takenNames()); err != nil || name != "aacpanel" {
		t.Fatalf("name %q (%v) — a phantom window took the name away from a live session", name, err)
	}

	fakeProc(t,
		fproc{pid: 200, comm: "claude", args: []string{"claude", "-n", "aacpanel"}},
		fproc{pid: 201, comm: "claude", args: []string{"claude", "--name=aacpanel-2"}},
	)
	name, err := freeName("aacpanel", takenNames())
	if err != nil {
		t.Fatal(err)
	}
	if name != "aacpanel-3" {
		t.Errorf("name %q — taken names are counted wrong, two sessions get the same one", name)
	}
}

func TestFreeNameRefusesEmptyBase(t *testing.T) {
	if _, err := freeName("  ", nil); err == nil {
		t.Error("an empty name was accepted: the session comes up nameless and cannot be closed")
	}
}

func fakeTmuxLauncher(t *testing.T, proc, name string, agent, parent int) string {
	t.Helper()
	return fakeTmux(t, proc, name, agent, parent, "")
}

func fakeTmux(t *testing.T, proc, name string, agent, parent int, setOption string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "arguments")
	script := filepath.Join(dir, "tmux")
	agentDir := filepath.Join(proc, fmt.Sprint(agent))
	if setOption == "" {
		setOption = "  echo \"$@\" >> " + log + "\n"
	}
	body := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"new-session)\n" +
		"  echo \"$@\" > " + log + "\n" +
		"  prev=\n" +
		"  for a in \"$@\"; do\n" +
		"    if [ \"$prev\" = sh ]; then cat \"$a\" > " + envLog(log) + " 2>/dev/null; fi\n" +
		"    prev=$a\n" +
		"  done\n" +
		"  mkdir -p " + agentDir + "\n" +
		"  printf 'claude\\n' > " + agentDir + "/comm\n" +
		"  printf 'claude\\0-n\\0" + name + "\\0' > " + agentDir + "/cmdline\n" +
		"  printf 'CLAUDE_PROFILE=personal\\0' > " + agentDir + "/environ\n" +
		fmt.Sprintf("  printf '%d (claude) S %d 0 0 0\\n' > %s/stat\n", agent, parent, agentDir) +
		"  ;;\n" +
		"set-option)\n" +
		setOption +
		"  ;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, script)
	return log
}

func envLog(log string) string { return filepath.Join(filepath.Dir(log), "environment") }

func waitFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil && len(raw) > 0 {
			return string(raw)
		}
		if !time.Now().Before(deadline) {
			if err == nil {
				err = fmt.Errorf("%s is empty", path)
			}
			t.Fatalf("the window was not opened: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitWindows(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		windowsInFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the window did not finish within 5s — directory cleanup races it")
	}
}

func fakeWindow(t *testing.T) (string, string) {
	t.Helper()
	t.Cleanup(func() { waitWindows(t) })
	dir := t.TempDir()
	log := filepath.Join(dir, "arguments")
	pidFile := filepath.Join(dir, "pid")
	script := filepath.Join(dir, "konsole")
	body := "#!/bin/sh\n" +
		"echo $$ > " + pidFile + "\n" +
		"{ echo \"$@\"; env; } > " + log + ".tmp && mv -f " + log + ".tmp " + log + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(terminalEnv, script)
	t.Setenv(terminalAutoEnv, "")
	return log, pidFile
}

func TestRunStartsSessionInTmuxAndAttachesWindow(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true", "DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}},
		fproc{pid: 100, comm: "konsole", args: []string{"konsole"}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	log, _ := fakeWindow(t)
	wrapper := machineClaude(t)
	t.Setenv("DISPLAY", ":10")

	dir := t.TempDir()
	rep, err := Run(context.Background(), Spec{
		Dir: dir, Session: "aacpanel",
		Launch: json.RawMessage(`{"room":"work","model":"opus","remoteControl":true,"env":{"FOO":"bar"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	rawTmux, err := os.ReadFile(tmuxLog)
	if err != nil {
		t.Fatalf("tmux was not called at all: %v", err)
	}
	session := string(rawTmux)
	for _, want := range []string{
		"new-session -d -s aacpanel",
		"-c " + dir,
		"env -i sh ",
		wrapper,
		"-n aacpanel",
		"--remote-control aacpanel",
		"--model opus",
		"set-option -t aacpanel mouse on",
	} {
		if !strings.Contains(session, want) {
			t.Errorf("tmux called without %q: %s", want, session)
		}
	}
	if strings.Contains(session, "set-option -g") {
		t.Errorf("the mouse was enabled globally — someone else's tmux must not be touched: %s", session)
	}
	if got := string(readFile(t, envLog(tmuxLog))); !strings.Contains(got, "'FOO=bar'") {
		t.Errorf("the variable from the map did not reach the session: %s", got)
	}
	if strings.Contains(session, "FOO=bar") {
		t.Errorf("the variable value went into the tmux arguments — the server keeps them and hands them to anyone: %s", session)
	}

	args := waitFile(t, log)
	for _, want := range []string{
		"--separate",
		"--workdir " + dir,
		"attach -t aacpanel",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("window opened without %q: %s", want, args)
		}
	}

	if rep.Session != "aacpanel" || rep.Agent != 200 || rep.Konsole == 0 {
		t.Errorf("report %+v — the wrong session was found", rep)
	}
	if len(rep.Leaks) != 0 || len(rep.Warnings) != 0 {
		t.Errorf("an honest start drew complaints: leaks %v, warnings %v", rep.Leaks, rep.Warnings)
	}
}

func TestRunWarnsWhenMouseWillNotSwitchOn(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 100, comm: "konsole", args: []string{"konsole"}})
	fakeTmux(t, proc, "aacpanel", 200, 4242, "  echo 'invalid option: mouse' >&2; exit 1\n")
	fakeWindow(t)
	machineClaude(t)
	t.Setenv("DISPLAY", ":10")

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil {
		t.Fatalf("a mouse refusal brought down a start whose session did come up: %v", err)
	}
	if rep.Session != "aacpanel" || rep.Agent != 200 {
		t.Errorf("report %+v — the wrong session was found", rep)
	}
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "mouse support") && strings.Contains(w, "invalid option: mouse") {
			found = true
		}
	}
	if !found {
		t.Errorf("nothing said about the mouse, warnings: %v", rep.Warnings)
	}
}

func TestRunFailsWhenAgentNeverAppears(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 100, comm: "konsole", args: []string{"konsole"}})
	fakeTmuxLauncher(t, filepath.Join(proc, "no-such-dir"), "aacpanel", 200, 4242)
	fakeWindow(t)
	machineClaude(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Run(ctx, Spec{Dir: t.TempDir(), Session: "aacpanel"}); err == nil {
		t.Fatal("a session without an agent passed for one that came up")
	}
}

func TestRunRefusesRelativeDir(t *testing.T) {
	fakeProc(t)
	if _, err := Run(context.Background(), Spec{Dir: "aacpanel", Session: "aacpanel"}); err == nil {
		t.Error("a relative path was accepted")
	}
}

func TestOpenListsAgentsNotWindows(t *testing.T) {
	fakeProc(t,
		fproc{pid: 100, comm: "konsole", args: []string{
			"konsole", "--workdir", "/srv/proj/aacpanel", "-e", "/bin/claude", "-n", "phantom",
		}},
		fproc{pid: 200, comm: "claude", ppid: 100, args: []string{"claude", "-n", "aacpanel"}},
		fproc{pid: 300, comm: "claude", args: []string{"claude", "-p", "-n", "tmp-8b"}},
	)
	list := Open()
	if len(list) != 1 {
		t.Fatalf("%d sessions in the list, expected one: %+v", len(list), list)
	}
	got := list[0]
	if got.Session != "aacpanel" || got.Agent != 200 || got.Konsole != 100 {
		t.Errorf("the wrong session was found: %+v", got)
	}
}

func TestCheckResumeWantsTranscriptOfThisProject(t *testing.T) {
	const uuid = "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"
	home := t.TempDir()
	contourDirs(t, home)

	dir := "/srv/proj/Beta/service/aacpanel"
	slug := "-srv-proj-Beta-service-aacpanel"
	if err := os.MkdirAll(filepath.Join(home, "projects", slug), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "projects", slug, uuid+".jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := checkResume(dir, uuid); err != nil {
		t.Errorf("our own conversation was rejected: %v", err)
	}
	if err := checkResume("/srv/proj/other", uuid); err == nil {
		t.Error("a conversation of another directory was accepted — the console comes up and dies")
	}
	if err := checkResume(dir, "--dangerously-skip-permissions"); err == nil {
		t.Error("a flag slipped through disguised as a conversation id")
	}
	if err := checkResume(dir, ""); err != nil {
		t.Errorf("a new session was rejected: %v", err)
	}
}

func TestCheckResumeStaysSilentWithoutProjectsDir(t *testing.T) {
	contourDirs(t, t.TempDir())
	if err := checkResume("/srv/proj/aacpanel", "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1"); err != nil {
		t.Errorf("refused while not a single conversations directory exists: %v", err)
	}
}

func TestRunRefusesForeignConversationBeforeOpeningWindow(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 100, comm: "konsole", args: []string{"konsole"}})
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	log, _ := fakeWindow(t)
	machineClaude(t)

	home := t.TempDir()
	contourDirs(t, home)
	if err := os.MkdirAll(filepath.Join(home, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), Spec{
		Dir: t.TempDir(), Session: "aacpanel",
		Resume: "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1",
	})
	if err == nil {
		t.Fatal("a foreign conversation was accepted")
	}
	if _, err := os.ReadFile(tmuxLog); err == nil {
		t.Error("the tmux session was started after all: the refusal came after the conversation had started")
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("the window was opened after all: the refusal came after the terminal had started")
	}
}

func TestWindowArgvFollowsTerminalSpec(t *testing.T) {
	attach := []string{"/usr/bin/tmux", "attach", "-t", "aacpanel"}

	cases := []struct {
		name    string
		spec    string
		want    []string
		konsole bool
	}{
		{
			name:    "konsole is called with its own flags",
			spec:    "konsole",
			konsole: true,
			want: []string{
				"konsole", "--separate", "--workdir", "/srv/proj",
				"-p", "LocalTabTitleFormat=aacpanel", "-p", "RemoteTabTitleFormat=aacpanel",
				"-e", "/usr/bin/tmux", "attach", "-t", "aacpanel",
			},
		},
		{
			name: "gnome-terminal gets the command as arguments",
			spec: "gnome-terminal --",
			want: []string{"gnome-terminal", "--", "/usr/bin/tmux", "attach", "-t", "aacpanel"},
		},
		{
			name: "xfce4-terminal gets the command as a string in place of %s",
			spec: "xfce4-terminal -e %s",
			want: []string{"xfce4-terminal", "-e", "/usr/bin/tmux attach -t aacpanel"},
		},
		{
			name: "an empty template opens no window",
			spec: "   ",
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, konsole := windowArgv(c.spec, "/srv/proj", "aacpanel", attach)
			if len(got) != len(c.want) {
				t.Fatalf("argv %q, want %q", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("argument %d = %q, want %q", i, got[i], c.want[i])
				}
			}
			if konsole != c.konsole {
				t.Errorf("konsole=%v, want %v: only konsole is called with its own flags", konsole, c.konsole)
			}
		})
	}
}

func TestChildEnvCarriesTerminalBasics(t *testing.T) {
	fakeProc(t)

	env, _ := childEnv([]string{"HOME=/home/u", "PATH=/usr/bin"}, Params{}, ":10", "", "")
	joined := strings.Join(env, " ")
	for _, want := range []string{"TERM=", "LANG="} {
		if !strings.Contains(joined, want) {
			t.Errorf("the session environment has no %s: %v", want, env)
		}
	}

	env, _ = childEnv([]string{"HOME=/home/u", "TERM=alacritty", "LANG=en_US.UTF-8"}, Params{}, ":10", "", "")
	joined = strings.Join(env, " ")
	if !strings.Contains(joined, "TERM=alacritty") || !strings.Contains(joined, "LANG=en_US.UTF-8") {
		t.Errorf("the caller's own TERM/LANG were overwritten by the defaults: %v", env)
	}

	env, _ = childEnv([]string{"HOME=/home/u"}, Params{}, ":10", "de_DE.UTF-8", "")
	if joined = strings.Join(env, " "); !strings.Contains(joined, "LANG=de_DE.UTF-8") {
		t.Errorf("the locale from the machine description did not arrive: %v", env)
	}
	env, _ = childEnv([]string{"HOME=/home/u"}, Params{}, ":10", "", "")
	if joined = strings.Join(env, " "); !strings.Contains(joined, "LANG="+defaultLang) {
		t.Errorf("without a machine description the fallback did not fire: %v", env)
	}
}

func TestRunTakesClaudeFromContourMap(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true", "DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	fromMachine := machineClaude(t)

	wrapper := filepath.Join(t.TempDir(), "profile-claude")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel", ClaudeBin: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	session := string(readFile(t, tmuxLog))
	if !strings.Contains(session, wrapper+" -n aacpanel") {
		t.Errorf("the session was not started by the profile wrapper: %s", session)
	}
	if strings.Contains(session, fromMachine+" -n aacpanel") {
		t.Errorf("the machine variable beat the map — the profile no longer decides what starts it: %s", session)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("a start through the profile wrapper drew complaints: %v", rep.Warnings)
	}
}

func TestRunRefusesMissingContourClaude(t *testing.T) {
	fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	t.Setenv("DISPLAY", ":10")
	machineClaude(t)

	missing := filepath.Join(t.TempDir(), "no-such-wrapper")
	_, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel", ClaudeBin: missing})
	if err == nil {
		t.Fatal("the session was started past the profile wrapper — that is a silent move into the personal account")
	}
	for _, want := range []string{missing, "profile"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, and that is the whole explanation: %v", want, err)
		}
	}
}

func pathClaude(t *testing.T) string {
	t.Helper()
	t.Setenv(claudeEnv, "")
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func TestRunWarnsWhenClaudeComesFromPATH(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true"}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	bin := pathClaude(t)
	contourDirs(t, t.TempDir(), t.TempDir())

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readFile(t, tmuxLog)), bin+" -n aacpanel") {
		t.Errorf("without a wrapper the session must come up with the claude found in PATH — otherwise this test checks the wrong thing")
	}
	var said bool
	for _, w := range rep.Warnings {
		if strings.Contains(w, "personal account") {
			said = true
		}
	}
	if !said {
		t.Errorf("a start from PATH is not named in the report: %v — from outside it is indistinguishable from an honest one", rep.Warnings)
	}
}

func TestRunStaysSilentAboutPATHWithOneContour(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true"}},
	)
	fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	pathClaude(t)
	contourDirs(t, t.TempDir())

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "personal account") {
			t.Errorf("an install with a single account got a scolding about profiles: %v", rep.Warnings)
		}
	}
}

func TestRunRefusesWhenClaudeIsNowhereInPATH(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true"}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	t.Setenv(claudeEnv, "")
	t.Setenv("HOME", t.TempDir())
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	contourDirs(t, t.TempDir())

	start := time.Now()
	_, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err == nil {
		t.Fatal("the session came up with not a single claude on the host")
	}
	if !strings.Contains(err.Error(), empty) {
		t.Errorf("the refusal does not name the PATH that was searched: %v", err)
	}
	if _, err := os.ReadFile(tmuxLog); err == nil {
		t.Error("the tmux session was started after all: the refusal came after the start")
	}
	if took := time.Since(start); took > startWait/2 {
		t.Errorf("the refusal took %s — so it waited for the agent to appear instead of looking for a file", took)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file %s was not read: %v", path, err)
	}
	return raw
}

func TestTerminalAutoDefaultsToOpening(t *testing.T) {
	cases := []struct {
		name  string
		set   bool
		value string
		want  bool
	}{
		{name: "no key at all — the window opens", want: true},
		{name: "an empty value opens too: it is not an answer", set: true, want: true},
		{name: "zero switches it off", set: true, value: "0"},
		{name: "no switches it off", set: true, value: "no"},
		{name: "off switches it off", set: true, value: "off"},
		{name: "false switches it off", set: true, value: "FALSE"},
		{name: "spaces around the answer do not get in the way", set: true, value: " 0 "},
		{name: "one opens", set: true, value: "1", want: true},
		{name: "an unknown word opens: only what is said out loud switches it off",
			set: true, value: "maybe", want: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Unsetenv(terminalAutoEnv)
			if c.set {
				t.Setenv(terminalAutoEnv, c.value)
			}
			if got := terminalAuto(); got != c.want {
				t.Errorf("terminalAuto() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTerminalAutoOffLeavesSessionWithoutWindow(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	windowLog, _ := fakeWindow(t)
	machineClaude(t)
	t.Setenv("DISPLAY", ":10")
	t.Setenv(terminalAutoEnv, "0")

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(tmuxLog); err != nil {
		t.Fatalf("the session did not come up: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.ReadFile(windowLog); err == nil {
		t.Error("the window was opened even though autostart is off")
	}
	if len(rep.Warnings) > 0 {
		t.Errorf("the report started talking about the window that was switched off: %v", rep.Warnings)
	}
}

func TestRunSurvivesWindowThatWillNotOpen(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	machineClaude(t)
	t.Setenv("DISPLAY", ":10")
	t.Setenv(terminalEnv, filepath.Join(t.TempDir(), "no-terminal"))

	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"})
	if err != nil {
		t.Fatalf("the start failed because of the window: %v", err)
	}
	if _, err := os.ReadFile(tmuxLog); err != nil {
		t.Fatalf("the session did not come up: %v", err)
	}
	if rep.Session != "aacpanel" || rep.Agent != 200 {
		t.Fatalf("report %+v — the wrong session was found", rep)
	}
	said := strings.Join(rep.Warnings, " ")
	if !strings.Contains(said, "window did not open") {
		t.Errorf("not a word in the report about the window failing: %v", rep.Warnings)
	}
	if !strings.Contains(said, "session itself did start") {
		t.Errorf("the warning does not say the session is alive: %v", rep.Warnings)
	}
}

func TestWindowOpensSameWayAsLaunch(t *testing.T) {
	fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	log, _ := fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	t.Setenv(tmuxEnv, "/usr/bin/tmux")

	dir := t.TempDir()
	rep, err := Window(WindowSpec{Dir: dir, Session: "aacpanel"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Session != "aacpanel" || rep.Konsole == 0 {
		t.Errorf("report %+v — the window is not named", rep)
	}

	args := waitFile(t, log)
	for _, want := range []string{
		"--separate",
		"--workdir " + dir,
		"attach -t aacpanel",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("window opened without %q: %s", want, args)
		}
	}
}

func TestWindowIgnoresAutostartSwitch(t *testing.T) {
	fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	log, _ := fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	t.Setenv(terminalAutoEnv, "0")

	if _, err := Window(WindowSpec{Dir: t.TempDir(), Session: "aacpanel"}); err != nil {
		t.Fatal(err)
	}
	waitFile(t, log)
}

func TestWindowRefusesWithoutTerminal(t *testing.T) {
	fakeProc(t)
	t.Setenv(terminalEnv, "")

	if WindowPossible() {
		t.Error("WindowPossible() with an empty template — the panel shows a button with nothing behind it")
	}
	_, err := Window(WindowSpec{Dir: t.TempDir(), Session: "aacpanel"})
	if err == nil {
		t.Fatal("an empty template was accepted: the panel reported a window that does not exist")
	}
	if !strings.Contains(err.Error(), terminalEnv) {
		t.Errorf("the refusal does not name what is missing: %v", err)
	}
}

func TestWindowChecksItsSpec(t *testing.T) {
	fakeProc(t)
	cases := []struct {
		name string
		spec WindowSpec
	}{
		{name: "no session name", spec: WindowSpec{Dir: "/srv/proj"}},
		{name: "directory is not absolute", spec: WindowSpec{Dir: "proj", Session: "aacpanel"}},
		{name: "no directory at all", spec: WindowSpec{Session: "aacpanel"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Window(c.spec); err == nil {
				t.Error("the spec was accepted")
			}
		})
	}
}

func TestIntentRidesAsPositionalPromptBeforeArgs(t *testing.T) {
	cases := []struct {
		name   string
		launch string
		resume string
		want   []string
	}{
		{
			name:   "intent of a new session",
			launch: `{"intent":"/rs"}`,
			want:   []string{"-n", "home", "/rs"},
		},
		{
			name:   "intent when resuming",
			launch: `{"intent":"let's continue"}`,
			resume: "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1",
			want: []string{"-n", "home",
				"--resume", "e29e01f1-748c-4a99-9fd6-e3d8827ed5d1", "let's continue"},
		},
		{
			name:   "before the arguments from the map",
			launch: `{"intent":"hello","args":["--add-dir","/opt/x"]}`,
			want: []string{"-n", "home",
				"hello", "--add-dir", "/opt/x"},
		},
		{
			name:   "an empty intent is not passed",
			launch: `{"intent":""}`,
			want:   []string{"-n", "home"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, warns := parseParams(json.RawMessage(c.launch))
			if len(warns) != 0 {
				t.Fatalf("the intent drew complaints from the launcher: %v", warns)
			}
			got := claudeArgs("home", c.resume, p)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("claude arguments:\n got %v\n want %v", got, c.want)
			}
		})
	}
}

func TestRunSendsIntentAndSaysSo(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true"}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	machineClaude(t)
	t.Setenv("DISPLAY", ":10")

	rep, err := Run(context.Background(), Spec{
		Dir: t.TempDir(), Session: "aacpanel",
		Launch: json.RawMessage(`{"intent":"read the queue and take the first task"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if said := string(readFile(t, tmuxLog)); !strings.Contains(said, "read the queue and take the first task") {
		t.Errorf("the intent did not reach the session command: %s", said)
	}
	if rep.Intent != "read the queue and take the first task" {
		t.Errorf("the report says nothing about the intent: %q", rep.Intent)
	}
}

func TestIntentStaysHomeWhenSessionLeavesItsContour(t *testing.T) {
	proc := fakeProc(t,
		fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "KDE_FULL_SESSION=true"}},
	)
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	pathClaude(t)
	contourDirs(t, t.TempDir(), t.TempDir())

	rep, err := Run(context.Background(), Spec{
		Dir: t.TempDir(), Session: "aacpanel",
		Launch: json.RawMessage(`{"intent":"run the checks"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if said := string(readFile(t, tmuxLog)); strings.Contains(said, "run the checks") {
		t.Errorf("the intent went to a session outside its profile: %s", said)
	}
	if rep.Intent != "" {
		t.Errorf("the report calls an intent sent that was never sent: %q", rep.Intent)
	}
	var said bool
	for _, w := range rep.Warnings {
		if strings.Contains(w, "opening message was not sent") {
			said = true
		}
	}
	if !said {
		t.Errorf("staying silent about the intent looks exactly like the intent being broken: %v", rep.Warnings)
	}
}
