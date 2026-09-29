// Package stand holds the tests of the stand's scripts: they run on a
// substituted root, a substituted tmux and substituted docker, never on the
// machine the tests run on.
package stand

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type result struct {
	out, err string
	code     int
}

// run runs a script of this directory with exactly env, stdin closed.
func run(t *testing.T, env []string, stdin *os.File, script string, args ...string) result {
	t.Helper()
	cmd := exec.Command("./"+script, args...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("%s %q did not end in a minute:\n%s%s", script, args, out.String(), errOut.String())
	}
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{out.String(), errOut.String(), code}
}

// stubs puts programs with the given bodies into a directory of their own
// and gives the PATH that finds them first.
func stubs(t *testing.T, bodies map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range bodies {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir + ":" + os.Getenv("PATH")
}

// put writes a file under root with its mode set whatever the umask.
func put(t *testing.T, root, path, body string, mode os.FileMode) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(full, mode); err != nil {
		t.Fatal(err)
	}
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// The docker of the tests answers as a machine with the panel on it; with
// FAKE_DOCKER_DENY it refuses whoever is not root, and sudo -n makes root of
// the caller only with FAKE_SUDO_OK.
const (
	fakeDocker = `#!/bin/sh
if [ -n "$FAKE_DOCKER_DENY" ] && [ -z "$FAKE_SUDO" ]; then
	echo "permission denied while trying to connect to the Docker daemon socket" >&2
	exit 1
fi
case "$1 $2" in
"info "*) exit 0 ;;
"ps -a") printf 'aacpanel\taacpanel-aacpanel c974\n' ;;
"ps -aq") printf 'c974\n' ;;
"volume ls") printf 'aacpanel_aacpanel-db\tlocal\n' ;;
"network ls") printf 'bridge\tn1\n' ;;
"image ls") printf 'postgres:18-alpine\tsha256:abc\n' ;;
"inspect --format") printf '/aacpanel\t2026-01-02T03:04:05Z\n' ;;
*) echo "docker of the test: $*" >&2; exit 1 ;;
esac
`
	fakeSudo = `#!/bin/sh
[ "$1" = -n ] && shift
[ -n "$FAKE_SUDO_OK" ] || { echo "sudo: a password is required" >&2; exit 1; }
FAKE_SUDO=1 exec "$@"
`
	fakeGetent = `#!/bin/sh
[ "$1 $2" = "group docker" ] && echo "docker:x:998:dev,alice"
`
)

// machine lays out a machine with the panel installed under a new root,
// with the account ~/.claude-work beside ~/.claude.
func machine(t *testing.T) (root, clone string) {
	t.Helper()
	root = t.TempDir()
	clone, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if clone, err = filepath.EvalSymlinks(clone); err != nil {
		t.Fatal(err)
	}
	home := "/home/dev"
	put(t, root, "/etc/systemd/system/aacpanel-agent@.service", "[Unit]\n", 0o644)
	if err := os.MkdirAll(filepath.Join(root, "/etc/systemd/system/multi-user.target.wants"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/systemd/system/aacpanel-agent@.service",
		filepath.Join(root, "/etc/systemd/system/multi-user.target.wants/aacpanel-agent@dev.service")); err != nil {
		t.Fatal(err)
	}
	put(t, root, home+"/.config/systemd/user/aacpanel-exec.service", "[Service]\n", 0o644)
	put(t, root, home+"/bin/aacpanel-exec", "executor", 0o755)
	put(t, root, home+"/.claude/settings.json", `{"hooks":{}}`, 0o600)
	put(t, root, home+"/.claude-work/settings.json", `{}`, 0o600)
	put(t, root, home+"/.claude-work/.claude.json",
		`{"numStartups":3,"mcpServers":{"aacpanel":{"type":"stdio","command":"/home/dev/bin/aacpanel-exec","args":["-mcp"],"env":{}}}}`, 0o600)
	put(t, root, home+"/.claude.json", `{"numStartups":7}`, 0o600)
	put(t, root, "/var/lib/systemd/linger/dev", "", 0o644)
	put(t, root, "/var/lib/aacpanel/host.env", "AACP_REPO=/home/dev/aacpanel\n", 0o644)
	put(t, root, home+"/.local/state/aacpanel-install/manifest.tsv", "S4\tlinger\tdev\tby-installer\n", 0o600)
	put(t, root, clone+"/.env", "AACP_SECRET=0\n", 0o600)
	put(t, root, "/var/lib/dpkg/status", `Package: tmux
Status: install ok installed
Architecture: amd64
Version: 3.4-1

Package: jq
Status: deinstall ok config-files
Architecture: amd64
Version: 1.7.1-3

`, 0o644)
	return root, clone
}

func tracesEnv(root, path string, extra ...string) []string {
	return append([]string{"PATH=" + path, "HOME=/home/dev", "AACP_TRACES_ROOT=" + root, "LC_ALL=C"}, extra...)
}

// TestTracesListAMachineUnderARoot: every trace of the list is a line with
// the path as it is on the machine, files with their sha256, and the list
// comes sorted and the same twice.
func TestTracesListAMachineUnderARoot(t *testing.T) {
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		t.Skip("no dpkg-query: the package list is read with the real one")
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root, clone := machine(t)
	path := stubs(t, map[string]string{"docker": fakeDocker, "sudo": fakeSudo, "getent": fakeGetent})
	r := run(t, tracesEnv(root, path), nil, "traces.sh")
	if r.code != 0 {
		t.Fatalf("traces.sh ended with %d:\n%s%s", r.code, r.out, r.err)
	}
	lines := strings.Split(strings.TrimSuffix(r.out, "\n"), "\n")
	if !slices.IsSorted(lines) {
		t.Errorf("the traces are not sorted:\n%s", r.out)
	}
	if strings.Contains(r.out, root) {
		t.Errorf("a path keeps the root it was read under:\n%s", r.out)
	}
	server := `{"args":["-mcp"],"command":"/home/dev/bin/aacpanel-exec","env":{},"type":"stdio"}`
	for _, want := range []string{
		"sysunit\t/etc/systemd/system/aacpanel-agent@.service\tfile 644 " + me.Username + " " + sum("[Unit]\n"),
		"sysunit\t/etc/systemd/system/multi-user.target.wants/aacpanel-agent@dev.service\tlink /etc/systemd/system/aacpanel-agent@.service",
		"sysunit\t/etc/systemd/system/multi-user.target.wants\tdir 755 " + me.Username,
		"userunit\t/home/dev/.config/systemd/user/aacpanel-exec.service\tfile 644 " + me.Username + " " + sum("[Service]\n"),
		"bin\t/home/dev/bin/aacpanel-exec\tfile 755 " + me.Username + " " + sum("executor"),
		"account\t/home/dev/.claude/settings.json\tfile 600 " + me.Username + " " + sum(`{"hooks":{}}`),
		"account\t/home/dev/.claude-work/settings.json\tfile 600 " + me.Username + " " + sum(`{}`),
		"mcp\t/home/dev/.claude-work/.claude.json aacpanel\t" + sum(server),
		"linger\t/var/lib/systemd/linger/dev\tfile 644 " + me.Username + " " + sum(""),
		"state\t/var/lib/aacpanel/host.env\tfile 644 " + me.Username + " " + sum("AACP_REPO=/home/dev/aacpanel\n"),
		"userdata\t/home/dev/.local/state/aacpanel-install/manifest.tsv\tfile 600 " + me.Username + " " + sum("S4\tlinger\tdev\tby-installer\n"),
		"clone\t" + clone + "/.env\tfile 600 " + me.Username + " " + sum("AACP_SECRET=0\n"),
		"docker-container\taacpanel\taacpanel-aacpanel c974",
		"docker-volume\taacpanel_aacpanel-db\tlocal",
		"docker-network\tbridge\tn1",
		"docker-image\tpostgres:18-alpine\tsha256:abc",
		"pkg\ttmux\t3.4-1",
		"group\tdocker\tgid 998",
		"group\tdocker\tmember dev",
		"group\tdocker\tmember alice",
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("no line %q in:\n%s", want, r.out)
		}
	}
	for _, not := range []string{"pkg\tjq", "/home/dev/.claude.json"} {
		if strings.Contains(r.out, not) {
			t.Errorf("%q is listed, and it should not be: a package removed, a .claude.json with no server:\n%s", not, r.out)
		}
	}
	if again := run(t, tracesEnv(root, path), nil, "traces.sh"); again.out != r.out {
		t.Errorf("a second list differs from the first:\n%s\nagainst\n%s", again.out, r.out)
	}
}

// TestTracesAskDockerThroughSudoOrSayItIsUnreachable: before an install the
// user is often outside the docker group.
func TestTracesAskDockerThroughSudoOrSayItIsUnreachable(t *testing.T) {
	root, _ := machine(t)
	path := stubs(t, map[string]string{"docker": fakeDocker, "sudo": fakeSudo, "getent": fakeGetent})

	r := run(t, tracesEnv(root, path, "FAKE_DOCKER_DENY=1", "FAKE_SUDO_OK=1"), nil, "traces.sh")
	if r.code != 0 || !strings.Contains(r.out, "docker-container\taacpanel\t") {
		t.Errorf("docker that lets root in is not listed through sudo -n (%d):\n%s%s", r.code, r.out, r.err)
	}
	r = run(t, tracesEnv(root, path, "FAKE_DOCKER_DENY=1"), nil, "traces.sh")
	if r.code != 0 || !strings.Contains(r.out, "docker\t-\tunreachable\n") || strings.Contains(r.out, "docker-container") {
		t.Errorf("docker that lets nobody in is not named unreachable (%d):\n%s%s", r.code, r.out, r.err)
	}
}

// TestTracesCompareFailsOnlyOnWhatUninstallPromises: packages and the docker
// group stay by design, docker that did not answer before cannot be compared,
// and every other difference is a trace left behind.
func TestTracesCompareFailsOnlyOnWhatUninstallPromises(t *testing.T) {
	before := "bin\t/home/dev/bin\tdir 755 dev\n" +
		"docker-network\tbridge\tn1\n" +
		"group\tdocker\tgid 998\n" +
		"pkg\tjq\t1.7.1-3\n"
	cases := []struct {
		name, before, after string
		code                int
		says                []string
	}{
		{"the same", before, before, 0, []string{"put back everything"}},
		{"a package and a member more", before,
			before + "pkg\ttmux\t3.4-1\n" + "group\tdocker\tmember dev\n", 0,
			[]string{"left on purpose", "  + pkg\ttmux\t3.4-1", "  + group\tdocker\tmember dev"}},
		{"an empty directory left", before,
			before + "userunit\t/home/dev/.config/systemd/user/default.target.wants\tdir 755 dev\n", 1,
			[]string{"+ userunit\t/home/dev/.config/systemd/user/default.target.wants\tdir 755 dev", "1 line(s) differ"}},
		{"a foreign file changed", before + "account\t/home/dev/.claude/settings.json\tfile 600 dev aaa\n",
			before + "account\t/home/dev/.claude/settings.json\tfile 600 dev bbb\n", 1,
			[]string{"- account\t/home/dev/.claude/settings.json\tfile 600 dev aaa", "+ account\t/home/dev/.claude/settings.json\tfile 600 dev bbb", "2 line(s) differ"}},
		{"docker installed by the installer", "docker\t-\tabsent\n",
			"docker-network\tbridge\tn1\n" + "pkg\tdocker.io\t26.1.5\n", 0,
			[]string{"not compared", "  + docker-network\tbridge\tn1", "left on purpose"}},
		{"a pulled image left", before, before + "docker-image\tpostgres:18-alpine\tsha256:abc\n", 1,
			[]string{"+ docker-image\tpostgres:18-alpine\tsha256:abc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			put(t, dir, "before", c.before, 0o644)
			put(t, dir, "after", c.after, 0o644)
			r := run(t, []string{"PATH=" + os.Getenv("PATH")}, nil, "traces.sh", "compare",
				filepath.Join(dir, "before"), filepath.Join(dir, "after"))
			if r.code != c.code {
				t.Errorf("status %d, want %d:\n%s%s", r.code, c.code, r.out, r.err)
			}
			for _, s := range c.says {
				if !strings.Contains(r.out, s) {
					t.Errorf("no %q in:\n%s", s, r.out)
				}
			}
		})
	}
}

// TestTracesStartsNameEveryInvocation: the units of the panel of both
// managers and every container, and a missing user manager said aloud.
func TestTracesStartsNameEveryInvocation(t *testing.T) {
	path := stubs(t, map[string]string{"docker": fakeDocker, "sudo": fakeSudo, "systemctl": `#!/bin/sh
scope=system
if [ "$1" = --user ]; then
	scope=user
	shift
	[ -z "$FAKE_NO_MANAGER" ] || { echo "Failed to connect to bus" >&2; exit 1; }
	[ -n "$XDG_RUNTIME_DIR" ] || { echo "no XDG_RUNTIME_DIR" >&2; exit 1; }
fi
case $1 in
list-units) [ $scope = user ] && echo "aacpanel-exec.service loaded active running the executor" || echo "aacpanel-agent@dev.service loaded active running the collector" ;;
show) eval "echo inv-$scope-\${$#}" ;;
esac
`})
	r := run(t, []string{"PATH=" + path, "HOME=/home/dev"}, nil, "traces.sh", "starts")
	want := "container\t/aacpanel\t2026-01-02T03:04:05Z\n" +
		"unit\tsystem aacpanel-agent@dev.service\tinv-system-aacpanel-agent@dev.service\n" +
		"unit\tuser aacpanel-exec.service\tinv-user-aacpanel-exec.service\n"
	if r.code != 0 || r.out != want {
		t.Errorf("starts (%d):\n%s%s\nwant:\n%s", r.code, r.out, r.err, want)
	}
	r = run(t, []string{"PATH=" + path, "HOME=/home/dev", "FAKE_NO_MANAGER=1"}, nil, "traces.sh", "starts")
	if r.code != 0 || !strings.Contains(r.out, "unit\tuser\tno user manager\n") {
		t.Errorf("a missing user manager is not said (%d):\n%s%s", r.code, r.out, r.err)
	}
}

func claudeEnv(home string, extra ...string) []string {
	return append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, extra...)
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, raw)
	}
	return v
}

