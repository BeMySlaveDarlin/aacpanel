package install

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/action"
)

// ---- S14: the check ----

// chainUp gives the rig a panel whose every link holds: the service and its
// local listener answer, the collector writes, the executor's socket is the
// user's, the containers are healthy, the units are on, claude is wired.
func (g *rig) chainUp() {
	g.t.Helper()
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\n", 0o600)
	g.write(filepath.Join(g.state, "state.json"), `{"sessions":[]}`, 0o644)
	g.fresh()
	uid := g.in.uid()
	g.m.Web = map[string]int{"http://127.0.0.1:8776/healthz": 200, "http://127.0.0.1:8777/healthz": 200}
	g.m.Pages = map[string]string{
		"http://127.0.0.1:8777/api/exec":     `{"available":true,"kinds":["session.open"]}`,
		"http://127.0.0.1:8777/api/profiles": `{"profiles":[{"id":1,"name":"personal","configDir":"` + g.home + `/.claude","groups":[]}]}`,
		"http://127.0.0.1:8777/api/devices":  `{"devices":[],"revoked":[],"current":0}`,
	}
	g.m.Stats[g.in.ExecSocket()] = Stat{Mode: fs.ModeSocket | 0o600, UID: uid}
	g.m.Stats[askSocket] = Stat{Mode: fs.ModeSocket | 0o600, UID: uid}
	g.m.Stats[lingerPath("u")] = Stat{Mode: 0o644}
	g.says([]string{g.in.execPath(), "-list"}, "session.open\nsession.close\n")
	g.says([]string{"docker", "compose", "logs", "--no-color", "aacpanel"}, "aacpanel  | store: connected\n")
	g.says([]string{"docker", "inspect", "-f", "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}",
		"aacpanel", "aacpanel-db", "aacpanel-socket-proxy"}, "/aacpanel running healthy\n/aacpanel-db running healthy\n/aacpanel-socket-proxy running \n")
	for _, c := range [][]string{
		{"systemctl", "is-active", "aacpanel-agent@u.service"}, {"systemctl", "--user", "is-active", execService},
	} {
		g.says(c, "active\n")
	}
	for _, c := range [][]string{
		{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, {"systemctl", "--user", "is-enabled", execService},
	} {
		g.says(c, "enabled\n")
	}
	personal := filepath.Join(g.home, ".claude")
	wired, _, err := Wire([]byte("{}"), g.in.wiring())
	if err != nil {
		g.t.Fatal(err)
	}
	g.write(filepath.Join(personal, "settings.json"), string(wired), 0o600)
	if err := g.mcpEntry(g.in.mcpFile(personal), g.in.mcpWant()); err != nil {
		g.t.Fatal(err)
	}
}

// fresh makes the collector's snapshot three seconds old by the run's clock.
func (g *rig) fresh() {
	g.t.Helper()
	at := g.clock.Now().Add(-3 * time.Second)
	if err := os.Chtimes(filepath.Join(g.state, "state.json"), at, at); err != nil {
		g.t.Fatal(err)
	}
}

// said collects the lines the steps said.
func (g *rig) said() *[]string {
	var out []string
	g.r.Sink = func(e Event) {
		if e.Type == Said {
			out = append(out, map[Mark]string{Note: "·", Pass: "✓", Warn: "⚠", Stop: "✗"}[e.Mark]+" "+e.Text)
		}
	}
	return &out
}

func TestTheCheckWalksTheChain(t *testing.T) {
	g := newRig(t)
	g.chainUp()
	lines := g.said()
	if err := g.do(g.in.CheckStep()); err != nil {
		t.Fatalf("a healthy chain: %v\n%s", err, strings.Join(*lines, "\n"))
	}
	want := []string{
		"✓ /healthz ok on 127.0.0.1:8776 and on :8777",
		"✓ state.json is 3s old",
		"✓ aacpanel-exec -list names 2 actions",
		"✓ executor socket: owner u · /api/exec says available",
		"✓ database: /api/profiles 200 · authentication failed: 0",
		"✓ containers healthy: aacpanel, aacpanel-db, aacpanel-socket-proxy",
		"✓ aacpanel-agent@u.service active and enabled",
		"✓ aacpanel-exec.service active and enabled · linger on",
		"✓ ask.sock in place",
		"✓ claude wiring in ~/.claude: hook once, status line chained, MCP server listed",
		"✓ the map has 1 contour",
	}
	if !slices.Equal(*lines, want) {
		t.Errorf("the check said\n%s\nwant\n%s", strings.Join(*lines, "\n"), strings.Join(want, "\n"))
	}
	if g.lines() != nil {
		t.Errorf("the check recorded %q", g.lines())
	}

	// Two links broken: each says so, and the step stops with what to look
	// at. A map without contours is no break: the panel works, and its
	// projects screen waits for one.
	g.clock.t = g.clock.t.Add(10 * time.Minute)
	g.m.Stats[g.in.ExecSocket()] = Stat{Mode: fs.ModeSocket | 0o600, UID: 0}
	g.m.Pages["http://127.0.0.1:8777/api/profiles"] = `{"profiles":[]}`
	*lines = nil
	f := failedWith(t, g.do(g.in.CheckStep()), "2 links of the chain are broken")
	// The snapshot was 10m3s old, and the check gave the collector half a
	// minute to write before it called it broken.
	for _, w := range []string{"✗ " + filepath.Join(g.state, "state.json") + " is 10m33s old: the collector does not write",
		"✗ the executor's socket belongs to uid 0, not ", "⚠ the map has no contour: the projects screen stays empty"} {
		if !slices.ContainsFunc(*lines, func(l string) bool { return strings.HasPrefix(l, w) }) {
			t.Errorf("the check never said %q:\n%s", w, strings.Join(*lines, "\n"))
		}
	}
	if len(f.Fix) != 2 {
		t.Errorf("the stop names %q to look at", f.Fix)
	}
}

// wakingClock is the clock of a run that does something of the machine's
// own after it has slept a while: a collector that writes its first snapshot.
type wakingClock struct {
	*fakeClock
	after time.Duration
	slept time.Duration
	wake  func(now time.Time)
}

func (c *wakingClock) Sleep(d time.Duration) {
	c.fakeClock.Sleep(d)
	if c.slept < c.after && c.slept+d >= c.after {
		c.wake(c.Now())
	}
	c.slept += d
}

// TestTheCheckWaitsForACollectorJustStarted: after a boot the collector
// starts late, and a check that comes a moment after it finds the snapshot
// of before the boot; the collector writes within seconds, and the check
// waits for that rather than calling it broken.
func TestTheCheckWaitsForACollectorJustStarted(t *testing.T) {
	g := newRig(t)
	g.chainUp()
	g.clock.t = g.clock.t.Add(2 * time.Minute)
	state := filepath.Join(g.state, "state.json")
	g.r.Clock = &wakingClock{fakeClock: g.clock, after: 8 * time.Second, wake: func(now time.Time) {
		if err := os.Chtimes(state, now, now); err != nil {
			t.Fatal(err)
		}
	}}
	lines := g.said()
	if err := g.do(g.in.CheckStep()); err != nil {
		t.Fatalf("a collector that writes 8 s into the check: %v\n%s", err, strings.Join(*lines, "\n"))
	}
	if !slices.Contains(*lines, "✓ state.json is 0s old") {
		t.Errorf("the check said:\n%s", strings.Join(*lines, "\n"))
	}
}

// TestTheCheckWaitsForAContainerOnItsWayUp: a container recreated a moment
// before the check — the app role's step recreates the panel's — is
// "starting" until its first healthcheck, and the check waits that out; one
// that never settles is a broken link once the wait is over.
func TestTheCheckWaitsForAContainerOnItsWayUp(t *testing.T) {
	inspect := []string{"docker", "inspect", "-f", "{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}",
		"aacpanel", "aacpanel-db", "aacpanel-socket-proxy"}
	const starting = "/aacpanel running starting\n/aacpanel-db running healthy\n/aacpanel-socket-proxy running \n"
	for _, c := range []struct {
		settles int // the inspect that first finds it healthy; 0 is never
		broken  bool
	}{{3, false}, {0, true}} {
		g := newRig(t)
		g.chainUp()
		g.says(inspect, starting)
		asked := 0
		key := Command(inspect[0], inspect[1:]...)
		g.m.Effects[key] = func(Cmd) error {
			if asked++; asked == c.settles {
				g.says(inspect, "/aacpanel running healthy\n/aacpanel-db running healthy\n/aacpanel-socket-proxy running \n")
			}
			return nil
		}
		began := g.clock.Now()
		lines := g.said()
		err := g.do(g.in.CheckStep())
		waited := g.clock.Now().Sub(began)
		switch {
		case !c.broken && (err != nil || !slices.Contains(*lines, "✓ containers healthy: aacpanel, aacpanel-db, aacpanel-socket-proxy")):
			t.Errorf("a container that settles after %d looks: %v\n%s", c.settles, err, strings.Join(*lines, "\n"))
		case !c.broken && waited < 4*time.Second:
			t.Errorf("the check waited %v for a container three looks away from healthy", waited)
		case c.broken:
			failedWith(t, err, "1 link of the chain is broken")
			if !slices.Contains(*lines, "✗ containers: aacpanel is running starting") || waited < healthSettles {
				t.Errorf("a container that never settles, after %v:\n%s", waited, strings.Join(*lines, "\n"))
			}
		}
	}
}

// TestAnAccountWithoutTheServerIsAWarning: the panel gives its own sessions
// the server at their start, so an account without it holds the chain; a
// session started by hand lacks the tools, and the check says so.
func TestAnAccountWithoutTheServerIsAWarning(t *testing.T) {
	g := newRig(t)
	g.chainUp()
	if err := os.Remove(g.in.mcpFile(filepath.Join(g.home, ".claude"))); err != nil {
		t.Fatal(err)
	}
	lines := g.said()
	if err := g.do(g.in.CheckStep()); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(*lines, "\n"))
	}
	for _, w := range []string{"✓ claude wiring in ~/.claude: hook once, status line chained",
		"⚠ no aacpanel server of this install in ~/.claude: the sessions the panel starts get it at their start, a session started by hand has none of the panel's tools"} {
		if !slices.Contains(*lines, w) {
			t.Errorf("the check never said %q:\n%s", w, strings.Join(*lines, "\n"))
		}
	}
}

