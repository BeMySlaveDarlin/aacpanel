package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/schema"
)

// The codex launch reads its part of the schema whole: every key of codex and
// every common one lands in what codex starts with, and none of claude's — so
// a key added to the codex tab without a launch behind it fails here.
func TestTheCodexLaunchTakesEveryKeyOfCodex(t *testing.T) {
	for _, p := range schema.Params() {
		if p.Host {
			continue
		}
		raw, err := json.Marshal(map[string]any{p.Key: sample(t, p)})
		if err != nil {
			t.Fatal(err)
		}
		got, warns := ParseCodex(raw)
		if len(warns) != 0 {
			t.Errorf("%s: the codex launch complained about a value the schema accepts: %v", p.Key, warns)
		}
		took := got != CodexParams{Transport: CodexInDaemon}
		switch mine := p.Agent == schema.AgentCodex || p.Key == keyAgent || p.Key == keyIntent; {
		case mine && !took:
			t.Errorf("%s: the codex launch read the key and kept nothing of it", p.Key)
		case !mine && took:
			t.Errorf("%s is not codex's, and the codex launch took it: %+v", p.Key, got)
		}
	}
	got, warns := ParseCodex(json.RawMessage(`{"agent":"codex","codexSandbox":"--yolo","codexModel":"gpt 5"}`))
	if got.Agent != schema.AgentCodex || got.Sandbox != "" || got.Model != "" || len(warns) != 2 {
		t.Errorf("values the schema refuses read as %+v with %v", got, warns)
	}
}

const codexThread = "019a2000-0000-7000-8000-0000beef0001"

// codex in tmux resumes the thread the panel started, by its id at a fixed
// place, so the command tmux keeps for the pane names the thread, in the
// project's directory, with the first message last, after "--". The map's
// choices are not flags: the thread has them, and codex given a configuration
// override refuses a thread the daemon holds loaded.
func TestTheCommandOfCodexInTmuxResumesTheThread(t *testing.T) {
	want := []string{"resume", codexThread, "-C", "/srv/proj", "--", "-v run the tests"}
	if got := CodexArgs("/srv/proj", codexThread, "-v run the tests"); !slices.Equal(got, want) {
		t.Errorf("the command is\n%q\nexpected\n%q", got, want)
	}
	if got := CodexArgs("/srv/proj", codexThread, ""); !slices.Equal(got, []string{"resume", codexThread, "-C", "/srv/proj"}) {
		t.Errorf("no first message gives %q", got)
	}
}

// The pane of codex the launcher started on a thread is told by the command
// tmux keeps for it, and only for that thread; codex started by hand, or by
// the launcher on another thread, is not it.
func TestTheLaunchedPaneOfCodexIsKnownByItsThread(t *testing.T) {
	env := "/run/user/1000/" + envDirPrefix + "1234/" + envFileName
	pane := "env -i sh " + env + " /opt/codex resume " + codexThread + " -C /srv/proj -- \"go on\""
	if !LaunchedCodex(pane, codexThread) {
		t.Errorf("the launched pane was not recognised: %s", pane)
	}
	for _, other := range []string{
		strings.Replace(pane, codexThread, "019a2000-0000-7000-8000-0000beef0002", 1),
		"codex resume " + codexThread,
		"env -i sh /tmp/env.sh /opt/codex resume " + codexThread,
		"env -i sh " + env + " /opt/codex -C /srv/proj resume " + codexThread,
	} {
		if LaunchedCodex(other, codexThread) {
			t.Errorf("a pane that is not the launcher's codex on the thread passed: %s", other)
		}
	}
	if LaunchedCodex(pane, "") {
		t.Error("a pane passed for no thread")
	}
}

