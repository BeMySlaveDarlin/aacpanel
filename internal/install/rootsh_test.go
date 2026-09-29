package install

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// rootScript is root.sh of this tree.
var rootScript = filepath.Join("..", "..", "deploy", "install", "root.sh")

// stubs is a directory of programs that answer the way a machine without
// the panel does: no package installed, no unit enabled or active. It goes
// first on PATH, so a dry run says the same on any machine.
func stubs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"dpkg-query", "systemctl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// root runs root.sh with args and gives its standard output, its standard
// error and its status.
func root(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{rootScript}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+stubs(t)+":"+os.Getenv("PATH"))
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

// dryRoot is a dry run of root.sh that went through.
func dryRoot(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, code := root(t, append(args, "--dry-run")...)
	if code != 0 {
		t.Fatalf("root.sh %q ended with %d:\n%s%s", args, code, out, errOut)
	}
	return out
}

func manifestOf(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(l, "MANIFEST\t"); ok {
			lines = append(lines, strings.ReplaceAll(rest, "\t", " | "))
		}
	}
	return lines
}

// TestADryRunOfRootShPrintsTheManifestOfAFreshMachine: every change comes
// with its line, in the order of the changes, and nothing is changed.
func TestADryRunOfRootShPrintsTheManifestOfAFreshMachine(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	staged := filepath.Join(dir, "host.env.staged")
	if err := os.WriteFile(staged, []byte("AACP_STATE_DIR="+state+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := dryRoot(t, "apply", "--user", "nobody", "--state", state, "--staged", staged,
		"--package", "tmux", "--package", "curl", "--package", "docker.io", "--package", "docker-compose-v2")
	got := manifestOf(out)
	want := []string{
		"pkg | tmux | by-installer",
		"pkg | curl | by-installer",
		"pkg | docker.io | by-installer",
		"pkg | docker-compose-v2 | by-installer",
		"group | docker nobody | by-installer",
		"dir | " + state + " | created",
		"file | " + state + "/host.env | created",
		"linger | nobody | by-installer",
	}
	if len(got) != len(want)+2 || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("the manifest of the dry run is\n%s\nwant it to begin\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.HasPrefix(got[len(want)], "sysunit | /etc/systemd/system/aacpanel-agent@.service | ") ||
		!strings.Contains(got[len(want)], " sha=") {
		t.Errorf("the unit's line is %q", got[len(want)])
	}
	if got[len(want)+1] != "enabled | system aacpanel-agent@nobody.service | by-installer" {
		t.Errorf("the last line is %q", got[len(want)+1])
	}
	// Each line comes before its change.
	for _, pair := range [][2]string{
		{"MANIFEST\tdir\t", "would run: install -d"},
		{"MANIFEST\tlinger\t", "would run: loginctl enable-linger nobody"},
		{"MANIFEST\tenabled\t", "would run: systemctl enable --now aacpanel-agent@nobody.service"},
	} {
		if i, j := strings.Index(out, pair[0]), strings.Index(out, pair[1]); i < 0 || j < 0 || i > j {
			t.Errorf("%q does not come before %q:\n%s", pair[0], pair[1], out)
		}
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Error("a dry run made the state directory")
	}
}

// TestRootShRunsToItsEndThroughACtrlC: the installer hands its terminal to
// root.sh, and a Ctrl+C there reaches the whole group; the part as root goes
// on to its end rather than stop between two changes.
func TestRootShRunsToItsEndThroughACtrlC(t *testing.T) {
	dir := t.TempDir()
	stubs := t.TempDir()
	// dpkg-query is asked first of all: the Ctrl+C comes while it runs.
	for name, body := range map[string]string{"dpkg-query": "#!/bin/sh\nkill -INT 0\nexit 1\n", "systemctl": "#!/bin/sh\nexit 1\n"} {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", rootScript, "apply", "--user", "nobody", "--state", filepath.Join(dir, "state"),
		"--package", "tmux", "--dry-run")
	cmd.Env = append(os.Environ(), "PATH="+stubs+":"+os.Getenv("PATH"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "MANIFEST\tenabled\tsystem aacpanel-agent@nobody.service") {
		t.Errorf("root.sh stopped at a Ctrl+C (%v):\n%s", err, out)
	}
}

// TestRootShDoesNotTakeAStateDirectoryOfAnotherUser: the directory stays
// whose it is, and the run stops before its first change.
func TestRootShDoesNotTakeAStateDirectoryOfAnotherUser(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if me.Uid == "65534" {
		t.Skip("the test runs as nobody, whose directory it would be")
	}
	state := t.TempDir()
	out, errOut, code := root(t, "apply", "--user", "nobody", "--state", state, "--package", "tmux", "--dry-run")
	if code != 1 || !strings.Contains(errOut, "stop: "+state+" exists and belongs to "+me.Username+" — left from another install. sudo chown -R nobody:") {
		t.Errorf("a foreign state directory: status %d, said %q", code, errOut)
	}
	if strings.Contains(out, "MANIFEST") || strings.Contains(out, "chown") {
		t.Errorf("root.sh went on past a foreign directory:\n%s", out)
	}
}

func TestRootShRefusesWhatItDoesNotTake(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
		says string
	}{
		{nil, 2, "a command is missing"},
		{[]string{"launch"}, 2, "no command launch"},
		{[]string{"apply"}, 2, "apply takes --user NAME"},
		{[]string{"apply", "--user"}, 2, "--user takes a value"},
		{[]string{"apply", "--user", "nobody", "--linger"}, 2, "apply takes no --linger"},
		{[]string{"restart-agent", "--user", "nobody", "--package", "tmux"}, 2, "restart-agent takes no --package"},
		{[]string{"apply", "--user", "nobody", "--package", "wget"}, 2, "wget is not one of them"},
		{[]string{"apply", "--user", "nobody", "--state", "var/lib/aacpanel"}, 2, "not a plain absolute path"},
		{[]string{"apply", "--user", "nobody", "--state", "/var/lib/../etc"}, 2, "not a plain absolute path"},
		{[]string{"apply", "--user", "nobody", "--state", "/srv/my state"}, 2, "not a plain absolute path"},
		{[]string{"apply", "--user", "no-such-user-here"}, 1, "there is no user no-such-user-here"},
		{[]string{"apply", "--user", "root", "--dry-run"}, 1, "the panel runs as a user, not as root"},
	} {
		_, errOut, code := root(t, c.args...)
		if code != c.code || !strings.Contains(errOut, c.says) {
			t.Errorf("root.sh %q: status %d, said %q; want %d saying %q", c.args, code, errOut, c.code, c.says)
		}
	}
	if os.Geteuid() != 0 {
		_, errOut, code := root(t, "apply", "--user", "nobody")
		if code != 1 || !strings.Contains(errOut, "runs as root: sudo bash ") {
			t.Errorf("a real run without root: status %d, said %q", code, errOut)
		}
	}
}

func TestRootShHelpNamesItsCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"apply", "--help"}} {
		out, _, code := root(t, args...)
		if code != 0 || !strings.HasPrefix(out, "usage: root.sh apply --user NAME") ||
			!strings.Contains(out, "restart-agent") || !strings.Contains(out, "--dry-run") {
			t.Errorf("root.sh %q: status %d:\n%s", args, code, out)
		}
	}
}