func TestTheCheckWithTheLocalListenerOff(t *testing.T) {
	g := newRig(t)
	g.chainUp()
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\nAACP_LOCAL_ADDR=\n", 0o600)
	g.m.Pages = nil
	lines := g.said()
	if err := g.do(g.in.CheckStep()); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(*lines, "\n"))
	}
	if !slices.Contains(*lines, "⚠ the map is not asked: the local listener is off (AACP_LOCAL_ADDR is empty in .env)") ||
		!slices.Contains(*lines, "✓ /healthz ok on 127.0.0.1:8776 · the local listener is off") {
		t.Errorf("the check said:\n%s", strings.Join(*lines, "\n"))
	}
}

// fakeExecutor is the executor's socket for the test session: it records
// every request, and the collector's snapshot shows a session it opened
// until it closes it.
type fakeExecutor struct {
	t     *testing.T
	state string
	mu    sync.Mutex
	asked []string
	live  []string
}

func (f *fakeExecutor) snapshot() {
	var sessions []map[string]string
	for _, n := range f.live {
		sessions = append(sessions, map[string]string{"session": n})
	}
	raw, _ := json.Marshal(map[string]any{"sessions": sessions})
	if err := os.WriteFile(f.state, raw, 0o644); err != nil {
		f.t.Error(err)
	}
}

