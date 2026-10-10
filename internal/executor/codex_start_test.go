package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/codex/codextest"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/launcher"
)

// onCodexContour starts a fake codex daemon as the home of contour acme and
// an executor linked to it, and returns the project New hands the executor:
// a directory in the roots, and the claude config directory the registry
// names acme by.
func onCodexContour(t *testing.T) (*codextest.Server, *Executor, *action.Project) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sessionFiles(t)
	srv := codextest.New(t)
	home := filepath.Join(t.TempDir(), "acme")
	if err := os.Symlink(srv.Home, home); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registry.CodexHomesEnv, home)
	config := filepath.Join(t.TempDir(), ".claude-client")
	reg := filepath.Join(t.TempDir(), "registry.conf")
	body := "acme | /srv/acme/ | " + config + " | -\nalgo | /srv/algo/ | /srv/algo-config | -\n"
	if err := os.WriteFile(reg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registry.RegistryEnv, reg)
	root := t.TempDir()
	dir := filepath.Join(root, "shop")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projectRootsEnv, root)

	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil, "")
	e.codex = codex.Start(ctx, registry.CodexHomes(), nil)
	t.Cleanup(func() {
		cancel()
		e.codex.Wait()
	})
	return srv, e, &action.Project{Path: dir, Session: "shop", ConfigDir: config}
}

func open(t *testing.T, e *Executor, p *action.Project, launch string) (string, string, error) {
	t.Helper()
	p.Launch = json.RawMessage(launch)
	return e.Open(context.Background(), action.Request{Kind: action.SessionOpen, Target: p.Session, Project: p})
}

// heldMark says whether the panel marked a thread of contour acme as one it
// holds.
func heldMark(t *testing.T, id string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "aacpanel-stream", "codex-held", "acme", id))
	return err == nil
}

func paramsOf(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// New of a project whose agent is codex starts a thread in the daemon of the
// project's contour: in its directory, with the model, the effort, the
// approvals and the sandbox of the map where it chose them, and the first
// message as the first turn at the map's effort. The answer names the session
// by the thread.
func TestNewOfACodexProjectStartsAThreadInTheDaemonOfItsContour(t *testing.T) {
	srv, e, p := onCodexContour(t)

	detail, name, err := open(t, e, p, `{"agent":"codex","codexModel":"gpt-5.5","codexEffort":"high",`+
		`"codexApproval":"untrusted","codexSandbox":"read-only","intent":"run the tests"}`)
	if err != nil {
		t.Fatal(err)
	}
	if name != "codex-beef0001" || !strings.Contains(detail, "session codex-beef0001 started") ||
		!strings.Contains(detail, "opening message: run the tests") {
		t.Errorf("New answered %q naming %q", detail, name)
	}
	starts := srv.Calls("thread/start")
	want := map[string]any{"cwd": p.Path, "model": "gpt-5.5", "approvalPolicy": "untrusted", "sandbox": "read-only",
		"config": map[string]any{"model_reasoning_effort": "high"}}
	if len(starts) != 1 || !reflect.DeepEqual(paramsOf(t, starts[0]), want) {
		t.Fatalf("thread/start went as %s", starts)
	}
	turns := srv.Calls("turn/start")
	if len(turns) != 1 {
		t.Fatalf("turn/start went %d times", len(turns))
	}
	if turn := paramsOf(t, turns[0]); turn["effort"] != "high" || !strings.Contains(string(turns[0]), `"run the tests"`) {
		t.Errorf("the first turn went as %s", turns[0])
	}
	found := e.codex.Find(name)
	if len(found) != 1 {
		t.Fatalf("the new session is not found by its name: %+v", found)
	}
	if !heldMark(t, found[0].ID) {
		t.Error("the panel does not hold the thread it started in the daemon")
	}

	if _, _, err := open(t, e, p, `{"agent":"codex"}`); err != nil {
		t.Fatal(err)
	}
	starts = srv.Calls("thread/start")
	if got := paramsOf(t, starts[1]); !reflect.DeepEqual(got, map[string]any{"cwd": p.Path}) {
		t.Errorf("a map that chose nothing started the thread with %v", got)
	}
	if n := len(srv.Calls("turn/start")); n != 1 {
		t.Errorf("a project with no first message started a turn: %d turns", n)
	}
}

// A thread of the archive is resumed in the daemon of its contour and held: it
// is a live session of the panel from then on, under the tail of its id, and
// closes like a thread the panel started.
func TestAThreadOfTheArchiveIsResumedInTheDaemonOfItsContour(t *testing.T) {
	srv, e, _ := onCodexContour(t)
	const past = "019a2000-0000-7000-8000-00000000a1b2"
	srv.Add(codextest.Thread{ID: past, CWD: "/srv/acme/shop", Path: "/srv/codex/sessions/rollout-" + past + ".jsonl",
		Model: "gpt-test", Created: 1791554348, Status: "notLoaded"})

	resume := action.Request{Kind: action.SessionResume, Target: "shop", Resume: past, Agent: action.ResumeCodex,
		Contour: "acme"}
	detail, name, err := e.Open(context.Background(), resume)
	if err != nil {
		t.Fatal(err)
	}
	if name != "codex-0000a1b2" ||
		!strings.Contains(detail, "session codex-0000a1b2 resumed in the codex daemon of contour acme") {
		t.Errorf("the resume answered %q naming %q", detail, name)
	}
	if resumes := srv.Calls("thread/resume"); len(resumes) == 0 || paramsOf(t, resumes[0])["threadId"] != past {
		t.Fatalf("thread/resume went as %s", resumes)
	}
	if !srv.Subscribed(past) || !heldMark(t, past) {
		t.Error("the panel does not hold the thread it resumed")
	}
	if found := e.codex.Find(name); len(found) != 1 || found[0].ID != past {
		t.Fatalf("the resumed session is not found by its name: %+v", found)
	}
	if n := len(srv.Calls("thread/start")) + len(srv.Calls("turn/start")); n != 0 {
		t.Errorf("a resume started %d threads and turns", n)
	}
	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionClose, Target: name}); err != nil {
		t.Fatal(err)
	}
	if srv.Subscribed(past) || heldMark(t, past) {
		t.Error("the resumed thread was not let go at its close")
	}
}

