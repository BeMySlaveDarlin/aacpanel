package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
)

// Install is what the steps of a run share: the answers, what the check
// found of the machine, and what one step learns for the steps after it.
type Install struct {
	S *Survey
	M Machine
	// Go builds the executor: the Go install.sh keeps in Cache, or the
	// developer's own under go run, when Cache is empty.
	Go, Cache string
	// Local is the panel's local listener the map is made through; empty
	// is where compose publishes it. Sock is the executor's socket; empty
	// is where the executor makes it.
	Local, Sock string
	// SkipVulncheck builds the image without the check of its dependencies
	// against published vulnerabilities: the person's knowing choice when a
	// vulnerability published after the release stops the build.
	SkipVulncheck bool
	// Update makes the run an update: the pulled images fresh first.
	// UpdatedFrom is the commit the clone was on before the update moved
	// it, empty when it did not.
	Update      bool
	UpdatedFrom string

	viaGroup     bool      // docker goes through sg: the root part added the group this run
	agentStarted bool      // the root part enabled the collector this run, on this tree
	agentUnit    bool      // the root part put a new unit of the collector in place
	hostChanged  bool      // host.env was edited in place, under a running collector
	execChanged  bool      // the executor's binary was replaced
	restarted    time.Time // when the collector was restarted
}

// The steps of the machine, in the order a run takes them.
func (in *Install) Steps() []*Step {
	var steps []*Step
	if in.f().Mode != Fresh {
		steps = append(steps, in.adoptStep())
	}
	if in.Update {
		steps = append(steps, in.updateStep())
	}
	steps = append(steps, in.hostEnvStep(), in.rootStep(), in.userManagerStep())
	if in.nativeInstalls() {
		steps = append(steps, in.claudeInstallStep())
	}
	steps = append(steps, in.envFileStep(), in.execBuildStep(), in.execUnitStep())
	if in.S.Has("gc") {
		steps = append(steps, in.gcTimerStep())
	}
	steps = append(steps, in.stackStep(), in.appRoleStep())
	if in.S.Has("testdb") {
		steps = append(steps, in.testDBStep())
	}
	if in.S.Has("tailscale") {
		steps = append(steps, in.tailscaleStep())
	}
	steps = append(steps, in.claudeStep())
	if in.mapAnswered() {
		steps = append(steps, in.mapStep())
	}
	return append(steps, in.collectorStep(), in.CheckStep(), in.SessionStep(), in.EnrollStep())
}

func (in *Install) f() Facts        { return in.S.Facts }
func (in *Install) home() string    { return in.f().Account.Home }
func (in *Install) clone() string   { return in.f().Clone }
func (in *Install) user() string    { return in.f().Account.Name }
func (in *Install) uid() int        { return in.f().Account.UID }
func (in *Install) state() string   { return in.S.StateDir() }
func (in *Install) runtime() string { return "/run/user/" + strconv.Itoa(in.uid()) }
func (in *Install) short(p string) string {
	return in.f().Short(p)
}

// Place is what the undos of this install need to know.
func (in *Install) Place() *Place { return &Place{Clone: in.clone(), User: in.user()} }

func (in *Install) hostEnvPath() string { return filepath.Join(in.state(), "host.env") }
func (in *Install) stagedPath() string  { return filepath.Join(in.f().InstallDir, "host.env.staged") }
func (in *Install) dotEnvPath() string  { return filepath.Join(in.clone(), ".env") }

// ask runs a command that only reads and gives what it printed, trimmed,
// whether it ended well or not: systemctl is-enabled says "disabled" with
// status 1, which is an answer and not a failure.
//
// It runs in the clone: docker compose finds its project there, whatever
// directory the installer was started from.
func (in *Install) ask(r *Run, argv ...string) string {
	out, _ := r.Exec(Cmd{Argv: argv, Dir: in.clone(), Limit: 30 * time.Second})
	return strings.TrimSpace(out)
}

// docker is how a step calls docker: straight, or through sg while the
// group the root part added this run is not the group of this session.
func (in *Install) docker(args ...string) []string {
	return in.withGroup(append([]string{"docker"}, args...))
}