func (f *fakeExecutor) Execute(_ context.Context, req action.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, string(req.Kind)+" "+req.Target)
	switch req.Kind {
	case action.SessionOpen:
		if req.Project == nil || req.Project.Session != req.Target {
			return "", errors.New("a session opened without its project")
		}
		f.live = append(f.live, req.Target)
	case action.SessionClose:
		i := slices.Index(f.live, req.Target)
		if i < 0 {
			return "", errors.New("no session " + req.Target)
		}
		f.live = slices.Delete(f.live, i, i+1)
	}
	f.snapshot()
	return "ok", nil
}

// withExecutor puts the fake executor on a socket of its own; the path is
// short, as a unix socket's must be.
func (g *rig) withExecutor(live ...string) *fakeExecutor {
	g.t.Helper()
	dir, err := os.MkdirTemp("", "ck")
	if err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.MkdirAll(g.state, 0o755); err != nil {
		g.t.Fatal(err)
	}
	f := &fakeExecutor{t: g.t, state: filepath.Join(g.state, "state.json"), live: live}
	f.snapshot()
	sock := filepath.Join(dir, "sock")
	srv := action.NewServer(sock, action.ExecutorFunc(f.Execute), 10*time.Second)
	ln, err := srv.Listen()
	if err != nil {
		g.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.t.Cleanup(cancel)
	go srv.Run(ctx, ln)
	g.in.Sock = sock
	return f
}

// TestTheTestSessionClosesOnlyItsOwn: the check opens aacpanel-check, waits
// for the panel to see it and closes it — and a session of that name that
// was there before is not the check's: it is neither opened again nor
// closed.
func TestTheTestSessionClosesOnlyItsOwn(t *testing.T) {
	g := newRig(t)
	g.r.Answers = map[string]string{"--check-session": yes}
	f := g.withExecutor("shop")
	lines := g.said()
	if err := g.do(g.in.SessionStep()); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(*lines, "\n"))
	}
	if want := []string{"session.open " + CheckSession, "session.close " + CheckSession}; !slices.Equal(f.asked, want) {
		t.Errorf("the executor was asked %q, want %q", f.asked, want)
	}
	if !slices.Equal(f.live, []string{"shop"}) {
		t.Errorf("the sessions left are %q", f.live)
	}
	if last := (*lines)[len(*lines)-1]; last != "✓ "+CheckSession+" closed — only it" {
		t.Errorf("the step ended saying %q", last)
	}

	g2 := newRig(t)
	g2.r.Answers = map[string]string{"--check-session": yes}
	f2 := g2.withExecutor(CheckSession)
	failedWith(t, g2.do(g2.in.SessionStep()), "a session named "+CheckSession+" is live already")
	if len(f2.asked) != 0 || !slices.Equal(f2.live, []string{CheckSession}) {
		t.Errorf("a session that was not the check's: asked %q, live %q", f2.asked, f2.live)
	}
}

