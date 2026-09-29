package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/hostcfg"
)

// fakeClock is the time of a test: a wait passes at once.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time        { return c.t }
func (c *fakeClock) Sleep(d time.Duration) { c.t = c.t.Add(d) }

// rig is a machine for the steps: a temporary tree the steps write into for
// real — the home, the clone with the templates of this tree, the state
// directory — and tables for the rest of the system and every command.
type rig struct {
	t       *testing.T
	root    string
	home    string
	clone   string
	state   string
	m       *Table
	in      *Install
	r       *Run
	clock   *fakeClock
	journal *Journal
}

// templates are the files of the tree the steps read from the clone.
var templates = []string{".env.example", "deploy/host.env.example",
	"deploy/systemd/aacpanel-agent@.service", "deploy/systemd/aacpanel-exec.service"}

type rigOption func(*rigSetup)

type rigSetup struct {
	answers map[string]string
	missing []Missing
	files   map[string]string // relative to the home
	mode    Mode
}

func answer(flag, value string) rigOption { return func(s *rigSetup) { s.answers[flag] = value } }
func lacking(m Missing) rigOption         { return func(s *rigSetup) { s.missing = append(s.missing, m) } }
func homeFile(rel, body string) rigOption { return func(s *rigSetup) { s.files[rel] = body } }
func mode(m Mode) rigOption               { return func(s *rigSetup) { s.mode = m } }

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	root := t.TempDir()
	g := &rig{t: t, root: root, home: filepath.Join(root, "home", "u")}
	g.clone = filepath.Join(g.home, "aacpanel")
	g.state = filepath.Join(root, "state")
	setup := &rigSetup{answers: map[string]string{
		"--host": "lab", "--locale": "C.UTF-8", "--claude": "/usr/bin/claude", "--state-dir": g.state,
	}, files: map[string]string{}, mode: Fresh}
	for _, o := range opts {
		o(setup)
	}
	for _, rel := range templates {
		raw, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		g.write(filepath.Join(g.clone, rel), string(raw), 0o644)
	}
	for rel, body := range setup.files {
		g.write(filepath.Join(g.home, rel), body, 0o644)
	}
	acct := Account{Name: "u", UID: os.Getuid(), GID: os.Getgid(), Home: g.home}
	g.m = &Table{Disk: root, Acct: acct, Vars: map[string]string{"HOME": g.home},
		Files: map[string]string{}, Stats: map[string]Stat{}, Cmds: map[string]Reply{}, Effects: map[string]func(Cmd) error{}}
	f := Facts{Account: acct, Clone: g.clone, Version: "3f1c2ab", InstallDir: filepath.Join(g.home, ".local", "state", "aacpanel-install"),
		StateDir: DefaultStateDir, LANPort: PortLAN, Mode: setup.mode}
	var findings []Finding
	for _, miss := range setup.missing {
		findings = append(findings, Finding{Mark: Stop, Missing: &miss})
	}
	s := NewSurvey(g.m, Inspection{Facts: f, Findings: findings}, &Run{Yes: true, Answers: setup.answers}, now)
	answerAll(t, s)
	man, err := OpenManifest(f.InstallDir)
	if err != nil {
		t.Fatal(err)
	}
	g.journal, err = OpenJournal(f.InstallDir, "install", now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.journal.Close() })
	g.clock = &fakeClock{t: now}
	g.in = &Install{S: s, M: g.m, Go: "go"}
	g.r = &Run{Manifest: man, Journal: g.journal, Shell: g.m, Clock: g.clock, Place: g.in.Place()}
	return g
}

func (g *rig) write(path, body string, perm os.FileMode) {
	g.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) read(path string) string {
	g.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		g.t.Fatal(err)
	}
	return string(raw)
}

func (g *rig) says(argv []string, out string) { g.m.Cmds[Command(argv[0], argv[1:]...)] = Says(out) }
func (g *rig) fails(argv []string, stderr string) {
	g.m.Cmds[Command(argv[0], argv[1:]...)] = FailsWith(stderr)
}

// lines are the lines of the manifest past its first, the installer's own
// directory, as "kind target meta".
func (g *rig) lines() []string {
	g.t.Helper()
	es, err := ReadManifest(g.r.Manifest.Path)
	if err != nil {
		g.t.Fatal(err)
	}
	var out []string
	for _, e := range es[1:] {
		out = append(out, strings.TrimSpace(e.Kind+" "+e.Target+" "+e.Meta))
	}
	return out
}