// A contour whose home has no daemon running is refused with the way to go on
// with the thread, and the panel starts no daemon for a resume; a contour
// without a codex home is refused as well.
func TestAResumeInAHomeWithoutADaemonIsRefused(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), ".codex")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registry.CodexHomesEnv, home)
	_, started := fakeCodex(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil, "")
	e.codex = codex.Start(ctx, registry.CodexHomes(), nil)
	t.Cleanup(func() {
		cancel()
		e.codex.Wait()
	})

	const past = "019a2000-0000-7000-8000-00000000a1b2"
	resume := action.Request{Kind: action.SessionResume, Target: "shop", Resume: past, Agent: action.ResumeCodex,
		Contour: "personal"}
	_, name, err := e.Open(context.Background(), resume)
	if err == nil || !strings.Contains(err.Error(), "no codex daemon runs for "+home) ||
		!strings.Contains(err.Error(), "codex resume "+past) || name != "" {
		t.Errorf("a resume in a home without a daemon answered %v, naming %q", err, name)
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Errorf("a resume started codex: %v", err)
	}
	resume.Contour = "algo"
	if _, err := e.Execute(context.Background(), resume); err == nil ||
		!strings.Contains(err.Error(), "contour algo has no codex home") {
		t.Errorf("a resume in a contour without a codex home answered %v", err)
	}
}

// A contour that has no codex home is refused with the reason, and no daemon
// hears of it.
func TestNewOfACodexProjectOfAContourWithoutAHomeIsRefused(t *testing.T) {
	srv, e, p := onCodexContour(t)
	p.ConfigDir = "/srv/algo-config"
	_, _, err := open(t, e, p, `{"agent":"codex"}`)
	if err == nil || !strings.Contains(err.Error(), "contour algo has no codex home") ||
		!strings.Contains(err.Error(), registry.CodexHomesEnv) {
		t.Errorf("New in a contour with no codex home: %v", err)
	}
	if n := len(srv.Calls("thread/start")); n != 0 {
		t.Errorf("a refused New started %d threads", n)
	}
}