// TestTheTestSessionIsNotAskedOfAnAccountNotSignedIn: its session would
// stop at the sign-in, so --yes takes no.
func TestTheTestSessionIsNotAskedOfAnAccountNotSignedIn(t *testing.T) {
	g := newRig(t)
	if q := g.in.SessionQuestion(); q.Default != no || q.Flag != "--check-session" {
		t.Errorf("not signed in: %+v", q)
	}
	g.r.Yes = true
	lines := g.said()
	if err := g.do(g.in.SessionStep()); err != nil || !slices.Contains(*lines, "· skipped: ./install.sh check --session opens it later") {
		t.Errorf("%v: %q", err, *lines)
	}
}

// ---- S15: the first device ----

func TestTheFirstDeviceGetsACodeOnlyWithNoDeviceYet(t *testing.T) {
	g := newRig(t)
	g.chainUp()
	enroll := []string{"docker", "compose", "exec", "-T", "aacpanel", "/aacpanel", "-enroll"}
	g.says(enroll, "7K3QM-9XW2P\n")
	lines := g.said()
	if err := g.do(g.in.EnrollStep()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(*lines, "✓ open http://localhost:8776 and enter the code 7K3QM-9XW2P") {
		t.Errorf("the plain view said %q", *lines)
	}
	g.journal.Close()
	if strings.Contains(g.read(g.journal.Path), "7K3QM") {
		t.Error("the journal holds the code")
	}
	// The screen gets the code with the addresses and a way to another.
	var shown Enrollment
	g.r.Enroll = func(e Enrollment) error { shown = e; return nil }
	g.says(enroll, "AAAAA-BBBBB\n")
	if err := g.do(g.in.EnrollStep()); err != nil {
		t.Fatal(err)
	}
	if shown.Code != "AAAAA-BBBBB" || shown.Expires != g.clock.Now().Add(codeLife) || shown.Again == nil {
		t.Errorf("the screen got %+v", shown)
	}
	// A device there: nothing to do.
	g.m.Pages["http://127.0.0.1:8777/api/devices"] = `{"devices":[{"id":1}]}`
	if !g.already(g.in.EnrollStep()) {
		t.Error("a panel with a device got a code")
	}
}

// ---- adoption ----

// handMade is a machine with the panel installed by hand: the units, the
// executor, the stack, claude's wiring, linger on.
func (g *rig) handMade() {
	g.t.Helper()
	g.write(filepath.Join(g.state, "host.env"), "AACP_HOST=lab\n", 0o644)
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\n", 0o600)
	g.write(g.in.execPath(), "binary", 0o755)
	g.write(g.in.execUnitPath(), "[Unit]\n", 0o644)
	g.m.Files[collectorUnitPath] = "[Unit]\n"
	g.m.Stats[lingerPath("u")] = Stat{Mode: 0o644}
	g.says([]string{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, "enabled\n")
	g.says([]string{"systemctl", "--user", "is-enabled", execService}, "enabled\n")
	g.says([]string{"docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=aacpanel"}, "3f1c2ab\n")
	g.says([]string{"docker", "volume", "inspect", DBVolume}, "[]")
	g.says([]string{"docker", "inspect", testDBName}, "[]")
	personal := filepath.Join(g.home, ".claude")
	g.write(filepath.Join(personal, "settings.json"), `{"hooks":{"PreToolUse":[{"matcher":"AskUserQuestion","hooks":[{"type":"command","command":"python3 `+
		g.clone+`/agent/ask-hook.py"}]}]},"model":"opus"}`, 0o600)
	if err := g.mcpEntry(g.in.mcpFile(personal), g.in.mcpWant()); err != nil {
		g.t.Fatal(err)
	}
}

// TestAnInstallByHandIsTakenOverAsFound: every trace goes into the manifest
// marked adopted before a step would change it, and a step after it adds
// no line of its own for the same thing.
func TestAnInstallByHandIsTakenOverAsFound(t *testing.T) {
	g := newRig(t, mode(Adopt))
	g.handMade()
	steps := g.in.Steps()
	if steps[0].ID != "adopt" {
		t.Fatalf("the first step of an adoption is %s", steps[0].ID)
	}
	if err := g.do(steps[0]); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(g.home, ".claude")
	want := []string{
		"dir " + g.state + " adopted",
		"file " + filepath.Join(g.state, "host.env") + " adopted data",
		"sysunit " + collectorUnitPath + " adopted",
		"enabled system aacpanel-agent@u.service adopted",
		"linger u adopted",
		"file " + g.in.execPath() + " adopted",
		"userunit " + g.in.execUnitPath() + " adopted",
		"enabled user aacpanel-exec.service adopted",
		"file " + filepath.Join(g.clone, ".env") + " adopted data",
		"compose aacpanel adopted project",
		"volume " + DBVolume + " adopted data",
		"testdb " + testDBName + " adopted",
		"json " + filepath.Join(personal, "settings.json") + " adopted",
		"mcp " + personal + " adopted claude=/usr/bin/claude file=" + g.home + "/.claude.json",
	}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds\n%s\nwant\n%s", strings.Join(g.lines(), "\n"), strings.Join(want, "\n"))
	}
	if !g.already(g.in.adoptStep()) {
		t.Error("a second adoption found something new")
	}
	// The wiring after it records the file as adopted: its undo takes the
	// panel out and leaves the rest, since no copy of it before the panel
	// exists.
	g.claudeMachine()
	if err := g.do(g.step("claude")); err != nil {
		t.Fatal(err)
	}
	last := g.lines()[len(g.lines())-1]
	if !strings.HasPrefix(last, "json "+filepath.Join(personal, "settings.json")+" adopted sha=") {
		t.Errorf("the wiring of an adopted file recorded %q", last)
	}
}

// ---- uninstall ----

// installed is a machine the installer put the panel on, with the lines
// its steps would have written.
func (g *rig) installed(linger string) (*Removal, string) {
	g.t.Helper()
	personal := filepath.Join(g.home, ".claude")
	settings := filepath.Join(personal, "settings.json")
	orig := "{\n  \"model\": \"opus\"\n}\n"
	g.write(settings, orig, 0o600)
	backup, err := g.in.backup(settings)
	if err != nil {
		g.t.Fatal(err)
	}
	wired, _, _ := Wire([]byte(orig), g.in.wiring())
	g.write(settings, string(wired), 0o600)
	g.claudeMachine()
	// What the part as root put down is on the machine until it is taken back.
	g.m.Stats[collectorUnitPath] = Stat{Mode: 0o644}
	g.m.Stats[lingerPath("u")] = Stat{Mode: 0o644}
	if err := g.mcpEntry(g.in.mcpFile(personal), g.in.mcpWant()); err != nil {
		g.t.Fatal(err)
	}
	units := g.in.userUnitDir()
	wants := filepath.Join(units, "default.target.wants")
	g.write(g.in.execUnitPath(), "[Unit]\n", 0o644)
	if err := os.MkdirAll(wants, 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.Symlink(g.in.execUnitPath(), filepath.Join(wants, execService)); err != nil {
		g.t.Fatal(err)
	}
	g.write(g.in.execPath(), "binary", 0o755)
	// The executor's probe of the limits starts claude in a cache of its own.
	g.write(filepath.Join(g.home, ".cache", "aacpanel", "limits-probe", ".keep"), "", 0o600)
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\n", 0o600)
	g.write(filepath.Join(g.state, "host.env"), "AACP_HOST=lab\n", 0o644)
	cache := filepath.Join(g.root, "cache")
	g.write(filepath.Join(cache, "gomod", "m@v1", "go.mod"), "module m\n", 0o444)
	if err := os.Chmod(filepath.Join(cache, "gomod", "m@v1"), 0o555); err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(func() { _ = os.Chmod(filepath.Join(cache, "gomod", "m@v1"), 0o755) })
	for _, e := range []Entry{
		{"hostenv", "file", filepath.Join(g.home, ".local/state/aacpanel-install/host.env.staged"), "created"},
		{"root", "dir", g.state, "created"},
		{"root", "file", filepath.Join(g.state, "host.env"), "created"},
		{"root", "linger", "u", linger},
		{"root", "sysunit", collectorUnitPath, "created sha=1"},
		{"root", "enabled", "system aacpanel-agent@u.service", "by-installer"},
		{"envfile", "file", filepath.Join(g.clone, ".env"), "created data"},
		{"exec-build", "dir", filepath.Join(g.home, "bin"), "created"},
		{"exec-build", "dir", cache, "cache"},
		{"exec-build", "file", g.in.execPath(), "created sha=2"},
		{"exec-unit", "dir", filepath.Join(g.home, ".config"), "created"},
		{"exec-unit", "dir", filepath.Join(g.home, ".config/systemd"), "created"},
		{"exec-unit", "dir", units, "created"},
		{"exec-unit", "userunit", g.in.execUnitPath(), "created"},
		{"exec-unit", "enabled", "user aacpanel-exec.service", "by-installer"},
		{"exec-unit", "dir", wants, "created"},
		{"compose", "compose", "aacpanel", "project"},
		{"compose", "volume", DBVolume, "data"},
		{"compose", "image", "aacpanel-aacpanel", "local"},
		{"compose", "image", "postgres:18-alpine", "pulled"},
		{"claude", "json", settings, "orig=" + backup + " sha=" + sha(wired)},
		{"claude", "mcp", personal, "claude=/usr/bin/claude file=" + g.in.mcpFile(personal)},
		{"collector-restart", "rev", "3f1c2ab", ""},
	} {
		if err := g.r.Manifest.Append(e); err != nil {
			g.t.Fatal(err)
		}
	}
	es, err := ReadManifest(g.r.Manifest.Path)
	if err != nil {
		g.t.Fatal(err)
	}
	f := g.in.f()
	f.StateDir = g.state
	rm := NewRemoval(g.m, f, es, cache)
	g.says([]string{g.in.execPath(), "-sessions"}, "")
	for _, c := range [][]string{{"systemctl", "--user", "disable", "--now", execService}, {"systemctl", "--user", "daemon-reload"},
		{"docker", "compose", "down", "--rmi", "local"}, {"docker", "image", "rm", "postgres:18-alpine"},
		{"docker", "volume", "rm", DBVolume}} {
		g.says(c, "")
	}
	g.fails([]string{"docker", "image", "rm", "aacpanel-aacpanel"}, "Error response from daemon: No such image: aacpanel-aacpanel:latest")
	return rm, orig
}

// uninstall runs the removal as the command does, and gives the titles of
// the parts in the order they began.
func (g *rig) uninstall(rm *Removal, rootArgs ...string) []string {
	g.t.Helper()
	root := append([]string{"sudo", "-n", "bash", g.in.Place().RootScript()}, rootArgs...)
	g.says(root, "")
	var began []string
	g.r.Sink = func(e Event) {
		if e.Type == Opened {
			began = append(began, e.Title)
		}
	}
	if err := Perform(g.r, rm.Steps()); err != nil {
		g.t.Fatal(err)
	}
	return began
}

// TestUninstallTakesBackTheManifestInItsOrder: claude's wiring first,
// while the hooks' clone is there; the user's units before the part as
// root, which is one call with linger in it; the executor and the
// directories the installer made; the stack with the images it brought;
// the installer's cache — and the data stays.
func TestUninstallTakesBackTheManifestInItsOrder(t *testing.T) {
	g := newRig(t)
	rm, orig := g.installed("by-installer")
	if err := rm.Choose(nil, "", false); err != nil {
		t.Fatal(err)
	}
	began := g.uninstall(rm, "remove", "--user", "u", "--state", g.state, "--linger")
	if want := []string{"Claude settings", "Your units", "The executor", "The stack", "Root part", "Data and the installer's cache"}; !slices.Equal(began, want) {
		t.Errorf("the parts began %q, want %q", began, want)
	}
	ran := g.m.Ran
	at := func(prefix string) int {
		return slices.IndexFunc(ran, func(c string) bool { return strings.HasPrefix(c, prefix) })
	}
	mcp, disable, root := at("/usr/bin/claude mcp remove"), at("systemctl --user disable"), at("sudo -n bash")
	if mcp < 0 || disable < 0 || root < 0 || !(mcp < disable && disable < root) {
		t.Errorf("claude, the units and root ran in the order %d, %d, %d:\n%s", mcp, disable, root, strings.Join(ran, "\n"))
	}
	if n := len(slices.DeleteFunc(slices.Clone(ran), func(c string) bool { return !strings.HasPrefix(c, "sudo") })); n != 1 {
		t.Errorf("root ran %d times: %q", n, ran)
	}
	if got := g.read(filepath.Join(g.home, ".claude", "settings.json")); got != orig {
		t.Errorf("the settings came back as\n%s", got)
	}
	for _, gone := range []string{g.in.execPath(), g.in.execUnitPath(), filepath.Join(g.home, "bin"), filepath.Join(g.home, ".config"), rm.Cache,
		filepath.Join(g.home, ".cache", "aacpanel")} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, kept := range []string{filepath.Join(g.clone, ".env"), filepath.Join(g.state, "host.env")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("the data %s did not stay: %v", kept, err)
		}
	}
	if slices.Contains(ran, "docker volume rm "+DBVolume) || !slices.Contains(ran, "docker image rm postgres:18-alpine") {
		t.Errorf("the volume went or the pulled image stayed: %q", ran)
	}
	left := strings.Join(rm.Left(), "\n")
	for _, w := range []string{"the volume " + DBVolume + ": docker volume rm " + DBVolume, g.state + ": sudo rm -r " + g.state, "~/aacpanel/.env: rm ~/aacpanel/.env",
		"docker's build cache, which the build of the image filled: docker builder prune"} {
		if !strings.Contains(left, w) {
			t.Errorf("the report of what is left does not say %q:\n%s", w, left)
		}
	}
	// The installer's directory goes last of all.
	if err := g.do(rm.LastStep()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g.in.f().InstallDir); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the installer's directory is still there")
	}
}