// fakeCodexTmux is tmux for a start of codex: it logs the start, reads the
// environment file the way the pane does — the file goes once read — names
// the sessions there are, and says whether the new one is still up.
func fakeCodexTmux(t *testing.T, sessions string, up bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log, env := filepath.Join(dir, "arguments"), filepath.Join(dir, "environment")
	status := "0"
	if !up {
		status = "1"
	}
	body := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"new-session)\n" +
		"  echo \"$@\" > " + log + "\n" +
		"  prev=\n" +
		"  for a in \"$@\"; do\n" +
		"    if [ \"$prev\" = sh ]; then cat \"$a\" > " + env + "; rm -f \"$a\"; fi\n" +
		"    prev=$a\n" +
		"  done ;;\n" +
		"list-sessions) printf '" + sessions + "' ;;\n" +
		"has-session) exit " + status + " ;;\n" +
		"esac\n"
	script := filepath.Join(dir, "tmux")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tmuxEnv, script)
	return log, env
}

// codex in tmux starts on the thread under the project's name or the first
// free one — a tmux session of that name counts as taken — with the home of
// the contour as CODEX_HOME and nothing of claude's in its environment.
func TestCodexStartsInTmuxOnTheThread(t *testing.T) {
	fakeProc(t, fproc{pid: 10, comm: "plasmashell", env: []string{"DISPLAY=:10", "DBUS_SESSION_BUS_ADDRESS=" + liveBus(t)}})
	t.Setenv("DISPLAY", ":10")
	t.Setenv(terminalAutoEnv, "0")
	t.Setenv("CLAUDE_CODE_SOMETHING", "1")
	t.Setenv("CODEX_HOME", "/home/u/.codex")
	log, env := fakeCodexTmux(t, "shop\\nother\\n", true)
	dir := t.TempDir()
	launch := json.RawMessage(`{"agent":"codex","codexTransport":"tmux","codexModel":"gpt-5.5","codexEffort":"high",` +
		`"codexApproval":"untrusted","codexSandbox":"workspace-write","intent":"go on"}`)

	rep, err := Run(context.Background(), Spec{Dir: dir, Session: "shop", Launch: launch,
		Codex: &CodexSpec{Thread: codexThread, Home: "/home/u/.codex-profiles/acme", Bin: "/opt/codex/bin/codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (Report{Session: "shop-2", Dir: dir, Intent: "go on", Transport: TransportTmux}); !reflect.DeepEqual(rep, want) {
		t.Errorf("the report is %+v, expected %+v", rep, want)
	}
	start := strings.Fields(string(readFile(t, log)))
	at := slices.Index(start, "sh")
	if at < 0 || !slices.Equal(start[:7], []string{"new-session", "-d", "-s", "shop-2", "-c", dir, "--"}) {
		t.Fatalf("tmux started %q", start)
	}
	if command := strings.Join(start[at-2:], " "); !LaunchedCodex(command, codexThread) {
		t.Errorf("the pane of the start is not known as the launcher's codex on the thread: %s", command)
	}
	if got, want := strings.Join(start[at+2:], " "), "/opt/codex/bin/codex resume "+codexThread+" -C "+dir+
		" -- go on"; got != want {
		t.Errorf("codex runs as\n%s\nexpected\n%s", got, want)
	}
	var named []string
	for _, line := range strings.Split(string(readFile(t, env)), "\n") {
		if strings.HasPrefix(line, "'CODEX_HOME=") || strings.HasPrefix(line, "'CLAUDE") {
			named = append(named, strings.TrimSuffix(line, " \\"))
		}
	}
	if want := []string{"'CODEX_HOME=/home/u/.codex-profiles/acme'"}; !slices.Equal(named, want) {
		t.Errorf("codex gets %q of the home and of claude, expected %q", named, want)
	}
}

// codex that ends at once ends its tmux session with it, and the start says
// so with the command it ran.
func TestCodexThatEndsAtOnceIsAFailedStart(t *testing.T) {
	fakeProc(t)
	t.Setenv(terminalAutoEnv, "0")
	fakeCodexTmux(t, "", false)
	_, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "shop",
		Codex: &CodexSpec{Thread: codexThread, Home: "/h", Bin: "/opt/codex"}})
	if err == nil || !strings.Contains(err.Error(), "ended at once") ||
		!strings.Contains(err.Error(), "/opt/codex resume "+codexThread) {
		t.Errorf("codex ending at once: %v", err)
	}
}
