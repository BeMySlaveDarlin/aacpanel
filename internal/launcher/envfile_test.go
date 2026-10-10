package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain gives the run a runtime directory and a home of its own. The
// launcher reads the account a session starts in — its .claude.json in
// CLAUDE_CONFIG_DIR or else in HOME, the registry of contours in the home —
// and the stream keeps its files under XDG_STATE_HOME. Against the machine's,
// a test reads what this machine's account says — claude in Chrome by default
// puts --chrome into every argv a test compares — and writes where live
// holders keep theirs. make test substitutes the home too, but a run of this
// package alone does not, and neither touches CLAUDE_CONFIG_DIR a session of a
// contour carries.
//
// The runtime directory is the run's directory itself: a socket path under it
// is near the 108 bytes a unix socket takes, and one level more puts it over.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aacpanel-launcher-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "the directory for this run was not created:", err)
		os.Exit(1)
	}
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "the home of this run was not created:", err)
		os.Exit(1)
	}
	os.Setenv("XDG_RUNTIME_DIR", dir)
	os.Setenv("HOME", home)
	for name, sub := range map[string]string{
		"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share",
		"XDG_STATE_HOME": ".local/state", "XDG_CACHE_HOME": ".cache",
	} {
		os.Setenv(name, filepath.Join(home, sub))
	}
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestSessionEnvArrivesThroughFileNotArgv(t *testing.T) {
	work := t.TempDir()
	vars := []string{
		"HOME=" + work,
		"TERM=xterm-256color",
		"CLAUDE_PROFILE=acme",
		"CLAUDE_CONFIG_DIR=" + work + "/.claude-contours/acme",
		"GITLAB_TOKEN=gl'itch $(id) `whoami` \"quotes\" \\ end",
		"MULTI=first\nsecond",
		"EMPTY=",
	}

	drop, warns, err := writeSessionEnv(vars)
	if err != nil {
		t.Fatal(err)
	}
	defer drop.remove()
	if len(warns) != 0 {
		t.Errorf("an honest environment drew complaints: %v", warns)
	}

	envOut := filepath.Join(work, "environment")
	argvOut := filepath.Join(work, "arguments")
	stub := filepath.Join(work, "claude")
	body := "#!/bin/sh\nenv -0 > " + envOut + "\nprintf '%s\\0' \"$@\" > " + argvOut + "\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("env", "-i", "sh", drop.path, stub, "-n", "probe", "--model", "opus")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the start chain did not work: %v: %s", err, out)
	}

	got := map[string]string{}
	for _, kv := range strings.Split(strings.TrimSuffix(string(readFile(t, envOut)), "\x00"), "\x00") {
		name, value, ok := strings.Cut(kv, "=")
		if ok {
			got[name] = value
		}
	}
	for _, kv := range vars {
		name, value, _ := strings.Cut(kv, "=")
		if have, ok := got[name]; !ok {
			t.Errorf("variable %s did not reach the session at all", name)
		} else if have != value {
			t.Errorf("variable %s arrived as %q, while it was set to %q", name, have, value)
		}
	}

	argv := strings.Split(strings.TrimSuffix(string(readFile(t, argvOut)), "\x00"), "\x00")
	want := []string{"-n", "probe", "--model", "opus"}
	if len(argv) != len(want) {
		t.Fatalf("claude got the arguments %q, want %q", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Errorf("argument %d = %q, want %q", i, argv[i], want[i])
		}
	}

	if _, err := os.Stat(drop.path); err == nil {
		t.Error("the environment file was left behind — any process of the owner can read it")
	}
}