// TestFakeClaudeAnswersVersionWithoutWaiting: the installer asks the version
// of whatever claude it finds, with a terminal that never closes.
func TestFakeClaudeAnswersVersionWithoutWaiting(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	defer wr.Close()
	home := t.TempDir()
	for _, args := range [][]string{{"--version"}, {"-v"}} {
		r := run(t, claudeEnv(home), rd, "fake-claude.sh", args...)
		if r.code != 0 || r.out != "2.1.284 (Claude Code)\n" {
			t.Errorf("claude %s (%d): %q %q", args[0], r.code, r.out, r.err)
		}
	}
	r := run(t, claudeEnv(home), rd, "fake-claude.sh", "auth", "status")
	if r.code != 2 || !strings.Contains(r.err, "does not play claude auth") {
		t.Errorf("a command the stand-in does not play (%d): %q %q", r.code, r.out, r.err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "sessions")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a command left a session file, as if it were a session: %v", err)
	}
}

// TestFakeClaudeKeepsMCPServersAsClaudeDoes: add, get, list and remove in the
// user scope land in .claude.json of the account and leave the rest of the
// file alone, with the messages and codes of claude.
func TestFakeClaudeKeepsMCPServersAsClaudeDoes(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude-work")
	config := filepath.Join(account, ".claude.json")
	put(t, account, ".claude.json", `{"numStartups": 3, "projects": {"/x": {}}}`, 0o600)
	env := claudeEnv(home, "CLAUDE_CONFIG_DIR="+account)
	add := []string{"mcp", "add", "--scope", "user", "aacpanel", "--", "/home/dev/bin/aacpanel-exec", "-mcp"}

	r := run(t, env, nil, "fake-claude.sh", add...)
	if r.code != 0 || r.out != "Added stdio MCP server aacpanel with command: /home/dev/bin/aacpanel-exec -mcp to user config\nFile modified: "+config+"\n" {
		t.Fatalf("add (%d): %q %q", r.code, r.out, r.err)
	}
	got := readJSON(t, config)
	want := map[string]any{"type": "stdio", "command": "/home/dev/bin/aacpanel-exec", "args": []any{"-mcp"}, "env": map[string]any{}}
	if servers, _ := got["mcpServers"].(map[string]any); !jsonEqual(servers["aacpanel"], want) {
		t.Errorf("the server in .claude.json: %v", got)
	}
	if got["numStartups"] != 3.0 || got["projects"] == nil {
		t.Errorf("the rest of .claude.json is not kept: %v", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("with CLAUDE_CONFIG_DIR the server went to ~/.claude.json as well: %v", err)
	}

	r = run(t, env, nil, "fake-claude.sh", add...)
	if r.code != 1 || r.err != "MCP server aacpanel already exists in user config\n" {
		t.Errorf("add again (%d): %q %q", r.code, r.out, r.err)
	}
	r = run(t, env, nil, "fake-claude.sh", "mcp", "get", "aacpanel")
	if r.code != 0 || !strings.Contains(r.out, "  Command: /home/dev/bin/aacpanel-exec\n  Args: -mcp\n") {
		t.Errorf("get (%d): %q %q", r.code, r.out, r.err)
	}
	r = run(t, env, nil, "fake-claude.sh", "mcp", "list")
	if r.code != 0 || !strings.Contains(r.out, "aacpanel: /home/dev/bin/aacpanel-exec -mcp - ") {
		t.Errorf("list (%d): %q %q", r.code, r.out, r.err)
	}
	r = run(t, env, nil, "fake-claude.sh", "mcp", "remove", "--scope", "user", "aacpanel")
	if r.code != 0 || r.out != "Removed MCP server aacpanel from user config\nFile modified: "+config+"\n" {
		t.Errorf("remove (%d): %q %q", r.code, r.out, r.err)
	}
	got = readJSON(t, config)
	if servers, ok := got["mcpServers"].(map[string]any); !ok || len(servers) != 0 || got["numStartups"] != 3.0 {
		t.Errorf("after remove: %v", got)
	}
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{"mcp", "remove", "--scope", "user", "aacpanel"}, "No MCP server named \"aacpanel\" in user scope\n"},
		{[]string{"mcp", "remove", "aacpanel"}, "No MCP server named \"aacpanel\". Run `claude mcp add` to add one.\n"},
		{[]string{"mcp", "get", "aacpanel"}, "No MCP server named \"aacpanel\". Run `claude mcp add` to add one.\n"},
	} {
		if r := run(t, env, nil, "fake-claude.sh", c.args...); r.code != 1 || r.err != c.err {
			t.Errorf("%q (%d): %q %q", c.args, r.code, r.out, r.err)
		}
	}
	r = run(t, env, nil, "fake-claude.sh", "mcp", "list")
	if r.code != 0 || r.out != "No MCP servers configured. Use `claude mcp add` to add a server.\n" {
		t.Errorf("list of none (%d): %q %q", r.code, r.out, r.err)
	}
}