// TestRootShTurnsLingerOffLast reads the removal of root.sh: linger goes
// after the collector is disabled and its unit is gone, since a user
// manager that stops with linger takes nothing of the panel's down with it
// then, and it goes only on --linger, which uninstall passes for linger the
// installer turned on.
func TestRootShTurnsLingerOffLast(t *testing.T) {
	raw, err := os.ReadFile(rootScript)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "\nremove() {")
	end := strings.Index(body[start:], "\n}\n")
	remove := body[start : start+end]
	linger := strings.Index(remove, "loginctl disable-linger")
	for _, before := range []string{"systemctl disable --now", `rm -f "$unit"`, "systemctl daemon-reload"} {
		if i := strings.Index(remove, before); i < 0 || linger < 0 || i > linger {
			t.Errorf("remove turns linger off at %d, before %q at %d:\n%s", linger, before, i, remove)
		}
	}
	if !strings.Contains(remove, `if [ "$linger" = 1 ]`) {
		t.Errorf("remove turns linger off without --linger:\n%s", remove)
	}
}

func TestADryRemoveNamesWhatItTakesAway(t *testing.T) {
	out := dryRoot(t, "remove", "--user", "nobody", "--state", t.TempDir(), "--purge-state")
	if strings.Contains(out, "MANIFEST") || !strings.Contains(out, "would run: rm -rf ") {
		t.Errorf("a dry remove:\n%s", out)
	}
	out = dryRoot(t, "restart-agent", "--user", "nobody")
	if out != "would run: systemctl restart aacpanel-agent@nobody.service\n" {
		t.Errorf("a dry restart: %q", out)
	}
}