// ran are the commands the steps ran, those that only read left out when
// asked.
func (g *rig) ran(changing bool) []string {
	var out []string
	for _, c := range g.m.Ran {
		if changing && (strings.Contains(c, " is-") || strings.HasPrefix(c, "dpkg-query") || strings.Contains(c, " inspect ") ||
			strings.Contains(c, " logs ") || strings.Contains(c, "pg_isready")) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (g *rig) do(s *Step) error {
	g.t.Helper()
	return g.r.Do(s)
}

func (g *rig) step(id string) *Step {
	for _, s := range g.in.Steps() {
		if s.ID == id {
			return s
		}
	}
	for _, s := range []*Step{g.in.testDBStep()} {
		if s.ID == id {
			return s
		}
	}
	g.t.Fatalf("no step %s", id)
	return nil
}

// already tells whether a run of the step stopped at its check.
func (g *rig) already(s *Step) bool {
	g.t.Helper()
	var skipped bool
	sink := g.r.Sink
	g.r.Sink = func(e Event) {
		if e.Type == Closed {
			skipped = e.Already
		}
	}
	defer func() { g.r.Sink = sink }()
	if err := g.r.Do(s); err != nil {
		g.t.Fatalf("%s: %v", s.ID, err)
	}
	return skipped
}

func failedWith(t *testing.T, err error, diagnosis string) *Failed {
	t.Helper()
	var f *Failed
	if !errors.As(err, &f) {
		t.Fatalf("the step ended with %v, want a stop saying %q", err, diagnosis)
	}
	if !strings.Contains(f.Diagnosis, diagnosis) {
		t.Fatalf("the diagnosis is %q, want one saying %q", f.Diagnosis, diagnosis)
	}
	return f
}

// ---- S3 ----

func TestHostEnvIsStagedForTheRootPartWhenTheStateIsNotThereYet(t *testing.T) {
	g := newRig(t)
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(g.home, ".local/state/aacpanel-install/host.env.staged")
	if want := []string{"file " + staged + " created"}; !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	have := hostcfg.Parse([]byte(g.read(staged)))
	for k, v := range map[string]string{"AACP_HOST": "lab", "AACP_REPO": g.clone, "AACP_UNIX_USER": "u", "AACP_STATE_DIR": g.state,
		"AACP_LANG": "C.UTF-8", "AACP_TERMINAL": "", "DISPLAY": "", "AACP_CLAUDE": "/usr/bin/claude"} {
		if got, ok := have[k]; !ok || got != v {
			t.Errorf("%s = %q (there: %v), want %q", k, got, ok, v)
		}
	}
	// The template's comments stay; the commented terminal is set where it
	// stood, not appended.
	body := g.read(staged)
	if !strings.Contains(body, "# Description of THIS machine") || strings.Count(body, "AACP_TERMINAL=") != 1 {
		t.Errorf("the staged file lost the template's shape:\n%s", body)
	}
	if _, err := os.Stat(g.state); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the state directory was made without root")
	}
}

func TestHostEnvIsEditedInPlaceOnlyWhereTheAnswersDiffer(t *testing.T) {
	g := newRig(t, answer("--host", "lab2"))
	path := filepath.Join(g.state, "host.env")
	before := "# mine\nAACP_HOST=\"lab\"\nAACP_TERMINAL=\nAACP_LANG=C.UTF-8\nEXTRA=kept\n"
	g.write(path, before, 0o644)
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	after := g.read(path)
	for _, want := range []string{"# mine\n", "AACP_HOST=lab2\n", "AACP_TERMINAL=\n", "EXTRA=kept\n", "AACP_REPO=" + g.clone + "\n"} {
		if !strings.Contains(after, want) {
			t.Errorf("host.env has no %q:\n%s", want, after)
		}
	}
	if strings.Count(after, "AACP_TERMINAL") != 1 {
		t.Errorf("an empty key the answers keep was written twice:\n%s", after)
	}
	backup := filepath.Join(g.home, ".local/state/aacpanel-install/backups", strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "_")+".orig")
	if want := []string{"file " + path + " orig=" + backup}; !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	if g.read(backup) != before {
		t.Error("the backup is not the file as it was")
	}
	if !g.already(g.step("hostenv")) {
		t.Error("a second run changed host.env again")
	}
	// A key there but empty is an answer; a key missing is none.
	g.write(path, strings.Replace(g.read(path), "AACP_TERMINAL=\n", "", 1), 0o644)
	if g.in.hostEnvHolds() {
		t.Error("host.env without AACP_TERMINAL holds the answer of no windows")
	}
}

// ---- S4 ----

// rootMachine answers the commands of the root part the way a machine where
// root.sh went through does.
func (g *rig) rootMachine(lines ...string) []string {
	staged := filepath.Join(g.home, ".local/state/aacpanel-install/host.env.staged")
	argv := []string{"sudo", "-n", "bash", filepath.Join(g.clone, "deploy/install/root.sh"), "apply", "--user", "u", "--state", g.state, "--staged", staged}
	for _, p := range g.in.S.Packages() {
		argv = append(argv, "--package", p)
	}
	var out []string
	for _, l := range lines {
		out = append(out, "MANIFEST\t"+l)
	}
	g.says(argv, "root.sh: apt-get install -y tmux\n"+strings.Join(out, "\n")+"\n")
	g.m.Effects[Command(argv[0], argv[1:]...)] = func(Cmd) error {
		// What root.sh does: the directory, the description, linger, the unit.
		if err := os.MkdirAll(g.state, 0o755); err != nil {
			return err
		}
		raw, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		g.write(filepath.Join(g.state, "host.env"), string(raw), 0o644)
		unit, _ := g.in.collectorUnit()
		g.m.Files[collectorUnitPath] = string(unit)
		g.m.Stats["/var/lib/systemd/linger/u"] = Stat{}
		g.m.Stats[filepath.Join(g.state, "state.json")] = Stat{Mod: g.clock.Now()}
		g.says([]string{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, "enabled\n")
		g.says([]string{"systemctl", "is-active", "aacpanel-agent@u.service"}, "active\n")
		g.says([]string{"dpkg-query", "-W", "-f=${Status}", "tmux"}, "install ok installed")
		return nil
	}
	return argv
}

func TestTheRootPartRecordsWhatRootShSaysInItsOrder(t *testing.T) {
	g := newRig(t, lacking(Missing{Name: "tmux", Command: "sudo apt install tmux"}))
	g.fails([]string{"dpkg-query", "-W", "-f=${Status}", "tmux"}, "no packages found matching tmux")
	g.says([]string{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, "disabled\n")
	g.says([]string{"systemctl", "is-active", "aacpanel-agent@u.service"}, "inactive\n")
	root := g.rootMachine(
		"pkg\ttmux\tby-installer",
		"group\tdocker u\tby-installer",
		"dir\t"+g.state+"\tcreated",
		"file\t"+g.state+"/host.env\tcreated",
		"linger\tu\tby-installer",
		"sysunit\t/etc/systemd/system/aacpanel-agent@.service\tcreated sha=abc",
		"enabled\tsystem aacpanel-agent@u.service\tby-installer",
	)
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	if err := g.do(g.step("root")); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(g.home, ".local/state/aacpanel-install/host.env.staged")
	want := []string{
		"file " + staged + " created",
		"pkg tmux by-installer",
		"group docker u by-installer",
		"dir " + g.state + " created",
		"file " + g.state + "/host.env created",
		"linger u by-installer",
		"sysunit /etc/systemd/system/aacpanel-agent@.service created sha=abc",
		"enabled system aacpanel-agent@u.service by-installer",
	}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds\n%s\nwant\n%s", strings.Join(g.lines(), "\n"), strings.Join(want, "\n"))
	}
	if !slices.Contains(g.m.Ran, Command(root[0], root[1:]...)) {
		t.Errorf("root.sh ran as %q", g.m.Ran)
	}
	if _, err := os.Stat(staged); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the staged host.env stayed after the root part put it in place")
	}
	if !g.in.viaGroup || !g.in.agentStarted {
		t.Error("the run did not learn that docker goes through the new group and the collector started")
	}
	if got := g.in.docker("compose", "up", "-d"); !slices.Equal(got, []string{"sg", "docker", "-c", "docker compose up -d"}) {
		t.Errorf("docker after a new group is %q", got)
	}
	if !g.already(g.step("root")) {
		t.Error("a second run of the root part asked for root again")
	}
}