// TestFakeClaudeWithoutAConfigDirWritesTheHomeFile: the default account
// keeps its servers in ~/.claude.json, and a scope the stand-in does not
// play stops rather than passing.
func TestFakeClaudeWithoutAConfigDirWritesTheHomeFile(t *testing.T) {
	home := t.TempDir()
	env := claudeEnv(home)
	r := run(t, env, nil, "fake-claude.sh", "mcp", "add", "aacpanel", "--", "/bin/true")
	if r.code != 2 {
		t.Errorf("add in the local scope passed (%d): %q %q", r.code, r.out, r.err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused add wrote .claude.json: %v", err)
	}
	r = run(t, env, nil, "fake-claude.sh", "mcp", "add", "-s", "user", "aacpanel", "--", "/bin/true")
	if r.code != 0 {
		t.Fatalf("add (%d): %q %q", r.code, r.out, r.err)
	}
	got := readJSON(t, filepath.Join(home, ".claude.json"))
	if servers, _ := got["mcpServers"].(map[string]any); servers["aacpanel"] == nil {
		t.Errorf("~/.claude.json: %v", got)
	}
}

// TestFakeClaudeStillPlaysASession: with no command it writes the session
// file and echoes its input until the input ends.
func TestFakeClaudeStillPlaysASession(t *testing.T) {
	home := t.TempDir()
	in, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	in.WriteString("hello\n")
	in.Seek(0, 0)
	defer in.Close()
	r := run(t, claudeEnv(home), in, "fake-claude.sh", "-n", "check")
	if r.code != 0 || !strings.Contains(r.out, "received: hello\n") {
		t.Errorf("session (%d): %q %q", r.code, r.out, r.err)
	}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	if len(files) != 1 || !strings.Contains(string(must(os.ReadFile(files[0]))), `"name":"check"`) {
		t.Errorf("session files: %v", files)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// The tmux of the scenario tests plays one pane of a program described in
// $FAKE_TMUX/app: its first line is "start<TAB>screen", every next one
// "key<TAB>screen after it", and "exit N" as a screen ends the program. A key
// the program does not expect puts "unexpected key" on the screen. A new
// screen shows only on the third look at the pane, so a single look finds
// the program at work. Every key is logged with the screen it was sent on.
const fakeTmux = `#!/bin/sh
d=$FAKE_TMUX
# look is a look at the pane: the one that comes after the lag shows the
# screen a key led to.
look() {
	[ -f "$d/lag" ] || return 0
	n=$(cat "$d/lag")
	if [ "$n" -gt 1 ]; then
		echo $((n - 1)) >"$d/lag"
		return 0
	fi
	rm "$d/lag"
	next=$(cat "$d/next")
	case $next in
	"exit "*) echo "${next#exit }" >"$d/dead"; echo ended >"$d/screen" ;;
	*) echo "$next" >"$d/screen" ;;
	esac
}
while [ $# -gt 0 ]; do
	case $1 in -L | -f) shift 2 ;; *) break ;; esac
done
cmd=$1
shift
case $cmd in
new-session)
	echo "new $*" >>"$d/log"
	head -n 1 "$d/app" | cut -f 2 >"$d/screen"
	echo 1 >"$d/pos"
	: >"$d/sock"
	;;
send-keys)
	how=key
	while [ $# -gt 0 ]; do
		case $1 in -t) shift 2 ;; -l) how=type; shift ;; --) shift; break ;; *) break ;; esac
	done
	[ $# -eq 1 ] || echo "many $*" >>"$d/log"
	echo "$how $1 on $(cat "$d/screen")" >>"$d/log"
	pos=$(cat "$d/pos")
	line=$(sed -n "$((pos + 1))p" "$d/app")
	if [ "$1" = "$(printf '%s' "$line" | cut -f 1)" ]; then
		echo $((pos + 1)) >"$d/pos"
		printf '%s\n' "$line" | cut -f 2 >"$d/next"
		echo 2 >"$d/lag"
		echo working >"$d/screen"
	else
		echo "unexpected key $1" >"$d/screen"
	fi
	;;
capture-pane)
	look
	cat "$d/screen"
	;;
display-message)
	for fmt; do :; done
	case $fmt in
	*socket_path*) echo "$d/sock" ;;
	*)
		look
		if [ -f "$d/dead" ]; then echo "1 $(cat "$d/dead")"; else echo "0 "; fi
		;;
	esac
	;;
kill-server) echo kill >>"$d/log" ;;
resize-window) echo "resize $*" >>"$d/log" ;;
*) echo "unknown $cmd" >>"$d/log"; exit 1 ;;
esac
`

// play runs scenario.sh with the scenario and the program on the fake tmux,
// and gives what it said and the log of the tmux.
func play(t *testing.T, app, scenario string) (result, string, string) {
	t.Helper()
	dir := t.TempDir()
	put(t, dir, "app", app, 0o644)
	put(t, dir, "scenario", scenario, 0o644)
	out := filepath.Join(dir, "out")
	r := run(t, []string{"PATH=" + stubs(t, map[string]string{"tmux": fakeTmux}), "FAKE_TMUX=" + dir},
		nil, "scenario.sh", "-o", out, "-x", "60", filepath.Join(dir, "scenario"), "--", "aacpanel-install", "install")
	log, _ := os.ReadFile(filepath.Join(dir, "log"))
	return r, string(log), dir
}

// TestScenarioSendsEachKeyOnTheScreenItAnswers: a key goes one at a time, and
// only once the screen the scenario waits for is there — never while the
// program is still at work.
func TestScenarioSendsEachKeyOnTheScreenItAnswers(t *testing.T) {
	app := "start\tWelcome. Press Enter\n" +
		"Enter\tWhich contour? 1. personal 2. work\n" +
		"2\tName of the group?\n" +
		"grp\tName of the group? grp\n" +
		"Enter\tInstalled\n" +
		"C-c\texit 0\n"
	scenario := `# the path of the test
wait Press Enter
key Enter
wait Which contour?
  key 2
wait Name of the group?
type grp
wait Name of the group? grp
key Enter
timeout 5
wait Installed
shot done
resize 80 24
key C-c
exit 0
`
	r, log, dir := play(t, app, scenario)
	if r.code != 0 {
		t.Fatalf("scenario.sh ended with %d:\n%s%s\nthe tmux:\n%s", r.code, r.out, r.err, log)
	}
	want := "new -d -s scenario -x 60 -y 30 aacpanel-install install\n" +
		"key Enter on Welcome. Press Enter\n" +
		"key 2 on Which contour? 1. personal 2. work\n" +
		"type grp on Name of the group?\n" +
		"key Enter on Name of the group? grp\n" +
		"resize -t scenario -x 80 -y 24\n" +
		"key C-c on Installed\n" +
		"kill\n"
	if log != want {
		t.Errorf("the tmux saw:\n%s\nwant:\n%s", log, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "sock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket of the tmux server is left: %v", err)
	}
	shot, _ := os.ReadFile(filepath.Join(dir, "out", "done.txt"))
	transcript, _ := os.ReadFile(filepath.Join(dir, "out", "transcript.txt"))
	if string(shot) != "Installed\n" || !strings.Contains(string(transcript), "=== seen: Which contour?\nWhich contour? 1. personal 2. work\n=== key: 2\n") {
		t.Errorf("the shot %q and the transcript:\n%s", shot, transcript)
	}
}

// TestScenarioRefusesAKeyWithoutAWait: a blind key is a mistake of the
// scenario, found before the program starts.
func TestScenarioRefusesAKeyWithoutAWait(t *testing.T) {
	for _, scenario := range []string{
		"key Enter\n",
		"wait Press Enter\nkey Enter\nkey Down\n",
		"wait Press Enter\nkey Enter\ngone Press Enter\ntype text\n",
	} {
		r, log, _ := play(t, "start\tPress Enter\n", scenario)
		if r.code != 2 || !strings.Contains(r.err, "without a wait before it") || log != "" {
			t.Errorf("%q: status %d, %q, the tmux saw %q", scenario, r.code, r.err, log)
		}
	}
}

// TestScenarioStopsWhenTheScreenIsNotTheOneAwaited: no key after a wait
// that did not come true, and the screen that was there instead is shown.
func TestScenarioStopsWhenTheScreenIsNotTheOneAwaited(t *testing.T) {
	cases := []struct {
		name, app, scenario, says string
		keys                      int
	}{
		{"the text never comes", "start\tWhich contour?\n", "timeout 1\nwait Name of the group?\nkey Enter\n",
			"\"Name of the group?\" was not on the screen in 1s", 0},
		{"the program ends while waited for", "start\tQ?\nEnter\texit 3\n", "wait Q?\nkey Enter\nwait Next\nkey Enter\n",
			"the program ended with 3 while waiting for \"Next\"", 1},
		{"the program ends with another status", "start\tQ?\nEnter\texit 3\n", "wait Q?\nkey Enter\nexit 0\n",
			"the program ended with 3, not 0", 1},
		{"the text stays", "start\tQ?\n", "timeout 1\ngone Q?\n", "\"Q?\" was not off the screen in 1s", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, log, _ := play(t, c.app, c.scenario)
			if r.code != 1 || !strings.Contains(r.err, c.says) || !strings.Contains(r.err, "--- the screen:") {
				t.Errorf("status %d:\n%s", r.code, r.err)
			}
			if n := strings.Count(log, "\nkey "); n != c.keys {
				t.Errorf("%d keys went, want %d:\n%s", n, c.keys, log)
			}
		})
	}
}

type cloudConfig struct {
	Users []struct {
		Name       string   `yaml:"name"`
		UID        int      `yaml:"uid"`
		Groups     []string `yaml:"groups"`
		Sudo       []string `yaml:"sudo"`
		LockPasswd *bool    `yaml:"lock_passwd"`
		Keys       []string `yaml:"ssh_authorized_keys"`
	} `yaml:"users"`
	Chpasswd *struct {
		Expire bool `yaml:"expire"`
		Users  []struct {
			Name, Password, Type string
		} `yaml:"users"`
	} `yaml:"chpasswd"`
	Packages []string `yaml:"packages"`
	Runcmd   []any    `yaml:"runcmd"`
}

// userData gives the user-data vm.sh would put on a fresh disk of the
// variant env names, parsed.
func userData(t *testing.T, env ...string) (cloudConfig, string) {
	t.Helper()
	key := filepath.Join(t.TempDir(), "key.pub")
	put(t, filepath.Dir(key), "key.pub", "ssh-ed25519 AAAAtest stand@test\n", 0o644)
	r := run(t, append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "AACP_STAND_KEY=" + key}, env...),
		nil, "vm.sh", "user-data")
	if r.code != 0 {
		t.Fatalf("vm.sh user-data %q ended with %d: %s", env, r.code, r.err)
	}
	if !strings.HasPrefix(r.out, "#cloud-config\n") {
		t.Errorf("no #cloud-config header:\n%s", r.out)
	}
	var c cloudConfig
	if err := yaml.Unmarshal([]byte(r.out), &c); err != nil {
		t.Fatalf("user-data %q is not YAML: %v\n%s", env, err, r.out)
	}
	return c, r.out
}