func TestSessionEnvFileIsPrivate(t *testing.T) {
	drop, _, err := writeSessionEnv([]string{"FOO=bar"})
	if err != nil {
		t.Fatal(err)
	}
	defer drop.remove()

	file, err := os.Stat(drop.path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := file.Mode().Perm(); mode != 0o600 {
		t.Errorf("the environment file is %v, want 0600", mode)
	}
	dir, err := os.Stat(drop.dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := dir.Mode().Perm(); mode != 0o700 {
		t.Errorf("the environment directory is %v, want 0700", mode)
	}
}

// The executor tells a tmux session the launcher started by the command tmux
// keeps for its pane, and kills only such a session with its claude: the
// command the launcher starts a pane with is known for one, and nothing a
// person starts by hand is.
func TestLaunchedKnowsThePaneOfALaunchAndNothingElse(t *testing.T) {
	drop, _, err := writeSessionEnv([]string{"FOO=bar"})
	if err != nil {
		t.Fatal(err)
	}
	defer drop.remove()

	var words []string
	for _, w := range sessionCommand(drop.path, "/usr/bin/claude", []string{"-n", "probe", "--mcp-config", `{"a": 1}`}) {
		if strings.ContainsAny(w, ` "`) {
			w = `"` + strings.ReplaceAll(w, `"`, `\"`) + `"`
		}
		words = append(words, w)
	}
	if kept := strings.Join(words, " "); !Launched(kept) {
		t.Errorf("the pane of a launch, kept by tmux as %q, is not known for one", kept)
	}
	for _, hand := range []string{
		"", "bash", "/bin/sh", "claude -n work",
		"env -i sh /tmp/env.sh /usr/bin/claude",
		"env -i sh /tmp/scripts/env.sh /usr/bin/claude",
		"env sh " + drop.path + " /usr/bin/claude",
		"env -i sh " + drop.path,
	} {
		if Launched(hand) {
			t.Errorf("a pane started with %q is taken for one the launcher started", hand)
		}
	}
}

func TestSessionEnvNamesTheVariableItDropped(t *testing.T) {
	drop, warns, err := writeSessionEnv([]string{"GOOD=yes", "GITLAB_TOKEN=beg\x00in"})
	if err != nil {
		t.Fatal(err)
	}
	defer drop.remove()

	if len(warns) != 1 || !strings.Contains(warns[0], "GITLAB_TOKEN") {
		t.Errorf("complaints %q — the variable was dropped silently, and that is found out inside another account", warns)
	}
	body := string(readFile(t, drop.path))
	if strings.Contains(body, "beg") {
		t.Errorf("the truncated value reached the session anyway: %s", body)
	}
	if !strings.Contains(body, "'GOOD=yes'") {
		t.Errorf("a neighbour in the list vanished together with the dropped variable: %s", body)
	}
}

func TestSessionEnvNeverGoesThroughTmuxArgv(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	machineClaude(t)

	const secret = "s3cr3t-from-the-map"
	if _, err := Run(context.Background(), Spec{
		Dir: t.TempDir(), Session: "aacpanel",
		Launch: json.RawMessage(`{"env":{"GITLAB_TOKEN":"` + secret + `"}}`),
	}); err != nil {
		t.Fatal(err)
	}

	if said := string(readFile(t, tmuxLog)); strings.Contains(said, secret) {
		t.Errorf("the secret from the map went into the tmux arguments — the server keeps them and shows them to anyone: %s", said)
	}
	if said := string(readFile(t, envLog(tmuxLog))); !strings.Contains(said, secret) {
		t.Errorf("the secret was hidden at the price of never reaching the session: %s", said)
	}
}

func TestRunTakesTheEnvFileAway(t *testing.T) {
	proc := fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10"}})
	tmuxLog := fakeTmuxLauncher(t, proc, "aacpanel", 200, 4242)
	fakeWindow(t)
	t.Setenv("DISPLAY", ":10")
	machineClaude(t)

	if _, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "aacpanel"}); err != nil {
		t.Fatal(err)
	}

	path := envFileFromArgv(t, string(readFile(t, tmuxLog)))
	if _, err := os.Stat(path); err == nil {
		t.Errorf("the session environment was left in %s — the same leak, only on disk", path)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Errorf("directory %s was left behind — in a month there will be one per start", filepath.Dir(path))
	}
}

func envFileFromArgv(t *testing.T, argv string) string {
	t.Helper()
	fields := strings.Fields(argv)
	for i, f := range fields {
		if f == "sh" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	t.Fatalf("the tmux arguments carry no environment file: %s", argv)
	return ""
}