func TestANoToTheRootCommandStopsWithTheCommandForAnAdministrator(t *testing.T) {
	g := newRig(t)
	g.says([]string{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, "disabled\n")
	var asked Handover
	g.r.Hand = func(h Handover) error { asked = h; return ErrDeclined }
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	f := failedWith(t, g.do(g.step("root")), "the root part did not run")
	script := filepath.Join(g.clone, "deploy/install/root.sh")
	if asked.Title != "Root command" || asked.Script != script || asked.Argv[0] != "sudo" || asked.Argv[1] != "bash" {
		t.Errorf("the frame was asked for %+v", asked)
	}
	if !strings.Contains(strings.Join(f.Fix, "\n"), "sudo bash "+script+" apply --user u --state "+g.state) {
		t.Errorf("the stop does not name the command for an administrator: %q", f.Fix)
	}
}

func TestSudoWithoutATerminalNamesTheAdministratorsCommand(t *testing.T) {
	g := newRig(t)
	g.says([]string{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, "disabled\n")
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	argv := g.rootMachine()
	g.fails(argv, "sudo: a password is required")
	f := failedWith(t, g.do(g.step("root")), "sudo needs a password")
	if fix := strings.Join(f.Fix, " "); !strings.Contains(fix, "sudo bash ") || !strings.HasSuffix(fix, "and then run ./install.sh again.") {
		t.Errorf("the fix is %q", f.Fix)
	}
	// An uninstall that stops there goes on by uninstall: install would put
	// back what it took.
	g.r.Again = "./install.sh uninstall"
	remove := []string{"sudo", "-n", "bash", g.in.Place().RootScript(), "remove", "--user", "u"}
	g.fails(remove, "sudo: a password is required")
	f = failedWith(t, g.r.AsRoot("Root command", "Removes the collector's unit.", remove[4:]...), "sudo needs a password")
	if fix := strings.Join(f.Fix, " "); !strings.HasSuffix(fix, "and then run ./install.sh uninstall again.") {
		t.Errorf("the fix of an uninstall is %q", f.Fix)
	}
}

func TestAStopOfRootShIsTheDiagnosis(t *testing.T) {
	g := newRig(t)
	g.says([]string{"systemctl", "is-enabled", "aacpanel-agent@u.service"}, "disabled\n")
	if err := g.do(g.step("hostenv")); err != nil {
		t.Fatal(err)
	}
	argv := g.rootMachine()
	delete(g.m.Effects, Command(argv[0], argv[1:]...))
	g.m.Cmds[Command(argv[0], argv[1:]...)] = Reply{Fail: "stop: " + g.state + " exists and belongs to root — left from another install. sudo chown -R u:u " + g.state + " (or remove it), then run again.", Code: 1}
	failedWith(t, g.do(g.step("root")), "belongs to root — left from another install")
	if len(g.lines()) != 1 {
		t.Errorf("a stop of root.sh left lines: %q", g.lines())
	}
}

func TestTheCollectorUnitOfGoIsTheOneRootShRenders(t *testing.T) {
	g := newRig(t)
	unit, err := g.in.collectorUnit()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EnvironmentFile=-" + g.state + "/host.env\n", "ReadWritePaths=" + g.state + "\n"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("the unit for %s has no %q", g.state, want)
		}
	}
	shipped := g.read(filepath.Join("..", "..", "deploy/systemd/aacpanel-agent@.service"))
	if got := string(renderUnit([]byte(shipped), DefaultStateDir, "EnvironmentFile=-%s/host.env", "ReadWritePaths=%s")); got != shipped {
		t.Error("the unit at the default place is not the shipped one byte for byte")
	}
	out := dryRoot(t, "apply", "--user", "nobody", "--state", g.state)
	if !strings.Contains(out, "sha="+sha(unit)) {
		t.Errorf("root.sh renders another unit than Go does:\n%s", out)
	}
}

// ---- S5 ----

func TestTheUserManagerIsReachedWithoutALogin(t *testing.T) {
	g := newRig(t)
	uid := strconv.Itoa(os.Getuid())
	g.says([]string{"systemctl", "--user", "is-system-running"}, "degraded\n")
	g.m.Stats["/run/user/"+uid+"/bus"] = Stat{Mode: fs.ModeSocket | 0o666}
	if err := g.do(g.step("usermgr")); err != nil {
		t.Fatal(err)
	}
	if g.r.Env["XDG_RUNTIME_DIR"] != "/run/user/"+uid || g.r.Env["DBUS_SESSION_BUS_ADDRESS"] != "unix:path=/run/user/"+uid+"/bus" {
		t.Errorf("the run's environment is %v", g.r.Env)
	}
	if env := g.m.Envs[len(g.m.Envs)-1]; !slices.Contains(env, "XDG_RUNTIME_DIR=/run/user/"+uid) {
		t.Errorf("a command after the step got %q", env)
	}
	g.m.Vars["XDG_RUNTIME_DIR"] = "/run/user/" + uid
	if !g.already(g.step("usermgr")) {
		t.Error("a machine with its manager up did the step again")
	}
	g.says([]string{"systemctl", "--user", "is-system-running"}, "offline\n")
	failedWith(t, g.do(g.step("usermgr")), "the user systemd manager did not come up after linger: systemctl status user@"+uid)
}