func (in *Install) withGroup(argv []string) []string {
	if !in.viaGroup && !in.f().ViaGroup {
		return argv
	}
	return []string{"sg", "docker", "-c", shellLine(argv)}
}

// shellLine is argv as one line of the shell, each word quoted as it needs.
func shellLine(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9._/:=@%+-]+$`)

func shellQuote(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// backup keeps a file as the installer found it, once: the undo of the line
// that names the copy puts it back.
func (in *Install) backup(path string) (string, error) {
	dir := filepath.Join(in.f().InstallDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	to := filepath.Join(dir, strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "_")+".orig")
	if _, err := os.Stat(to); err == nil {
		return to, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return to, writeFile(to, raw, st.Mode().Perm())
}

// touch records the first change of a file the installer writes: made new,
// or found and kept aside for the undo. Later runs change their own file.
func (in *Install) touch(r *Run, kind Kind, path, extra string) error {
	if r.Recorded(kind, path) {
		return nil
	}
	meta := "created"
	if _, err := os.Stat(path); err == nil {
		orig, err := in.backup(path)
		if err != nil {
			return fmt.Errorf("%s was not kept aside before the change: %w", path, err)
		}
		meta = "orig=" + orig
	}
	return r.Record(kind, path, strings.TrimSpace(meta+" "+extra))
}

// makeDirs makes a directory and the parents it lacks, recording each one
// made from the top down, so the undo takes them away from the bottom up.
func (in *Install) makeDirs(r *Run, dir string) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || d == "/" || d == "." {
			break
		}
		missing = append(missing, d)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := r.MakeDir(missing[i], 0o755); err != nil {
			return err
		}
	}
	return nil
}

func sha(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ---- S3: host.env ----

// hostEnvHolds tells whether host.env in the state directory holds every
// key of the answers with its value. A key there but empty is an answer; a
// key missing is none.
func (in *Install) hostEnvHolds() bool {
	raw, err := in.M.ReadFile(in.hostEnvPath())
	if err != nil {
		return false
	}
	return len(changed(hostcfg.Parse(raw), in.S.HostEnv())) == 0
}

// stateIsMine tells whether the state directory is there and the person's:
// then host.env is edited in place, without root.
func (in *Install) stateIsMine() bool {
	st, err := in.M.Stat(in.state())
	return err == nil && st.Mode.IsDir() && st.UID == in.uid()
}

func (in *Install) hostEnvStep() *Step {
	return &Step{ID: "hostenv", Title: "Host description",
		Done: func(*Run) (bool, error) { return in.hostEnvHolds(), nil },
		Apply: func(r *Run) error {
			vars := in.S.HostEnv()
			raw, err := in.M.ReadFile(in.hostEnvPath())
			there := err == nil
			if !there {
				if raw, err = in.M.ReadFile(filepath.Join(in.clone(), "deploy", "host.env.example")); err != nil {
					return fmt.Errorf("the template of host.env is not in the clone: %w", err)
				}
			}
			out := Edit(raw, changed(hostcfg.Parse(raw), vars), nil, nil)
			if !in.stateIsMine() {
				// The root part makes the directory and puts the file in it.
				if err := r.Once(File, in.stagedPath(), "created"); err != nil {
					return err
				}
				if err := writeFile(in.stagedPath(), out, 0o644); err != nil {
					return err
				}
				r.Say(Pass, fmt.Sprintf("%s · %d keys, for the root part to put in place", in.short(in.stagedPath()), len(vars)))
				return nil
			}
			if err := in.touch(r, File, in.hostEnvPath(), ""); err != nil {
				return err
			}
			if err := writeFile(in.hostEnvPath(), out, 0o644); err != nil {
				return err
			}
			if err := removeIfThere(in.stagedPath()); err != nil {
				return err
			}
			in.hostChanged = there
			r.Say(Pass, fmt.Sprintf("%s · %d keys", in.hostEnvPath(), len(vars)))
			return nil
		},
		Verify: func(r *Run) error {
			path := in.hostEnvPath()
			if !in.stateIsMine() {
				path = in.stagedPath()
			}
			raw, err := in.M.ReadFile(path)
			if err != nil {
				return fail(path+" is not there after it was written", err)
			}
			if miss := changed(hostcfg.Parse(raw), in.S.HostEnv()); len(miss) > 0 {
				return &Failed{Diagnosis: fmt.Sprintf("%s does not hold %s after it was written", path, miss[0].Key)}
			}
			return nil
		},
		Undo: UndoKind,
	}
}

// ---- S4: the root part ----

// collectorUnit is the collector's unit for the state directory, as root.sh
// renders it: the shipped file byte for byte at the default place, else
// with the two lines that name the directory.
func (in *Install) collectorUnit() ([]byte, error) {
	raw, err := in.M.ReadFile(filepath.Join(in.clone(), "deploy", "systemd", "aacpanel-agent@.service"))
	if err != nil {
		return nil, err
	}
	return renderUnit(raw, in.state(), "EnvironmentFile=-%s/host.env", "ReadWritePaths=%s"), nil
}

// renderUnit points the lines of a unit that name the state directory at
// dir; systemd expands no variable in them.
func renderUnit(raw []byte, dir string, forms ...string) []byte {
	lines := strings.Split(string(raw), "\n")
	for i, l := range lines {
		for _, form := range forms {
			if l == fmt.Sprintf(form, DefaultStateDir) {
				lines[i] = fmt.Sprintf(form, dir)
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

const collectorUnitPath = "/etc/systemd/system/aacpanel-agent@.service"

func (in *Install) agent() string { return "aacpanel-agent@" + in.user() + ".service" }

func (in *Install) installed(r *Run, pkg string) bool {
	return in.ask(r, "dpkg-query", "-W", "-f=${Status}", pkg) == "install ok installed"
}

// rootDone tells whether the root part has nothing to do: the packages are
// in, the state directory is the person's and holds the answers, linger is
// on, the collector's unit is the one of this tree and runs.
func (in *Install) rootDone(r *Run) bool {
	for _, p := range in.S.Packages() {
		if !in.installed(r, p) {
			return false
		}
	}
	if !in.stateIsMine() || !in.hostEnvHolds() {
		return false
	}
	if _, err := in.M.Stat("/var/lib/systemd/linger/" + in.user()); err != nil {
		return false
	}
	want, err := in.collectorUnit()
	if err != nil {
		return false
	}
	if have, err := in.M.ReadFile(collectorUnitPath); err != nil || !bytes.Equal(have, want) {
		return false
	}
	return in.ask(r, "systemctl", "is-enabled", in.agent()) == "enabled" &&
		in.ask(r, "systemctl", "is-active", in.agent()) == "active"
}

// rootSays is what the frame of the root command says it does.
func (in *Install) rootSays() string {
	var parts []string
	if pkgs := in.S.Packages(); len(pkgs) > 0 {
		parts = append(parts, "Installs "+strings.Join(pkgs, " ")+" with apt")
	}
	parts = append(parts, "creates "+in.state()+" for "+in.user(),
		"installs the collector unit and enables linger. Nothing else runs as root.")
	s := strings.Join(parts, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

func (in *Install) rootStep() *Step {
	var began time.Time
	return &Step{ID: "root", Title: "Root part",
		Done: func(r *Run) (bool, error) { return in.rootDone(r), nil },
		Apply: func(r *Run) error {
			began = r.clock().Now()
			args := []string{"apply", "--user", in.user(), "--state", in.state()}
			if _, err := os.Stat(in.stagedPath()); err == nil {
				args = append(args, "--staged", in.stagedPath())
			}
			for _, p := range in.S.Packages() {
				args = append(args, "--package", p)
			}
			err := r.AsRoot("Root command", in.rootSays(), args...)
			for _, e := range r.ChangedInRun() {
				switch {
				case e.Kind == string(Group) && strings.HasPrefix(e.Target, "docker "):
					in.viaGroup = true
				case e.Kind == string(Enabled) && strings.HasPrefix(e.Target, "system "):
					in.agentStarted = true
				case e.Kind == string(SysUnit):
					in.agentUnit = true
				}
			}
			return err
		},
		Verify: func(r *Run) error {
			if !in.stateIsMine() {
				return &Failed{Diagnosis: in.state() + " is not a directory of " + in.user() + " after the root part"}
			}
			probe := filepath.Join(in.state(), ".install-probe")
			if err := os.WriteFile(probe, nil, 0o600); err != nil {
				return &Failed{Diagnosis: in.user() + " cannot write into " + in.state() + ": " + err.Error()}
			}
			_ = os.Remove(probe)
			if !in.hostEnvHolds() {
				return &Failed{Diagnosis: in.hostEnvPath() + " does not hold the answers after the root part"}
			}
			if _, err := in.M.Stat("/var/lib/systemd/linger/" + in.user()); err != nil {
				return &Failed{Diagnosis: "linger is not on for " + in.user() + " after the root part"}
			}
			stateJSON := filepath.Join(in.state(), "state.json")
			ok, err := r.Until(30*time.Second, time.Second, func() (bool, error) {
				if in.ask(r, "systemctl", "is-active", in.agent()) != "active" {
					return false, nil
				}
				st, err := in.M.Stat(stateJSON)
				return err == nil && !st.Mod.Before(began.Add(-10*time.Second)), nil
			})
			if err != nil {
				return err
			}
			if !ok {
				journal, _ := r.Exec(Cmd{Argv: []string{"journalctl", "-u", in.agent(), "-n", "30", "--no-pager"}, Limit: 30 * time.Second})
				return &Failed{
					Diagnosis: "the collector does not write " + stateJSON + ": journalctl -u " + in.agent() + " -n 30",
					Fix:       []string{"Most often the clone moved after the install: AACP_REPO in " + in.hostEnvPath() + " names where it was."},
					Tail:      lastLines(journal, tailLines),
				}
			}
			_ = removeIfThere(in.stagedPath())
			r.Say(Pass, in.agent()+" active · "+stateJSON+" fresh")
			return nil
		},
		Undo: func(r *Run, e Entry) error {
			if e.Kind == string(Dir) {
				r.Say(Note, e.Target+" is left: it holds the panel's state; sudo rm -r "+e.Target+" deletes it")
				return nil
			}
			return UndoKind(r, e)
		},
	}
}

func lastLines(out string, n int) []string {
	lines := splitLines([]byte(out))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ---- S5: the user manager ----

func (in *Install) managerUp(r *Run) bool {
	switch in.ask(r, "systemctl", "--user", "is-system-running") {
	case "running", "degraded":
		return true
	}
	return false
}

func (in *Install) userManagerStep() *Step {
	return &Step{ID: "usermgr", Title: "User manager",
		Done: func(r *Run) (bool, error) {
			return in.M.Env("XDG_RUNTIME_DIR") == in.runtime() && in.managerUp(r), nil
		},
		Apply: func(r *Run) error {
			// Without a login of its own — ssh without logind, sudo -u, a CI
			// runner — the manager is reached by its address all the same.
			if r.Env == nil {
				r.Env = map[string]string{}
			}
			r.Env["XDG_RUNTIME_DIR"] = in.runtime()
			r.Env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + in.runtime() + "/bus"
			_, err := r.Until(15*time.Second, 500*time.Millisecond, func() (bool, error) {
				st, err := in.M.Stat(in.runtime() + "/bus")
				return err == nil && st.Mode&fs.ModeSocket != 0, nil
			})
			return err
		},
		Verify: func(r *Run) error {
			ok, err := r.Until(15*time.Second, time.Second, func() (bool, error) { return in.managerUp(r), nil })
			if err != nil {
				return err
			}
			if !ok {
				return &Failed{Diagnosis: fmt.Sprintf("the user systemd manager did not come up after linger: systemctl status user@%d", in.uid())}
			}
			r.Say(Pass, "systemctl --user answers · linger keeps it up after you log out")
			return nil
		},
		Undo: UndoKind,
	}
}