// TestUninstallGoesOnAfterTheAdministratorsCommand: without a terminal and
// with sudo that wants a password the uninstall stops at the part as root;
// once an administrator ran the command, the uninstall run again finds it
// taken back and goes on to the data without asking for root.
func TestUninstallGoesOnAfterTheAdministratorsCommand(t *testing.T) {
	g := newRig(t)
	rm, _ := g.installed("by-installer")
	if err := rm.Choose(nil, "", false); err != nil {
		t.Fatal(err)
	}
	delete(g.m.Stats, collectorUnitPath)
	delete(g.m.Stats, lingerPath("u"))
	began := g.uninstall(rm, "remove", "--user", "u", "--state", g.state, "--linger")
	if slices.ContainsFunc(g.m.Ran, func(c string) bool { return strings.HasPrefix(c, "sudo") }) || !slices.Contains(began, "Data and the installer's cache") {
		t.Errorf("root was asked for again: ran %q, parts %q", g.m.Ran, began)
	}
	// Linger still on is still root's to take back.
	g2 := newRig(t)
	rm2, _ := g2.installed("by-installer")
	if err := rm2.Choose(nil, "", false); err != nil {
		t.Fatal(err)
	}
	delete(g2.m.Stats, collectorUnitPath)
	g2.uninstall(rm2, "remove", "--user", "u", "--state", g2.state, "--linger")
	if !slices.ContainsFunc(g2.m.Ran, func(c string) bool { return strings.HasPrefix(c, "sudo") }) {
		t.Errorf("linger left on was not taken back: ran %q", g2.m.Ran)
	}
}