// ---- S6 ----

func TestTheEnvFileIsMadeFromTheTemplateWithItsSecrets(t *testing.T) {
	g := newRig(t)
	g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
	if err := g.do(g.step("envfile")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(g.clone, ".env")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the .env is %v, %v; want 0600", st, err)
	}
	body := g.read(path)
	env := hostcfg.Parse([]byte(body))
	for _, k := range []string{"AACP_SECRET", "AACP_DB_PASSWORD"} {
		if len(env[k]) < 48 || strings.Count(body, k+"=") != 1 {
			t.Errorf("%s is %q, written %d times", k, env[k], strings.Count(body, k+"="))
		}
	}
	// A commented line of the template is set where it stands.
	if !strings.Contains(body, "\nAACP_UID="+strconv.Itoa(os.Getuid())+"\n") || strings.Contains(body, "#AACP_UID=") {
		t.Errorf("AACP_UID was not set on the template's commented line:\n%s", body)
	}
	want := []string{"file " + path + " created data", "envkey AACP_SECRET generated", "envkey AACP_DB_PASSWORD generated"}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	g.journal.Close()
	log := g.read(g.journal.Path)
	for _, k := range []string{"AACP_SECRET", "AACP_DB_PASSWORD"} {
		if strings.Contains(log, env[k]) {
			t.Errorf("the journal holds %s:\n%s", k, log)
		}
	}
	// The secrets stay: a second run makes none.
	secret := env["AACP_SECRET"]
	g.says([]string{"docker", "compose", "config", "-q"}, "")
	if !g.already(g.step("envfile")) {
		t.Error("a second run changed the .env")
	}
	if hostcfg.Parse([]byte(g.read(path)))["AACP_SECRET"] != secret {
		t.Error("the cookie key was made again")
	}
}

func (g *rig) composeConfig(uid, gid int, execDir string) {
	g.says([]string{"docker", "compose", "config", "--format", "json"},
		`{"services":{"aacpanel":{"user":"`+strconv.Itoa(uid)+":"+strconv.Itoa(gid)+`","volumes":[`+
			`{"type":"bind","source":"/var/lib/aacpanel","target":"/host-state"},`+
			`{"type":"bind","source":"`+execDir+`","target":"/run/aacpanel-exec"}]}}}`)
}

func TestTheEnvFileKeepsWhatIsThereAndAddsWhatIsNot(t *testing.T) {
	g := newRig(t)
	path := filepath.Join(g.clone, ".env")
	// A key set, a key empty, a key commented, a key missing altogether.
	g.write(path, "AACP_SECRET=keepme\nAACP_DB_PASSWORD=\n#AACP_UID=1000\nAACP_UID=77\nMINE=1\n", 0o644)
	g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
	if err := g.do(g.step("envfile")); err != nil {
		t.Fatal(err)
	}
	body := g.read(path)
	env := hostcfg.Parse([]byte(body))
	if env["AACP_SECRET"] != "keepme" || len(env["AACP_DB_PASSWORD"]) != 48 || env["MINE"] != "1" {
		t.Errorf("the .env reads %v", env)
	}
	if !strings.Contains(body, "#AACP_UID=1000\nAACP_UID="+strconv.Itoa(os.Getuid())+"\n") {
		t.Errorf("the live AACP_UID was not the one set, or the commented one went:\n%s", body)
	}
	if !strings.Contains(body, "\nAACP_EXEC_DIR=/run/user/") {
		t.Errorf("a key the file lacks was not added:\n%s", body)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("the .env is %o", st.Mode().Perm())
	}
	if lines := g.lines(); len(lines) != 2 || !strings.HasPrefix(lines[0], "file "+path+" orig=") || lines[1] != "envkey AACP_DB_PASSWORD generated" {
		t.Errorf("the manifest holds %q", lines)
	}
}