func runcmd(c cloudConfig) string {
	var b strings.Builder
	for _, cmd := range c.Runcmd {
		switch v := cmd.(type) {
		case string:
			b.WriteString(v)
		case []any:
			for i, a := range v {
				if i > 0 {
					b.WriteString(" ")
				}
				b.WriteString(a.(string))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestVMUserDataOfEachVariant: the bare machine has none of what the
// installer is to name, the installing user is uid 1001 with sudo as asked
// and the desktop, and Debian gets its own packages and its own GDM file.
func TestVMUserDataOfEachVariant(t *testing.T) {
	full := []string{"docker.io", "tmux", "jq", "curl", "golang-go"}

	c, _ := userData(t)
	if len(c.Users) != 1 || c.Users[0].Name != "dev" || c.Users[0].Keys[0] != "ssh-ed25519 AAAAtest stand@test" {
		t.Errorf("users of the default stand: %+v", c.Users)
	}
	for _, p := range append(full, "ubuntu-desktop-minimal", "docker-compose-v2") {
		if !slices.Contains(c.Packages, p) {
			t.Errorf("the default stand has no %s: %v", p, c.Packages)
		}
	}
	if cmds := runcmd(c); !strings.Contains(cmds, "usermod -aG docker dev\n") || !strings.Contains(cmds, "AutomaticLogin=dev") ||
		!strings.Contains(cmds, "/etc/gdm3/custom.conf") {
		t.Errorf("runcmd of the default stand:\n%s", cmds)
	}

	c, _ = userData(t, "AACP_STAND_BARE=1")
	for _, p := range append(full, "docker-compose-v2") {
		if slices.Contains(c.Packages, p) {
			t.Errorf("the bare stand installs %s: %v", p, c.Packages)
		}
	}
	if cmds := runcmd(c); !strings.Contains(cmds, "apt-get purge -y curl jq tmux\n") || strings.Contains(cmds, "docker") {
		t.Errorf("runcmd of the bare stand:\n%s", cmds)
	}

	c, _ = userData(t, "AACP_STAND_USER=inst")
	if len(c.Users) != 2 || c.Users[1].Name != "inst" || c.Users[1].UID != 1001 ||
		!slices.Equal(c.Users[1].Sudo, []string{"ALL=(ALL) NOPASSWD:ALL"}) || c.Chpasswd != nil {
		t.Errorf("the installing user with sudo without a password: %+v %+v", c.Users, c.Chpasswd)
	}
	if cmds := runcmd(c); !strings.Contains(cmds, "usermod -aG docker inst\n") || !strings.Contains(cmds, "AutomaticLogin=inst") {
		t.Errorf("runcmd with the installing user:\n%s", cmds)
	}

	c, _ = userData(t, "AACP_STAND_USER=inst", "AACP_STAND_SUDO=password", "AACP_STAND_PASSWORD=pw")
	u := c.Users[1]
	if u.UID != 1001 || u.Sudo != nil || u.LockPasswd == nil || *u.LockPasswd || !slices.Contains(u.Groups, "sudo") {
		t.Errorf("the installing user with sudo by password: %+v", u)
	}
	if c.Chpasswd == nil || len(c.Chpasswd.Users) != 1 || c.Chpasswd.Users[0].Name != "inst" ||
		c.Chpasswd.Users[0].Password != "pw" || c.Chpasswd.Expire {
		t.Errorf("chpasswd: %+v", c.Chpasswd)
	}

	c, _ = userData(t, "AACP_STAND_OS=debian13")
	for _, p := range []string{"gnome-core", "docker.io", "docker-compose", "tmux", "jq"} {
		if !slices.Contains(c.Packages, p) {
			t.Errorf("the Debian stand has no %s: %v", p, c.Packages)
		}
	}
	if slices.Contains(c.Packages, "ubuntu-desktop-minimal") || slices.Contains(c.Packages, "docker-compose-v2") {
		t.Errorf("the Debian stand asks for Ubuntu packages: %v", c.Packages)
	}
	if cmds := runcmd(c); !strings.Contains(cmds, "/etc/gdm3/daemon.conf") {
		t.Errorf("GDM of Debian reads daemon.conf:\n%s", cmds)
	}

	for _, env := range []string{"AACP_STAND_OS=fedora", "AACP_STAND_SUDO=maybe"} {
		if r := run(t, []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), env}, nil, "vm.sh", "user-data"); r.code != 2 {
			t.Errorf("%s: status %d, %q", env, r.code, r.err)
		}
	}
}

// TestVMUpInTheForegroundHoldsQemu: up detaches qemu unless asked to keep it
// in the foreground, and then qemu takes the place of the script, so the
// machine ends with the job that ran up.
func TestVMUpInTheForegroundHoldsQemu(t *testing.T) {
	if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err != nil {
		t.Skipf("up refuses without /dev/kvm before it reaches qemu: %v", err)
	} else {
		f.Close()
	}
	for _, c := range []struct {
		env    string
		detach bool
	}{{"AACP_STAND_FOREGROUND=0", true}, {"AACP_STAND_FOREGROUND=1", false}} {
		dir, args := t.TempDir(), filepath.Join(t.TempDir(), "args")
		put(t, dir, "noble-cloud.img", "image", 0o644)
		key := filepath.Join(t.TempDir(), "key.pub")
		put(t, filepath.Dir(key), "key.pub", "ssh-ed25519 AAAAtest stand@test\n", 0o644)
		path := stubs(t, map[string]string{
			"qemu-system-x86_64": "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + args + "\nexit 7\n",
			"qemu-img":           "#!/bin/sh\n",
			"cloud-localds":      "#!/bin/sh\n: > \"$1\"\n",
		})
		r := run(t, []string{"PATH=" + path, "HOME=" + t.TempDir(), "AACP_STAND_VM_DIR=" + dir, "AACP_STAND_KEY=" + key, c.env},
			nil, "vm.sh", "up")
		got, err := os.ReadFile(args)
		if err != nil {
			t.Fatalf("%s: qemu did not run: %v\n%s%s", c.env, err, r.out, r.err)
		}
		if lines := strings.Split(strings.TrimSpace(string(got)), "\n"); slices.Contains(lines, "-daemonize") != c.detach {
			t.Errorf("%s: qemu got %q, detached should be %v", c.env, lines, c.detach)
		}
		// Detached, a qemu that failed stops the script before it names the
		// ports; held, the ports are named first and qemu is the script,
		// its status the job's.
		if r.code != 7 || c.detach == strings.Contains(r.out, "the stand is coming up") {
			t.Errorf("%s: status %d, said %q", c.env, r.code, r.out)
		}
	}
}