// TestLingerFoundOnStays: linger on before the installer, by hand or by an
// administrator, is never turned off; the root part is still one call.
func TestLingerFoundOnStays(t *testing.T) {
	g := newRig(t)
	rm, _ := g.installed("adopted")
	if err := rm.Choose(nil, "", false); err != nil {
		t.Fatal(err)
	}
	args, _ := rm.RootArgs()
	if slices.Contains(args, "--linger") {
		t.Errorf("root.sh turns off linger it did not turn on: %q", args)
	}
	g.uninstall(rm, "remove", "--user", "u", "--state", g.state)
	if !strings.Contains(strings.Join(rm.Left(), "\n"), "linger for u, on before the installer: sudo loginctl disable-linger u") {
		t.Errorf("the report does not name linger left on:\n%s", strings.Join(rm.Left(), "\n"))
	}
}

// TestDataGoesOnlyWithTheHostNameTyped: nothing chosen keeps it all; a
// choice needs the host name typed, or --yes on a command line that named
// the data.
func TestDataGoesOnlyWithTheHostNameTyped(t *testing.T) {
	g := newRig(t)
	rm, _ := g.installed("by-installer")
	if rm.Host != "lab" {
		t.Fatalf("the host name to type is %q", rm.Host)
	}
	for _, c := range []struct {
		typed string
		yes   bool
		err   error
	}{{"", false, ErrNotTyped}, {"helios", false, ErrNotTyped}, {"lab", false, nil}, {"", true, nil}} {
		if err := rm.Choose([]string{"db", "env", "state"}, c.typed, c.yes); !errors.Is(err, c.err) {
			t.Errorf("typed %q, yes %v: %v", c.typed, c.yes, err)
		}
	}
	if err := rm.Choose([]string{"db", "env", "state"}, "lab", false); err != nil {
		t.Fatal(err)
	}
	g.uninstall(rm, "remove", "--user", "u", "--state", g.state, "--linger", "--purge-state")
	if !slices.Contains(g.m.Ran, "docker volume rm "+DBVolume) {
		t.Errorf("the volume chosen stayed: %q", g.m.Ran)
	}
	if _, err := os.Stat(filepath.Join(g.clone, ".env")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the .env chosen stayed")
	}
}