func TestAWayInTurnedOffGoesBackToTheTemplate(t *testing.T) {
	g := newRig(t, mode(Adopt),
		answer("--kit", "+lan,-domain"), answer("--lan-addr", "192.168.1.20"), answer("--passkey-home", "localhost"),
		func(s *rigSetup) { s.answers["--lan-cert"] = "plain" },
		homeFile("aacpanel/.env", "AACP_SECRET=s\nAACP_DB_PASSWORD=p\nAACP_PUBLIC_URL=https://panel.example.org\n"+
			"AACP_BIND=172.17.0.1\nAACP_RP_ID=panel.example.org\nAACP_RP_ORIGINS=https://panel.example.org\n"),
		homeFile(".claude/settings.json", `{"hooks":{"PreToolUse":[{"hooks":[{"command":"python3 x/agent/ask-hook.py"}]}]}}`))
	g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
	if err := g.do(g.step("envfile")); err != nil {
		t.Fatal(err)
	}
	body := g.read(filepath.Join(g.clone, ".env"))
	env := hostcfg.Parse([]byte(body))
	if _, ok := env["AACP_PUBLIC_URL"]; ok || !strings.Contains(body, "\n#AACP_PUBLIC_URL=https://panel.example\n") {
		t.Errorf("the domain's address did not go back to the template's line:\n%s", body)
	}
	for k, v := range map[string]string{"AACP_BIND": "192.168.1.20", "AACP_SECURE": "0", "AACP_LAN_BIND": "192.168.1.20",
		"AACP_RP_ID": "localhost", "AACP_RP_ORIGINS": "http://localhost:8776"} {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	if _, ok := env["AACP_LAN_URL"]; ok {
		t.Error("plain http got the TLS listener's address")
	}
}

// TestTheRareSettingsAreWrittenOnlyWhenTuned: the contact for pushes, the
// length of a sign-in and the probes keep what the files hold unless the
// person chose to tune them.
func TestTheRareSettingsAreWrittenOnlyWhenTuned(t *testing.T) {
	for _, tuned := range []bool{false, true} {
		var opts []rigOption
		if tuned {
			opts = append(opts, answer("--login-life", "30m/12h"), answer("--push-contact", "mailto:u@example.org"))
		}
		g := newRig(t, opts...)
		g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
		if err := g.do(g.step("envfile")); err != nil {
			t.Fatal(err)
		}
		env := hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env"))))
		_, idle := env["AACP_SESSION_IDLE"]
		_, push := env["AACP_PUSH_CONTACT"]
		probes := slices.ContainsFunc(g.in.S.HostEnv(), func(v Var) bool { return v.Key == "AACP_PROBE_PORTS" })
		if !tuned && (idle || push || probes) {
			t.Errorf("untuned, the rare settings were written: idle %v, push %v, probes %v", idle, push, probes)
		}
		if tuned && (env["AACP_SESSION_IDLE"] != "30m" || env["AACP_SESSION_MAX"] != "12h" || env["AACP_PUSH_CONTACT"] != "mailto:u@example.org") {
			t.Errorf("tuned, the .env reads %v", env)
		}
	}
}

func TestTheEnvFileCheckReadsWhomComposeRunsAs(t *testing.T) {
	g := newRig(t)
	g.composeConfig(1000+os.Getuid()+1, os.Getgid(), "/run/user/"+strconv.Itoa(os.Getuid())+"/aacpanel-exec")
	failedWith(t, g.do(g.step("envfile")), "the executor's socket would not open to it")
	g.composeConfig(os.Getuid(), os.Getgid(), "/run/user/1/aacpanel-exec")
	failedWith(t, g.do(g.step("envfile")), "as the executor's directory")
	g.fails([]string{"docker", "compose", "config", "--format", "json"}, "required variable AACP_SECRET is missing a value")
	failedWith(t, g.do(g.step("envfile")), "docker compose config fails: required variable AACP_SECRET")
}

// ---- S7 ----

func (g *rig) goBuild(body string) []string {
	bin := filepath.Join(g.home, "bin", "aacpanel-exec")
	argv := []string{"go", "build", "-trimpath", "-buildvcs=false", "-o", bin + ".new", "./cmd/aacpanel-exec"}
	g.says(argv, "")
	g.m.Effects[Command(argv[0], argv[1:]...)] = Writes(bin+".new", body)
	g.says([]string{bin, "-list"}, "container.start\nsession.open\n")
	return argv
}

func TestTheExecutorIsReplacedByARenameAndOnlyWhenItChanged(t *testing.T) {
	g := newRig(t)
	bin := filepath.Join(g.home, "bin", "aacpanel-exec")
	g.goBuild("first")
	if err := g.do(g.step("exec-build")); err != nil {
		t.Fatal(err)
	}
	if g.read(bin) != "first" || !g.in.execChanged {
		t.Fatal("the first build is not in place")
	}
	want := []string{"dir " + filepath.Join(g.home, "bin") + " created", "file " + bin + " created sha=" + sha([]byte("first"))}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	// A holder of a stream session runs the old file: it must keep it.
	holder := filepath.Join(g.root, "holder")
	if err := os.Link(bin, holder); err != nil {
		t.Fatal(err)
	}
	g.in.execChanged = false
	g.goBuild("first")
	if err := g.do(g.step("exec-build")); err != nil {
		t.Fatal(err)
	}
	if g.in.execChanged || len(g.lines()) != 2 {
		t.Errorf("the same build replaced the binary: changed %v, manifest %q", g.in.execChanged, g.lines())
	}
	if _, err := os.Stat(bin + ".new"); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the build of the same binary stayed beside it")
	}
	g.goBuild("second")
	if err := g.do(g.step("exec-build")); err != nil {
		t.Fatal(err)
	}
	if g.read(bin) != "second" || g.read(holder) != "first" {
		t.Errorf("the new build went over the running file: the holder reads %q", g.read(holder))
	}
	if len(g.lines()) != 2 {
		t.Errorf("a rebuild of the installer's own binary added lines: %q", g.lines())
	}
}

func TestTheExecutorsBuildAndListAreDiagnosed(t *testing.T) {
	g := newRig(t)
	argv := g.goBuild("x")
	delete(g.m.Effects, Command(argv[0], argv[1:]...))
	g.fails(argv, "write /home/u/.cache/go-build/ab: no space left on device")
	failedWith(t, g.do(g.step("exec-build")), "no room left on the disk")
	g.fails(argv, "go: github.com/x/y: Get \"https://proxy.golang.org/x\": dial tcp: lookup proxy.golang.org: no such host")
	failedWith(t, g.do(g.step("exec-build")), "did not download from proxy.golang.org")
	g.goBuild("x")
	g.says([]string{filepath.Join(g.home, "bin", "aacpanel-exec"), "-list"}, "container.start\nrocket.launch\n")
	failedWith(t, g.do(g.step("exec-build")), `names "rocket.launch"`)
}

// ---- S8 ----

func (g *rig) execUp() {
	uid := os.Getuid()
	dir := "/run/user/" + strconv.Itoa(uid) + "/aacpanel-exec"
	g.m.Stats[dir] = Stat{Mode: fs.ModeDir | 0o700, UID: uid}
	g.m.Stats[dir+"/sock"] = Stat{Mode: fs.ModeSocket | 0o600, UID: uid}
}