// A close interrupts the turn that runs and leaves the thread, and closes by
// its exact name the tmux session the launcher started codex in on that
// thread — no other, and not codex typed by hand. A restart is refused with
// the reason.
func TestACodexSessionIsClosedAndNotRestarted(t *testing.T) {
	srv, e, p := onCodexContour(t)
	_, name, err := open(t, e, p, `{"agent":"codex","intent":"go"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := e.codex.Find(name)[0].ID
	env := "env -i sh /run/user/1000/aacpanel-launch-77/env.sh /opt/codex resume "
	tmux := newTmuxStub(t, nil, "")
	tmux.reply("list-panes", strings.Join([]string{
		"shop\t" + env + id + " -C " + p.Path,
		"shop\t" + env + id + " -C " + p.Path,
		"shop-2\t" + env + "019a2000-0000-7000-8000-0000beef0099 -C " + p.Path,
		"mine\tcodex resume " + id,
	}, "\n"))

	ctx := context.Background()
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionRestart, Target: name}); err == nil ||
		!strings.Contains(err.Error(), action.CodexNotRestarted) {
		t.Errorf("a restart of a codex session: %v", err)
	}

	detail, err := e.Execute(ctx, action.Request{Kind: action.SessionClose, Target: name})
	if err != nil {
		t.Fatal(err)
	}
	if stops := srv.Calls("turn/interrupt"); len(stops) != 1 || paramsOf(t, stops[0])["turnId"] != "turn-1" {
		t.Errorf("turn/interrupt went as %s", stops)
	}
	if leaves := srv.Calls("thread/unsubscribe"); len(leaves) != 1 || paramsOf(t, leaves[0])["threadId"] != id {
		t.Errorf("thread/unsubscribe went as %s", leaves)
	}
	if srv.Subscribed(id) {
		t.Error("the closed thread was not left")
	}
	argv := string(readArgv(t, tmux.log))
	if strings.Count(argv, "kill-session\n-t\n=shop\n") != 1 || strings.Contains(argv, "=shop-2") ||
		strings.Contains(argv, "=mine") {
		t.Errorf("tmux was told\n%s", argv)
	}
	for _, want := range []string{"closed", "interrupted", "tmux session shop closed"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the close says %q, without %q", detail, want)
		}
	}
	if found := e.codex.Find(name); len(found) != 0 {
		t.Errorf("the closed session is still found: %+v", found)
	}
	if n := len(srv.Calls("thread/archive")); n != 0 {
		t.Errorf("the close archived the thread %d times", n)
	}
}

func readArgv(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("tmux was not called: %v", err)
	}
	return raw
}

// fakeCodex is a codex program that logs how it was started — its words, its
// home and its directory — and fails when asked to.
func fakeCodex(t *testing.T, fail bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "started")
	bin := filepath.Join(dir, "codex")
	body := "#!/bin/sh\n{ echo \"$@\"; echo \"CODEX_HOME=$CODEX_HOME\"; } > " + log + ".tmp && mv " + log + ".tmp " + log + "\n"
	if fail {
		body += "echo 'no daemon for you' >&2\nexit 3\n"
	}
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codexEnv, bin)
	return bin, log
}

// A home whose daemon does not run gets one: codex app-server daemon start in
// a transient unit of its own, with the home named, in the project's
// directory; the executor waits for its socket and its connection and starts
// the thread within the same action.
func TestNewStartsTheDaemonWhereNoneRuns(t *testing.T) {
	srv, e, p := onCodexContour(t)
	waitFor(t, "the link to connect", func() bool { return e.codex.Home(registry.CodexHomes()[0].Dir).Up() })
	srv.Close()
	bin, started := fakeCodex(t, false)
	unit := fakeSystemdRun(t)
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(started); err == nil {
				srv.Start(t)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	detail, name, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	home := registry.CodexHomes()[0].Dir
	if name == "" || !strings.Contains(detail, "the daemon of "+home+" was not running and was started") {
		t.Errorf("New answered %q naming %q", detail, name)
	}
	args := strings.Split(strings.TrimSpace(string(readArgv(t, unit))), "\n")
	for _, want := range []string{"--user", "--wait", "--collect", "--property=KillMode=process",
		"--working-directory=" + p.Path, "--setenv=CODEX_HOME=" + home} {
		if !slices.Contains(args, want) {
			t.Errorf("systemd-run went without %q: %q", want, args)
		}
	}
	if at := slices.Index(args, "--"); at < 0 || !slices.Equal(args[at+1:], []string{bin, "app-server", "daemon", "start"}) {
		t.Errorf("the unit runs %q", args)
	}
	if got := string(readArgv(t, started)); got != "app-server daemon start\nCODEX_HOME="+home+"\n" {
		t.Errorf("codex started as %q", got)
	}
}

// A daemon that does not start, or starts and puts up no socket in the home,
// is said so — the second with the program to name instead.
func TestADaemonThatDoesNotComeUpIsSaid(t *testing.T) {
	srv, e, p := onCodexContour(t)
	srv.Close()
	fakeSystemdRun(t)
	fakeCodex(t, true)
	if _, _, err := open(t, e, p, `{"agent":"codex"}`); err == nil || !strings.Contains(err.Error(), "was not started") {
		t.Errorf("a daemon start that failed: %v", err)
	}

	was := daemonWait
	daemonWait = 200 * time.Millisecond
	defer func() { daemonWait = was }()
	fakeCodex(t, false)
	if _, _, err := open(t, e, p, `{"agent":"codex"}`); err == nil || !strings.Contains(err.Error(), "no control socket") ||
		!strings.Contains(err.Error(), codexEnv) {
		t.Errorf("a daemon that put up no socket: %v", err)
	}
	if n := len(srv.Calls("thread/start")); n != 0 {
		t.Errorf("%d threads started with no daemon", n)
	}
}

// codex in tmux: the thread starts in the daemon with the map's choices,
// named after the project's session and with no first message — codex in the
// terminal sends it — and the launcher gets the thread, the home and the codex
// program to resume it with.
func TestNewInTmuxStartsTheThreadAndHandsItToTheLauncher(t *testing.T) {
	srv, e, p := onCodexContour(t)
	bin, _ := fakeCodex(t, false)
	task := fakeLauncher(t, launcher.Report{Session: "shop", Transport: launcher.TransportTmux})

	detail, name, err := open(t, e, p, `{"agent":"codex","codexTransport":"tmux","codexModel":"gpt-5.5",`+
		`"codexApproval":"untrusted","intent":"go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "session "+name+" started: codex in tmux session shop") {
		t.Errorf("New answered %q", detail)
	}
	if n := len(srv.Calls("turn/start")); n != 0 {
		t.Errorf("the panel sent the first message itself: %d turns", n)
	}
	id := e.codex.Find(name)[0].ID
	if heldMark(t, id) {
		t.Error("the panel holds a thread codex in a terminal holds")
	}
	if got := paramsOf(t, srv.Calls("thread/start")[0]); got["model"] != "gpt-5.5" || got["approvalPolicy"] != "untrusted" {
		t.Errorf("the thread started with %v", got)
	}
	if names := srv.Calls("thread/name/set"); len(names) != 1 || paramsOf(t, names[0])["name"] != "shop" ||
		paramsOf(t, names[0])["threadId"] != id {
		t.Errorf("thread/name/set went as %s", names)
	}
	raw := string(readArgv(t, task))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	var spec launcher.Spec
	if start < 0 || json.Unmarshal([]byte(raw[start:end+1]), &spec) != nil {
		t.Fatalf("the launcher got %q", raw)
	}
	home := registry.CodexHomes()[0].Dir
	if spec.Codex == nil || *spec.Codex != (launcher.CodexSpec{Thread: id, Home: home, Bin: bin}) ||
		spec.Dir != p.Path || spec.Session != "shop" || !strings.Contains(string(spec.Launch), `"intent":"go"`) {
		t.Errorf("the launcher got %+v (codex %+v)", spec, spec.Codex)
	}
}