// TestUninstallOfAnAdoptedInstallLeavesWhatItCannotTell: units by name, the
// executor, the stack, the hooks by the tails of their paths and the server
// by its name go; linger, the test database made by hand and the rest of
// the person's settings stay.
func TestUninstallOfAnAdoptedInstallLeavesWhatItCannotTell(t *testing.T) {
	g := newRig(t, mode(Adopt))
	g.handMade()
	if err := g.do(g.in.adoptStep()); err != nil {
		t.Fatal(err)
	}
	g.claudeMachine()
	es, _ := ReadManifest(g.r.Manifest.Path)
	f := g.in.f()
	f.StateDir = g.state
	rm := NewRemoval(g.m, f, es, filepath.Join(g.root, "cache"))
	if err := rm.Choose(nil, "", false); err != nil {
		t.Fatal(err)
	}
	g.says([]string{g.in.execPath(), "-sessions"}, "")
	for _, c := range [][]string{{"systemctl", "--user", "disable", "--now", execService}, {"systemctl", "--user", "daemon-reload"},
		{"docker", "compose", "down", "--rmi", "local"}} {
		g.says(c, "")
	}
	g.uninstall(rm, "remove", "--user", "u", "--state", g.state)
	settings := g.read(filepath.Join(g.home, ".claude", "settings.json"))
	if strings.Contains(settings, "ask-hook") || !strings.Contains(settings, `"model": "opus"`) {
		t.Errorf("the adopted settings came out as\n%s", settings)
	}
	if slices.ContainsFunc(g.m.Ran, func(c string) bool { return strings.Contains(c, "docker rm") }) {
		t.Errorf("the test database made by hand was removed: %q", g.m.Ran)
	}
	for _, gone := range []string{g.in.execPath(), g.in.execUnitPath()} {
		if _, err := os.Stat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s stayed", gone)
		}
	}
	if _, err := os.Stat(g.state); err != nil {
		t.Errorf("the adopted state directory went: %v", err)
	}
}