func TestTheExecutorsUnitIsInstalledEnabledAndStarted(t *testing.T) {
	g := newRig(t)
	for _, c := range [][]string{{"daemon-reload"}, {"enable", "aacpanel-exec.service"}, {"restart", "aacpanel-exec.service"}} {
		g.says(append([]string{"systemctl", "--user"}, c...), "")
	}
	g.says([]string{"systemctl", "--user", "is-enabled", "aacpanel-exec.service"}, "disabled\n")
	g.says([]string{"systemctl", "--user", "is-active", "aacpanel-exec.service"}, "inactive\n")
	g.execUp()
	if err := g.do(g.step("exec-unit")); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(g.home, ".config/systemd/user/aacpanel-exec.service")
	if !strings.Contains(g.read(unit), "EnvironmentFile=-"+g.state+"/host.env\n") {
		t.Errorf("the unit does not read the host description of %s", g.state)
	}
	want := []string{
		"dir " + filepath.Join(g.home, ".config") + " created",
		"dir " + filepath.Join(g.home, ".config/systemd") + " created",
		"dir " + filepath.Join(g.home, ".config/systemd/user") + " created",
		"userunit " + unit + " created",
		"enabled user aacpanel-exec.service by-installer",
		"dir " + filepath.Join(g.home, ".config/systemd/user/default.target.wants") + " created",
	}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds\n%s\nwant\n%s", strings.Join(g.lines(), "\n"), strings.Join(want, "\n"))
	}
	wantRan := []string{"systemctl --user daemon-reload", "systemctl --user enable aacpanel-exec.service", "systemctl --user restart aacpanel-exec.service"}
	if got := g.ran(true); !slices.Equal(got, wantRan) {
		t.Errorf("ran %q, want %q", got, wantRan)
	}
	g.says([]string{"systemctl", "--user", "is-enabled", "aacpanel-exec.service"}, "enabled\n")
	g.says([]string{"systemctl", "--user", "is-active", "aacpanel-exec.service"}, "active\n")
	if !g.already(g.step("exec-unit")) {
		t.Error("an executor in place was restarted")
	}
}

func TestAnExecutorWithoutItsSocketIsDiagnosedByItsJournal(t *testing.T) {
	g := newRig(t)
	for _, c := range [][]string{{"daemon-reload"}, {"enable", "aacpanel-exec.service"}, {"restart", "aacpanel-exec.service"}} {
		g.says(append([]string{"systemctl", "--user"}, c...), "")
	}
	g.says([]string{"journalctl", "--user", "-u", "aacpanel-exec.service", "-n", "30", "--no-pager"},
		"systemd[1]: aacpanel-exec.service: Failed at step NAMESPACE spawning /home/u/bin/aacpanel-exec: No such file or directory\n"+
			"systemd[1]: aacpanel-exec.service: Main process exited, code=exited, status=226/NAMESPACE\n")
	f := failedWith(t, g.do(g.step("exec-unit")), "226/NAMESPACE")
	if len(f.Tail) != 2 {
		t.Errorf("the tail is %q", f.Tail)
	}
	uid := os.Getuid()
	dir := "/run/user/" + strconv.Itoa(uid) + "/aacpanel-exec"
	g.m.Stats[dir] = Stat{Mode: fs.ModeDir | 0o755, UID: 0}
	g.m.Stats[dir+"/sock"] = Stat{Mode: fs.ModeSocket | 0o600, UID: uid}
	failedWith(t, g.do(g.step("exec-unit")), "docker made it as root before the executor ran")
}

// ---- S9 ----

func (g *rig) stackUp() {
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s3cr3t-cookie-key\nAACP_DB_PASSWORD=db-pass-123\n", 0o600)
	g.says([]string{"docker", "compose", "config", "--images"}, "tecnativa/docker-socket-proxy:0.3.0\npostgres:18-alpine\naacpanel-aacpanel\n")
	g.says([]string{"docker", "image", "inspect", "--format", "{{.Id}}", "tecnativa/docker-socket-proxy:0.3.0"}, "sha256:1\n")
	g.says([]string{"docker", "compose", "build", "aacpanel"}, "#13 naming to docker.io/library/aacpanel-aacpanel done\n")
	g.says([]string{"docker", "compose", "up", "-d"}, " Container aacpanel  Started\n")
	g.fails([]string{"docker", "volume", "inspect", DBVolume}, "Error: No such volume: "+DBVolume)
	g.fails([]string{"docker", "volume", "inspect", TSVolume}, "Error: No such volume: "+TSVolume)
	g.says([]string{"docker", "inspect", "-f", "{{.State.Health.Status}}", "aacpanel-db"}, "healthy\n")
	// A log line may carry the DSN whole: the journal must not.
	g.says([]string{"docker", "compose", "logs", "--no-color", "aacpanel"},
		"aacpanel  | store: postgres://aacpanel:db-pass-123@aacpanel-db:5432/aacpanel\naacpanel  | store: connected\n")
	g.m.Web = map[string]int{"http://127.0.0.1:8776/healthz": 200, "http://127.0.0.1:8777/api/profiles": 200}
}

func TestTheStackIsRecordedBeforeItComesUp(t *testing.T) {
	g := newRig(t)
	g.stackUp()
	if err := g.do(g.step("compose")); err != nil {
		t.Fatal(err)
	}
	// The image the machine had stays its own; the one pulled now is the
	// install's, and uninstall takes it away with the stack. up makes the
	// volume of the tailnet node without the node: it is the install's too,
	// and a second run does not take it for an install by hand.
	want := []string{"compose aacpanel project", "volume " + DBVolume + " data", "volume " + TSVolume + " data",
		"image aacpanel-aacpanel local", "image postgres:18-alpine pulled"}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	wantRan := []string{"docker compose config --images", "docker compose build aacpanel", "docker compose up -d"}
	if got := g.ran(true); !slices.Equal(got, wantRan) {
		t.Errorf("ran %q, want %q", got, wantRan)
	}
	// An image built from the cache keeps its id only without the
	// attestation BuildKit dates: else up recreates the panel every run.
	for _, c := range g.m.Given {
		if strings.Join(c.Argv, " ") == "docker compose build aacpanel" && !slices.Contains(c.Env, "BUILDX_NO_DEFAULT_ATTESTATIONS=1") {
			t.Errorf("the image is built with the attestation of its time: %q", c.Env)
		}
	}
	// Again: compose keeps what holds, and the manifest takes no line twice.
	g.says([]string{"docker", "volume", "inspect", DBVolume}, "[]")
	g.says([]string{"docker", "volume", "inspect", TSVolume}, "[]")
	g.says([]string{"docker", "image", "inspect", "--format", "{{.Id}}", "postgres:18-alpine"}, "sha256:2\n")
	if err := g.do(g.step("compose")); err != nil {
		t.Fatal(err)
	}
	if len(g.lines()) != len(want) {
		t.Errorf("a second run added lines: %q", g.lines())
	}
	for _, left := range g.in.unowned(g.r) {
		if left.kind == Volume {
			t.Errorf("a second run would take over the volume %s as found", left.target)
		}
	}
	// A build past a vulnerability is the person's knowing choice, said so.
	g.in.SkipVulncheck = true
	g.says([]string{"docker", "compose", "build", "--build-arg", "SKIP_VULNCHECK=1", "aacpanel"}, "")
	if err := g.do(g.step("compose")); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(g.m.Ran, "docker compose build --build-arg SKIP_VULNCHECK=1 aacpanel") {
		t.Errorf("--skip-vulncheck did not reach the build: %q", g.m.Ran)
	}
}

func TestTheStackIsDiagnosed(t *testing.T) {
	g := newRig(t)
	g.stackUp()
	g.fails([]string{"docker", "compose", "up", "-d"},
		"Error response from daemon: driver failed programming external connectivity: Bind for 127.0.0.1:8776 failed: port is already allocated")
	failedWith(t, g.do(g.step("compose")), "a port the panel publishes is taken")
	g.stackUp()
	delete(g.m.Web, "http://127.0.0.1:8776/healthz")
	g.says([]string{"docker", "compose", "logs", "--no-color", "--tail", "20", "aacpanel"}, "aacpanel  | store: dial tcp aacpanel-db:5432: connection refused\n")
	f := failedWith(t, g.do(g.step("compose")), "did not answer with the store connected in 120 s")
	if len(f.Tail) != 1 || !strings.Contains(f.Tail[0], "connection refused") {
		t.Errorf("the tail is %q", f.Tail)
	}
	if elapsed := g.clock.Now().Sub(now); elapsed < 120*time.Second {
		t.Errorf("the wait gave up after %s", elapsed)
	}
}

// ---- S10 ----

func TestTheAppRolePasswordGoesThroughTheEnvironmentOnly(t *testing.T) {
	g := newRig(t)
	g.stackUp()
	script := []string{"bash", "deploy/create-app-role.sh"}
	g.says(script, "The role monitor_app is ready.\n")
	g.says([]string{"docker", "compose", "up", "-d", "aacpanel"}, "")
	if err := g.do(g.step("approle")); err != nil {
		t.Fatal(err)
	}
	env := hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env"))))
	pw := env["AACP_APP_PASSWORD"]
	if env["AACP_APP_ROLE"] != "monitor_app" || len(pw) != 48 {
		t.Fatalf("the .env holds role %q, password %q", env["AACP_APP_ROLE"], pw)
	}
	i := slices.Index(g.m.Ran, Command(script[0], script[1:]...))
	if i < 0 || !slices.Contains(g.m.Envs[i], "AACP_APP_PASSWORD="+pw) {
		t.Fatalf("the script did not get the password in its environment: %q", g.m.Envs[max(i, 0)])
	}
	for _, c := range g.m.Ran {
		if strings.Contains(c, pw) {
			t.Errorf("the password is in the arguments of %q", c)
		}
	}
	want := []string{"dbrole monitor_app created", "envkey AACP_APP_ROLE generated", "envkey AACP_APP_PASSWORD generated"}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	if got := g.ran(true); !slices.Equal(got[len(got)-2:], []string{"bash deploy/create-app-role.sh", "docker compose up -d aacpanel"}) {
		t.Errorf("ran %q", got)
	}
	g.journal.Close()
	if log := g.read(g.journal.Path); strings.Contains(log, pw) || strings.Contains(log, "db-pass-123") {
		t.Errorf("the journal holds a password:\n%s", log)
	}
}

func TestARefusedPasswordStopsTheAppRole(t *testing.T) {
	g := newRig(t)
	g.stackUp()
	g.says([]string{"bash", "deploy/create-app-role.sh"}, "")
	g.says([]string{"docker", "compose", "up", "-d", "aacpanel"}, "")
	g.says([]string{"docker", "compose", "logs", "--no-color", "aacpanel"},
		"aacpanel  | store: connected\naacpanel  | FATAL: password authentication failed for user \"monitor_app\"\n")
	failedWith(t, g.do(g.step("approle")), "the panel's password was refused 1 time")
}

// ---- S10a ----

func TestTheTestDatabaseIsAContainerOfItsOwnWithItsDSNInTheEnvFile(t *testing.T) {
	g := newRig(t, answer("--kit", "+testdb"))
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\n", 0o600)
	g.fails([]string{"docker", "inspect", testDBName}, "Error: No such object: "+testDBName)
	run := []string{"docker", "run", "-d", "--restart", "unless-stopped", "--name", testDBName, "-p", "127.0.0.1:55432:5432",
		"-e", "POSTGRES_USER=aacpanel", "-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_DB=aacpanel", "postgres:18-alpine"}
	g.says(run, "4c1d\n")
	g.says([]string{"docker", "exec", testDBName, "pg_isready", "-q"}, "")
	if !slices.ContainsFunc(g.in.Steps(), func(s *Step) bool { return s.ID == "testdb" }) {
		t.Fatal("the kit with the test database has no step for it")
	}
	if err := g.do(g.step("testdb")); err != nil {
		t.Fatal(err)
	}
	dsn := hostcfg.Parse([]byte(g.read(filepath.Join(g.clone, ".env"))))["AACP_TEST_DSN"]
	pw, _ := urlPassword(dsn)
	if len(pw) != 48 || !strings.HasPrefix(dsn, "postgres://aacpanel:") || !strings.HasSuffix(dsn, "@127.0.0.1:55432/aacpanel?sslmode=disable") {
		t.Fatalf("the DSN is %q", dsn)
	}
	i := slices.Index(g.m.Ran, Command(run[0], run[1:]...))
	if i < 0 || !slices.Contains(g.m.Envs[i], "POSTGRES_PASSWORD="+pw) {
		t.Error("the container did not get its password through the environment")
	}
	want := []string{"image " + testDBImage + " pulled", "testdb " + testDBName + " created", "envkey AACP_TEST_DSN generated"}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	g.journal.Close()
	if strings.Contains(g.read(g.journal.Path), pw) {
		t.Error("the journal holds the password of the test database")
	}
}