// ---- update ----

// TestNewKeysAreTheTemplatesNewOnes: a key the template gained since the
// commit an update came from, and the .env does not set.
func TestNewKeysAreTheTemplatesNewOnes(t *testing.T) {
	g := newRig(t)
	g.says([]string{"git", "-C", g.clone, "show", "abc:.env.example"}, "AACP_SECRET=\n#AACP_BIND=\n")
	g.says([]string{"git", "-C", g.clone, "show", "HEAD:.env.example"}, "AACP_SECRET=\n#AACP_BIND=\n#AACP_NEW_ONE=\nAACP_SET_ALREADY=\n")
	if got := NewKeys(g.r, g.clone, "abc", map[string]string{"AACP_SET_ALREADY": "x"}); !slices.Equal(got, []string{"AACP_NEW_ONE"}) {
		t.Errorf("new keys %q", got)
	}
}

// ---- the report ----

func TestTheReportSaysWhatChangedAndWhatStayed(t *testing.T) {
	g := newRig(t, answer("--terminal", ""))
	step := &Step{ID: "s", Title: "Root part", Undo: UndoKind, Apply: func(r *Run) error {
		if err := r.Record(Group, "docker u", "by-installer"); err != nil {
			return err
		}
		r.Remind("claude is not signed in to ~/.claude-work")
		return r.Record(Dir, filepath.Join(g.home, "bin"), "created")
	}}
	kept := &Step{ID: "k", Title: "Host description", Done: func(*Run) (bool, error) { return true, nil }}
	if err := Perform(g.r, []*Step{kept, step}); err != nil {
		t.Fatal(err)
	}
	rep := NewReport(g.in.S, g.r, time.Minute)
	if !slices.Equal(rep.Changed, []string{"docker u", "~/bin"}) || !slices.Equal(rep.Kept, []string{"Host description"}) {
		t.Errorf("changed %q, kept %q", rep.Changed, rep.Kept)
	}
	for _, w := range []string{"docker came with this run", "no windows"} {
		if !slices.ContainsFunc(rep.Warnings, func(s string) bool { return strings.HasPrefix(s, w) }) {
			t.Errorf("the warnings %q do not say %q", rep.Warnings, w)
		}
	}
	if !slices.Contains(rep.Remind, "claude is not signed in to ~/.claude-work") {
		t.Errorf("the reminders are %q", rep.Remind)
	}
}