// ---- S13a ----

func TestTheCollectorRestartsOnANewTreeAndNotAfterItsFirstStart(t *testing.T) {
	g := newRig(t)
	check := []string{"python3", "importcheck.py"}
	g.says(check, "")
	g.in.agentStarted = true
	if err := g.do(g.step("collector-restart")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.lines(), []string{"rev 3f1c2ab"}) || slices.ContainsFunc(g.m.Ran, func(c string) bool { return strings.HasPrefix(c, "sudo") }) {
		t.Errorf("a collector started on this tree was restarted: manifest %q, ran %q", g.lines(), g.m.Ran)
	}
	if !g.already(g.step("collector-restart")) {
		t.Error("the same tree restarted the collector")
	}
	g.in.agentStarted = false
	g.in.S.Facts.Version = "9e8d7c6"
	restart := []string{"sudo", "-n", "bash", filepath.Join(g.clone, "deploy/install/root.sh"), "restart-agent", "--user", "u"}
	g.says(restart, "root.sh: systemctl restart aacpanel-agent@u.service\n")
	g.m.Effects[Command(restart[0], restart[1:]...)] = func(Cmd) error {
		g.m.Stats[filepath.Join(g.state, "state.json")] = Stat{Mod: g.clock.Now().Add(5 * time.Second)}
		return nil
	}
	if err := g.do(g.step("collector-restart")); err != nil {
		t.Fatal(err)
	}
	asked := fmt.Sprintf("rev 9e8d7c6 asked=%d", g.clock.Now().Unix())
	if n := len(g.lines()); !slices.Contains(g.m.Ran, Command(restart[0], restart[1:]...)) || g.lines()[n-2] != asked || g.lines()[n-1] != "rev 9e8d7c6" {
		t.Errorf("a new tree: ran %q, manifest %q", g.m.Ran, g.lines())
	}
	if !g.already(g.step("collector-restart")) {
		t.Error("a collector restarted on this tree was restarted again")
	}
	// A restart that did not go through — said no to, or cut short — leaves
	// the collector older than the ask, and the next run restarts it; one an
	// administrator ran after the stop leaves it younger, and the run goes on.
	g.in.S.Facts.Version = "2222222"
	g.fails(restart, "sudo: a password is required")
	if err := g.do(g.step("collector-restart")); err == nil {
		t.Fatal("a restart that failed passed")
	}
	now := g.clock.Now().Unix()
	g.says([]string{"systemctl", "show", "-p", "MainPID", "--value", "aacpanel-agent@u.service"}, "4242\n")
	g.m.Files["/proc/stat"] = fmt.Sprintf("cpu  1 2 3\nbtime %d\nprocesses 9\n", now-1000)
	for _, c := range []struct {
		started int64 // seconds after the boot
		done    bool
	}{{500, false}, {1000, true}, {2000, true}} {
		g.m.Files["/proc/4242/stat"] = fmt.Sprintf("4242 (python3 agent) S 1 4242 4242 0 -1 4194560 1 0 0 0 0 0 0 0 20 0 1 0 %d 1 2\n", c.started*100)
		if done, _ := g.step("collector-restart").Done(g.r); done != c.done {
			t.Errorf("a collector started %d s before the ask: done %v", 1000-c.started, done)
		}
	}
	g.says(restart, "root.sh: systemctl restart aacpanel-agent@u.service\n")
	g.in.S.Facts.Version = "1111111"
	g.fails(check, "!! the collector will not come up - these modules do not import:\n   agent.py: NameError: name 'x' is not defined")
	f := failedWith(t, g.do(g.step("collector-restart")), "a module of agent/ does not import")
	if !strings.Contains(strings.Join(f.Tail, "\n"), "agent.py: NameError") {
		t.Errorf("the tail is %q", f.Tail)
	}
}

func TestTheAppRoleAndTheTestDatabaseInPlaceArePassedBy(t *testing.T) {
	g := newRig(t, answer("--kit", "+testdb"))
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\nAACP_APP_ROLE=monitor_app\nAACP_APP_PASSWORD=a\n"+
		"AACP_TEST_DSN=postgres://aacpanel:t@127.0.0.1:55432/aacpanel?sslmode=disable\n", 0o600)
	g.says([]string{"docker", "exec", "aacpanel-db", "psql", "-U", "aacpanel", "-d", "aacpanel", "-tAc",
		"SELECT 1 FROM pg_roles WHERE rolname = 'monitor_app'"}, "1\n")
	g.says([]string{"docker", "compose", "logs", "--no-color", "aacpanel"}, "aacpanel  | store: connected\n")
	g.says([]string{"docker", "inspect", "-f", "{{.State.Running}}", testDBName}, "true\n")
	g.says([]string{"docker", "exec", testDBName, "pg_isready", "-q"}, "")
	for _, id := range []string{"approle", "testdb"} {
		if !g.already(g.step(id)) {
			t.Errorf("%s in place was done again", id)
		}
	}
	if len(g.lines()) != 0 {
		t.Errorf("steps in place recorded %q", g.lines())
	}
	// A refused password since the start is a role that parted from .env.
	g.says([]string{"docker", "compose", "logs", "--no-color", "aacpanel"}, "FATAL: password authentication failed for user \"monitor_app\"\n")
	if done, _ := g.step("approle").Done(g.r); done {
		t.Error("a role whose password is refused counts as done")
	}
}
