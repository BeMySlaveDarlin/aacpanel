package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
)

// ---- S6: .env ----

// secrets are the keys of the .env made once on this machine and never
// made again: a new AACP_SECRET logs every device out, a new database
// password locks the volume.
var secrets = []secret{{"AACP_SECRET", 32}, {"AACP_DB_PASSWORD", 24}}

type secret struct {
	key   string
	bytes int
}

// tokenBytes is the size of the token a phone signs in with over the home
// network: 48 hex characters, twice the least the panel takes.
const tokenBytes = 24

// lacking are the secrets the .env is to hold and does not: the two of
// every install, and the token when the answers want one.
func (in *Install) lacking(env map[string]string) []secret {
	var out []secret
	for _, s := range secrets {
		if strings.TrimSpace(env[s.key]) == "" {
			out = append(out, s)
		}
	}
	if want, _ := in.S.Token(); want && strings.TrimSpace(env["AACP_TOKEN"]) == "" {
		out = append(out, secret{"AACP_TOKEN", tokenBytes})
	}
	return out
}

// secretKeys are the keys of the .env whose values the journal never holds.
var secretKeys = []string{"AACP_SECRET", "AACP_DB_PASSWORD", "AACP_APP_PASSWORD", "AACP_TOKEN", "TS_AUTHKEY", "AACP_TEST_DSN"}

// readEnv reads the .env of the clone, and hides every secret in it from
// the journal before any command could print one.
func (in *Install) readEnv(r *Run) ([]byte, map[string]string, error) {
	raw, err := in.M.ReadFile(in.dotEnvPath())
	if err != nil {
		return nil, nil, err
	}
	env := hostcfg.Parse(raw)
	for _, k := range secretKeys {
		r.Hide(env[k])
	}
	if u, err := urlPassword(env["AACP_TEST_DSN"]); err == nil {
		r.Hide(u)
	}
	return raw, env, nil
}

func urlPassword(dsn string) (string, error) {
	_, rest, ok := strings.Cut(dsn, "://")
	if !ok {
		return "", errors.New("no scheme")
	}
	cred, _, ok := strings.Cut(rest, "@")
	if !ok {
		return "", errors.New("no credentials")
	}
	_, pw, _ := strings.Cut(cred, ":")
	return pw, nil
}

// envWant is what the .env should say: the keys the answers give, the
// secrets it lacks, and the keys of a way in turned off.
func (in *Install) envWant(raw []byte) (vars []Var, drop []string) {
	have := hostcfg.Parse(raw)
	set, drop := in.S.DotEnv()
	return changed(have, set), live(raw, drop)
}

func (in *Install) envFileStep() *Step {
	return &Step{ID: "envfile", Title: "Panel settings",
		Done: func(r *Run) (bool, error) {
			raw, env, err := in.readEnv(r)
			if err != nil {
				return false, nil
			}
			if st, err := in.M.Stat(in.dotEnvPath()); err != nil || st.Mode.Perm() != 0o600 {
				return false, nil
			}
			if len(in.lacking(env)) > 0 {
				return false, nil
			}
			if vars, drop := in.envWant(raw); len(vars) > 0 || len(drop) > 0 {
				return false, nil
			}
			_, err = r.Exec(Cmd{Argv: in.docker("compose", "config", "-q"), Dir: in.clone(), Limit: time.Minute})
			return err == nil, nil
		},
		Apply: func(r *Run) error {
			path := in.dotEnvPath()
			template, err := in.M.ReadFile(filepath.Join(in.clone(), ".env.example"))
			if err != nil {
				return fmt.Errorf("the template of .env is not in the clone: %w", err)
			}
			if err := in.touch(r, File, path, "data"); err != nil {
				return err
			}
			raw, env, err := in.readEnv(r)
			if err != nil {
				raw, env = template, hostcfg.Parse(template)
			}
			var made []Var
			for _, s := range in.lacking(env) {
				v, err := NewSecret(s.bytes)
				if err != nil {
					return err
				}
				r.Hide(v)
				if err := r.Once(EnvKey, s.key, "generated"); err != nil {
					return err
				}
				made = append(made, Var{Key: s.key, Value: v})
			}
			vars, drop := in.envWant(raw)
			if err := writeFile(path, Edit(raw, append(made, vars...), drop, template), 0o600); err != nil {
				return err
			}
			line := fmt.Sprintf("%s · 0600 · %s set", in.short(path), count(len(made)+len(vars), "key"))
			if len(made) > 0 {
				line += fmt.Sprintf(", %s made, not shown", count(len(made), "secret"))
			}
			if len(drop) > 0 {
				line += " · back to the template: " + strings.Join(drop, ", ")
			}
			r.Say(Pass, line)
			return nil
		},
		Verify: func(r *Run) error {
			if st, err := in.M.Stat(in.dotEnvPath()); err != nil || st.Mode.Perm() != 0o600 {
				return &Failed{Diagnosis: in.dotEnvPath() + " is not 0600: it holds the secrets of the panel"}
			}
			// The whole configuration carries the secrets: it is read here
			// for two fields and goes nowhere else.
			out, err := r.Exec(Cmd{Argv: in.docker("compose", "config", "--format", "json"), Dir: in.clone(), Quiet: true, Limit: time.Minute})
			if err != nil {
				var fl *Failure
				why := "docker compose config fails"
				if errors.As(err, &fl) && fl.Stderr != "" {
					why += ": " + firstLine(fl.Stderr)
				}
				return &Failed{Diagnosis: why}
			}
			return in.checkCompose(out)
		},
		Undo: UndoKind,
	}
}

// checkCompose holds the two things compose must take from the .env: the
// user the service runs as, who alone may open the executor's socket, and
// where that socket's directory is.
func (in *Install) checkCompose(out string) error {
	var cfg struct {
		Services map[string]struct {
			User    string `json:"user"`
			Volumes []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"volumes"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return &Failed{Diagnosis: "docker compose config gave no configuration to read: " + err.Error()}
	}
	svc := cfg.Services["aacpanel"]
	if want := fmt.Sprintf("%d:%d", in.uid(), in.f().Account.GID); svc.User != want {
		return &Failed{Diagnosis: fmt.Sprintf("compose runs the panel as %q, not %s: the executor's socket would not open to it", svc.User, want)}
	}
	for _, v := range svc.Volumes {
		if v.Target == "/run/aacpanel-exec" {
			if v.Source != in.f().ExecDir() {
				return &Failed{Diagnosis: fmt.Sprintf("compose mounts %s as the executor's directory, not %s", v.Source, in.f().ExecDir())}
			}
			return nil
		}
	}
	return &Failed{Diagnosis: "compose mounts no directory of the executor into the panel"}
}

// ---- S7: the executor's binary ----

func (in *Install) execPath() string { return filepath.Join(in.home(), "bin", "aacpanel-exec") }

func (in *Install) goEnv() []string {
	env := []string{"GOTOOLCHAIN=local", "CGO_ENABLED=0"}
	if in.Cache != "" {
		env = append(env, "GOCACHE="+filepath.Join(in.Cache, "gocache"), "GOMODCACHE="+filepath.Join(in.Cache, "gomod"),
			"XDG_CONFIG_HOME="+filepath.Join(in.Cache, "config"))
	}
	return env
}

// buildDiagnosis is why a build of the executor failed, by what it said.
func buildDiagnosis(err error) string {
	text := strings.ToLower(errText(err))
	var ran *Ran
	if errors.As(err, &ran) {
		text += "\n" + strings.ToLower(strings.Join(ran.Tail, "\n"))
	}
	switch {
	case strings.Contains(text, "no space left"):
		return "the executor did not build: no room left on the disk"
	case strings.Contains(text, "exec format error"):
		return "the executor did not build: the installer's Go is not of this machine's architecture"
	case strings.Contains(text, "proxy.golang.org"), strings.Contains(text, "dial tcp"), strings.Contains(text, "i/o timeout"):
		return "the executor did not build: its modules did not download from proxy.golang.org (the network, or HTTPS_PROXY)"
	}
	return "the executor did not build"
}

func (in *Install) execBuildStep() *Step {
	return &Step{ID: "exec-build", Title: "Build the executor",
		// A build is how the step knows: the same tree and Go give the
		// same binary, and the one in place stays when they do.
		Apply: func(r *Run) error {
			bin := in.execPath()
			if err := in.makeDirs(r, filepath.Dir(bin)); err != nil {
				return err
			}
			if in.Cache != "" {
				if _, err := os.Stat(in.Cache); err == nil {
					if err := r.Once(Dir, in.Cache, "cache"); err != nil {
						return err
					}
				}
			}
			next := bin + ".new"
			_, err := r.Exec(Cmd{Argv: []string{in.Go, "build", "-trimpath", "-buildvcs=false", "-o", next, "./cmd/aacpanel-exec"},
				Dir: in.clone(), Env: in.goEnv()})
			if err != nil {
				_ = os.Remove(next)
				return fail(buildDiagnosis(err), err)
			}
			built, err := os.ReadFile(next)
			if err != nil {
				return err
			}
			if cur, err := os.ReadFile(bin); err == nil && sha(cur) == sha(built) {
				r.Say(Pass, in.short(bin)+" is this tree's already: the build left it in place")
				return os.Remove(next)
			}
			if err := in.touch(r, File, bin, "sha="+sha(built)); err != nil {
				return err
			}
			// A rename, never a write over the file: the holders of sessions
			// on the stream run from it and stay on the old copy.
			if err := os.Rename(next, bin); err != nil {
				return err
			}
			in.execChanged = true
			return nil
		},
		Verify: func(r *Run) error {
			out, err := r.Exec(Cmd{Argv: []string{in.execPath(), "-list"}, Limit: 30 * time.Second})
			if err != nil {
				return fail(in.short(in.execPath())+" -list fails", err)
			}
			kinds := strings.Fields(out)
			for _, k := range kinds {
				if !action.Valid(action.Kind(k)) {
					return &Failed{Diagnosis: fmt.Sprintf("%s -list names %q, which this tree does not know", in.short(in.execPath()), k)}
				}
			}
			if len(kinds) == 0 {
				return &Failed{Diagnosis: in.short(in.execPath()) + " -list names no action"}
			}
			r.Say(Pass, fmt.Sprintf("%s · -list names %d of %d actions", in.short(in.execPath()), len(kinds), len(action.Kinds)))
			return nil
		},
		Undo: UndoKind,
	}
}

// ---- S8: the executor's unit ----

func (in *Install) execUnitPath() string {
	return filepath.Join(in.home(), ".config", "systemd", "user", "aacpanel-exec.service")
}

func (in *Install) execUnit() ([]byte, error) {
	raw, err := in.M.ReadFile(filepath.Join(in.clone(), "deploy", "systemd", "aacpanel-exec.service"))
	if err != nil {
		return nil, err
	}
	return renderUnit(raw, in.state(), "EnvironmentFile=-%s/host.env"), nil
}

const execService = "aacpanel-exec.service"

func (in *Install) execUnitStep() *Step {
	return &Step{ID: "exec-unit", Title: "Executor unit",
		Done: func(r *Run) (bool, error) {
			want, err := in.execUnit()
			if err != nil {
				return false, err
			}
			have, err := in.M.ReadFile(in.execUnitPath())
			return err == nil && bytes.Equal(have, want) && !in.execChanged &&
				in.ask(r, "systemctl", "--user", "is-enabled", execService) == "enabled" &&
				in.ask(r, "systemctl", "--user", "is-active", execService) == "active", nil
		},
		Apply: func(r *Run) error {
			want, err := in.execUnit()
			if err != nil {
				return err
			}
			path := in.execUnitPath()
			if err := in.makeDirs(r, filepath.Dir(path)); err != nil {
				return err
			}
			newUnit := false
			if have, err := os.ReadFile(path); err != nil || !bytes.Equal(have, want) {
				if err := in.touch(r, UserUnit, path, ""); err != nil {
					return err
				}
				if err := writeFile(path, want, 0o644); err != nil {
					return err
				}
				if _, err := r.Exec(Cmd{Argv: []string{"systemctl", "--user", "daemon-reload"}, Limit: time.Minute}); err != nil {
					return fail("systemctl --user daemon-reload fails", err)
				}
				newUnit = true
			}
			if in.ask(r, "systemctl", "--user", "is-enabled", execService) != "enabled" {
				if err := r.Once(Enabled, "user "+execService, "by-installer"); err != nil {
					return err
				}
				// enable makes the directory of the links when it is not
				// there: the installer's then, and uninstall takes it away.
				wants := filepath.Join(filepath.Dir(path), "default.target.wants")
				if _, err := os.Stat(wants); errors.Is(err, fs.ErrNotExist) {
					if err := r.Once(Dir, wants, "created"); err != nil {
						return err
					}
				}
				if _, err := r.Exec(Cmd{Argv: []string{"systemctl", "--user", "enable", execService}, Limit: time.Minute}); err != nil {
					return fail("the executor's unit did not enable", err)
				}
			}
			if newUnit || in.execChanged || in.ask(r, "systemctl", "--user", "is-active", execService) != "active" {
				if _, err := r.Exec(Cmd{Argv: []string{"systemctl", "--user", "restart", execService}, Limit: time.Minute}); err != nil {
					return in.execDiagnosis(r, err)
				}
			}
			return nil
		},
		Verify: func(r *Run) error {
			dir := filepath.Join(in.runtime(), "aacpanel-exec")
			sock := filepath.Join(dir, "sock")
			ok, err := r.Until(15*time.Second, 500*time.Millisecond, func() (bool, error) {
				st, err := in.M.Stat(sock)
				return err == nil && st.Mode&fs.ModeSocket != 0, nil
			})
			if err != nil {
				return err
			}
			if !ok {
				return in.execDiagnosis(r, nil)
			}
			st, _ := in.M.Stat(sock)
			dst, err := in.M.Stat(dir)
			switch {
			case err != nil:
				return &Failed{Diagnosis: dir + " is not there, and the socket is"}
			case dst.UID != in.uid() || dst.Mode.Perm() != 0o700 || st.UID != in.uid():
				return &Failed{Diagnosis: fmt.Sprintf("%s belongs to uid %d with mode %o: docker made it as root before the executor ran. sudo rmdir %s, then run again.",
					dir, dst.UID, dst.Mode.Perm(), dir)}
			}
			r.Say(Pass, fmt.Sprintf("%s active · %s, owner %s, 0700", execService, sock, in.user()))
			return nil
		},
		Undo: UndoKind,
	}
}

// execDiagnosis is why the executor did not come up, by its journal.
func (in *Install) execDiagnosis(r *Run, cause error) error {
	journal, _ := r.Exec(Cmd{Argv: []string{"journalctl", "--user", "-u", execService, "-n", "30", "--no-pager"}, Limit: 30 * time.Second})
	f := &Failed{Diagnosis: "the executor's socket did not appear: journalctl --user -u aacpanel-exec -n 30", Tail: lastLines(journal, tailLines)}
	if strings.Contains(journal, "226/NAMESPACE") {
		f.Diagnosis = "the executor's unit could not build its namespace (226/NAMESPACE): an older unit without the minus in InaccessiblePaths"
		f.Fix = []string{"Run ./install.sh again: it installs the unit of this tree."}
	}
	var ran *Ran
	if errors.As(cause, &ran) && len(f.Tail) == 0 {
		f.Tail = ran.Tail
	}
	return f
}

// ---- S9: the stack ----

// healthz is where the panel answers whether it is up: the address compose
// publishes it on.
func (in *Install) healthz(env map[string]string) string {
	bind := env["AACP_BIND"]
	if bind == "" {
		bind = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d/healthz", bind, PortPanel)
}

// stackDiagnosis is why compose failed, by what it said.
func stackDiagnosis(err error) (string, []string) {
	var ran *Ran
	text := strings.ToLower(errText(err))
	if errors.As(err, &ran) {
		text += "\n" + strings.ToLower(strings.Join(ran.Tail, "\n"))
	}
	switch {
	case strings.Contains(text, "govulncheck") || strings.Contains(text, "vulnerability #"):
		return "the image build stopped on a vulnerability published after this release", []string{
			"./install.sh update, or knowingly ./install.sh --skip-vulncheck"}
	case strings.Contains(text, "port is already allocated"), strings.Contains(text, "address already in use"):
		return "a port the panel publishes is taken", []string{"ss -ltnp names who holds it."}
	case strings.Contains(text, "is already in use by container"):
		return "a container of a name the panel takes belongs to something else", []string{"docker ps -a names it; remove or rename it."}
	case strings.Contains(text, `mount source: "overlay"`) && strings.Contains(text, "invalid argument"):
		return "docker cannot mount overlay here: its storage lies on overlay itself", []string{"/var/lib/containerd has to live on a volume of its own."}
	}
	return "docker compose failed", nil
}

func (in *Install) stackStep() *Step {
	return &Step{ID: "compose", Title: "Panel stack",
		// Compose itself keeps a stack as it is: an image built again from
		// the same tree comes out of the cache with the same id, and up
		// recreates no container whose image and configuration hold.
		Apply: func(r *Run) error {
			if err := r.Once(Compose, Project, "project"); err != nil {
				return err
			}
			// up makes every volume of the file, the tailnet node's as well
			// whether the node runs or not: both are the install's.
			for _, v := range []string{DBVolume, TSVolume} {
				if _, err := r.Exec(Cmd{Argv: in.docker("volume", "inspect", v), Quiet: true, Limit: time.Minute}); err != nil {
					if err := r.Once(Volume, v, "data"); err != nil {
						return err
					}
				}
			}
			if err := r.Once(Image, Project+"-aacpanel", "local"); err != nil {
				return err
			}
			if err := in.recordPulls(r); err != nil {
				return err
			}
			build := []string{"compose", "build"}
			if in.SkipVulncheck {
				build = append(build, "--build-arg", "SKIP_VULNCHECK=1")
				r.Say(Warn, "the image is built without the check of its dependencies: --skip-vulncheck")
			}
			// BuildKit gives an image a provenance attestation that carries
			// the time of the build, and the id of the image changes with it:
			// a build all from the cache would have up recreate the panel on
			// every run. Without the attestation the same tree keeps its id.
			for _, c := range []Cmd{
				{Argv: in.docker(append(build, "aacpanel")...), Env: []string{"BUILDX_NO_DEFAULT_ATTESTATIONS=1"}},
				{Argv: in.docker("compose", "up", "-d")},
			} {
				c.Dir = in.clone()
				if _, err := r.Exec(c); err != nil {
					why, fix := stackDiagnosis(err)
					return fail(why, err, fix...)
				}
			}
			return nil
		},
		Verify: func(r *Run) error {
			_, env, err := in.readEnv(r)
			if err != nil {
				return err
			}
			return in.panelUp(r, env)
		},
		Undo: UndoKind,
	}
}

// recordPulls records the images of the stack that are not on the machine
// yet, before compose pulls them: they are the install's, and uninstall
// takes them away, where the images the machine had before stay.
func (in *Install) recordPulls(r *Run) error {
	out, err := r.Exec(Cmd{Argv: in.docker("compose", "config", "--images"), Dir: in.clone(), Limit: time.Minute})
	if err != nil {
		return fail("docker compose config --images fails", err)
	}
	for _, ref := range strings.Fields(out) {
		if ref == Project+"-aacpanel" {
			continue
		}
		if err := in.recordPull(r, ref); err != nil {
			return err
		}
	}
	return nil
}

// recordPull records an image about to be pulled, unless the machine has it.
func (in *Install) recordPull(r *Run, ref string) error {
	if _, err := r.Exec(Cmd{Argv: in.docker("image", "inspect", "--format", "{{.Id}}", ref), Limit: time.Minute}); err == nil {
		return nil
	}
	return r.Once(Image, ref, "pulled")
}

// panelUp waits for the database to be healthy and the panel to answer on
// its address and to say it reached the store.
func (in *Install) panelUp(r *Run, env map[string]string) error {
	healthy, err := r.Until(90*time.Second, 2*time.Second, func() (bool, error) {
		return in.ask(r, in.docker("inspect", "-f", "{{.State.Health.Status}}", "aacpanel-db")...) == "healthy", nil
	})
	if err != nil {
		return err
	}
	if !healthy {
		logs, _ := r.Exec(Cmd{Argv: in.docker("logs", "--tail", "20", "aacpanel-db"), Limit: time.Minute})
		return &Failed{Diagnosis: "aacpanel-db is not healthy after 90 s: docker compose logs aacpanel-db", Tail: lastLines(logs, tailLines)}
	}
	url := in.healthz(env)
	up, err := r.Until(120*time.Second, 2*time.Second, func() (bool, error) {
		status, err := in.M.Reach(url)
		if err != nil || status != 200 {
			return false, nil
		}
		logs := in.ask(r, in.docker("compose", "logs", "--no-color", "aacpanel")...)
		return strings.Contains(logs, "store: connected"), nil
	})
	if err != nil {
		return err
	}
	if !up {
		logs, _ := r.Exec(Cmd{Argv: in.docker("compose", "logs", "--no-color", "--tail", "20", "aacpanel"), Dir: in.clone(), Limit: time.Minute})
		return &Failed{Diagnosis: url + " did not answer with the store connected in 120 s: docker compose logs aacpanel",
			Fix: []string{"A wrong AACP_BIND in .env keeps the address from answering."}, Tail: lastLines(logs, tailLines)}
	}
	r.Say(Pass, "aacpanel-db healthy · "+url+" ok · store: connected")
	return nil
}

// ---- S10: the application role ----

var roleName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func orDefault(env map[string]string, key, def string) string {
	if v := strings.TrimSpace(env[key]); v != "" {
		return v
	}
	return def
}

// authFailures counts the times the panel's container said its password
// was refused since it started.
func (in *Install) authFailures(r *Run) int {
	return strings.Count(in.ask(r, in.docker("compose", "logs", "--no-color", "aacpanel")...), "authentication failed")
}

func (in *Install) appRoleStep() *Step {
	role := func(env map[string]string) string { return orDefault(env, "AACP_APP_ROLE", "monitor_app") }
	return &Step{ID: "approle", Title: "App role",
		Done: func(r *Run) (bool, error) {
			_, env, err := in.readEnv(r)
			if err != nil || env["AACP_APP_ROLE"] == "" || env["AACP_APP_PASSWORD"] == "" || !roleName.MatchString(role(env)) {
				return false, nil
			}
			found := in.ask(r, in.docker("exec", "aacpanel-db", "psql", "-U", orDefault(env, "AACP_DB_USER", "aacpanel"),
				"-d", orDefault(env, "AACP_DB_NAME", "aacpanel"), "-tAc",
				"SELECT 1 FROM pg_roles WHERE rolname = '"+role(env)+"'")...)
			return found == "1" && in.authFailures(r) == 0, nil
		},
		Apply: func(r *Run) error {
			raw, env, err := in.readEnv(r)
			if err != nil {
				return err
			}
			name := role(env)
			if !roleName.MatchString(name) {
				return &Failed{Diagnosis: fmt.Sprintf("AACP_APP_ROLE=%q in .env is not a plain role name", name)}
			}
			pw, err := NewSecret(24)
			if err != nil {
				return err
			}
			r.Hide(pw)
			if err := r.Once(DBRole, name, "created"); err != nil {
				return err
			}
			// The password reaches the script through its environment, never
			// its arguments; the script sets it with ALTER ROLE when the role
			// is there, so a run that stopped before the .env had it heals.
			env2 := []string{"AACP_APP_PASSWORD=" + pw, "AACP_APP_ROLE=" + name,
				"AACP_DB_USER=" + orDefault(env, "AACP_DB_USER", "aacpanel"), "AACP_DB_NAME=" + orDefault(env, "AACP_DB_NAME", "aacpanel")}
			if _, err := r.Exec(Cmd{Argv: in.withGroup([]string{"bash", "deploy/create-app-role.sh"}), Dir: in.clone(), Env: env2}); err != nil {
				return fail("the app role was not made: deploy/create-app-role.sh failed", err)
			}
			// The name and the password go in together: compose builds the
			// DSN of the two, and a password without its name connects as
			// the owner with a password not the owner's.
			for _, k := range []string{"AACP_APP_ROLE", "AACP_APP_PASSWORD"} {
				if err := r.Once(EnvKey, k, "generated"); err != nil {
					return err
				}
			}
			out := Edit(raw, []Var{{Key: "AACP_APP_ROLE", Value: name}, {Key: "AACP_APP_PASSWORD", Value: pw}}, nil, nil)
			if err := writeFile(in.dotEnvPath(), out, 0o600); err != nil {
				return err
			}
			if _, err := r.Exec(Cmd{Argv: in.docker("compose", "up", "-d", "aacpanel"), Dir: in.clone()}); err != nil {
				why, fix := stackDiagnosis(err)
				return fail(why, err, fix...)
			}
			return nil
		},
		Verify: func(r *Run) error {
			_, env, err := in.readEnv(r)
			if err != nil {
				return err
			}
			if err := in.panelUp(r, env); err != nil {
				return err
			}
			if n := in.authFailures(r); n > 0 {
				return &Failed{Diagnosis: fmt.Sprintf("the panel's password was refused %s: the role and .env parted", count(n, "time")),
					Fix: []string{"Run ./install.sh again: the step sets the password anew (ALTER ROLE)."}}
			}
			if local, ok := env["AACP_LOCAL_ADDR"]; !ok || local != "" {
				url := fmt.Sprintf("http://127.0.0.1:%d/api/profiles", PortLocal)
				if status, err := in.M.Reach(url); err != nil || status != 200 {
					return &Failed{Diagnosis: fmt.Sprintf("%s answers %d, not 200: the panel does not read its database under the role", url, status)}
				}
			}
			r.Say(Pass, "role "+role(env)+" · the panel works under it · authentication failed: 0")
			return nil
		},
		Undo: UndoKind,
	}
}

// ---- S10a: the test database ----

const (
	testDBName  = "aacpanel-test-db"
	testDBPort  = 55432
	testDBImage = "postgres:18-alpine"
)

func (in *Install) testDBUp(r *Run) bool {
	return in.ask(r, in.docker("inspect", "-f", "{{.State.Running}}", testDBName)...) == "true" && in.pgReady(r)
}

func (in *Install) pgReady(r *Run) bool {
	_, err := r.Exec(Cmd{Argv: in.docker("exec", testDBName, "pg_isready", "-q"), Limit: 30 * time.Second})
	return err == nil
}

func (in *Install) testDBStep() *Step {
	return &Step{ID: "testdb", Title: "Test database",
		Done: func(r *Run) (bool, error) {
			_, env, err := in.readEnv(r)
			return err == nil && env["AACP_TEST_DSN"] != "" && in.testDBUp(r), nil
		},
		Apply: func(r *Run) error {
			raw, _, err := in.readEnv(r)
			if err != nil {
				return err
			}
			user, db, pw := "aacpanel", "aacpanel", ""
			if _, err := r.Exec(Cmd{Argv: in.docker("inspect", testDBName), Quiet: true, Limit: time.Minute}); err == nil {
				// A test database made by hand: its password is in its
				// environment, and the DSN is written from it.
				out, err := r.Exec(Cmd{Argv: in.docker("inspect", "-f", "{{range .Config.Env}}{{println .}}{{end}}", testDBName), Quiet: true, Limit: time.Minute})
				if err != nil {
					return fail("the environment of "+testDBName+" did not read", err)
				}
				cenv := hostcfg.Parse([]byte(out))
				user, db, pw = orDefault(cenv, "POSTGRES_USER", "postgres"), orDefault(cenv, "POSTGRES_DB", "postgres"), cenv["POSTGRES_PASSWORD"]
				r.Hide(pw)
				if in.ask(r, in.docker("inspect", "-f", "{{.State.Running}}", testDBName)...) != "true" {
					if _, err := r.Exec(Cmd{Argv: in.docker("start", testDBName), Limit: time.Minute}); err != nil {
						return fail(testDBName+" did not start", err)
					}
				}
			} else {
				if holder, taken := in.listening(testDBPort); taken {
					return &Failed{Diagnosis: fmt.Sprintf("127.0.0.1:%d is taken by %s: the test database publishes there", testDBPort, holder)}
				}
				if pw, err = NewSecret(24); err != nil {
					return err
				}
				r.Hide(pw)
				if err := in.recordPull(r, testDBImage); err != nil {
					return err
				}
				if err := r.Record(TestDB, testDBName, "created"); err != nil {
					return err
				}
				// Restarts with docker: a test database that is gone after a
				// reboot turns make check green without its forty tests.
				_, err := r.Exec(Cmd{Argv: in.docker("run", "-d", "--restart", "unless-stopped", "--name", testDBName,
					"-p", fmt.Sprintf("127.0.0.1:%d:5432", testDBPort),
					"-e", "POSTGRES_USER="+user, "-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_DB="+db, testDBImage),
					Env: []string{"POSTGRES_PASSWORD=" + pw}})
				if err != nil {
					return fail(testDBName+" did not start", err)
				}
			}
			ready, err := r.Until(60*time.Second, time.Second, func() (bool, error) { return in.pgReady(r), nil })
			if err != nil {
				return err
			}
			if !ready {
				return &Failed{Diagnosis: testDBName + " does not take connections after 60 s: docker logs " + testDBName}
			}
			dsn := fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable", user, pw, testDBPort, db)
			if err := r.Once(EnvKey, "AACP_TEST_DSN", "generated"); err != nil {
				return err
			}
			out := Edit(raw, []Var{{Key: "AACP_TEST_DSN", Value: dsn,
				Comment: "Where the tests with a database make theirs (internal/testdb): make check reads it from here.\n" +
					"Without it forty tests are skipped, silently."}}, nil, nil)
			return writeFile(in.dotEnvPath(), out, 0o600)
		},
		Verify: func(r *Run) error {
			if !in.pgReady(r) {
				return &Failed{Diagnosis: testDBName + " does not take connections: docker logs " + testDBName}
			}
			r.Say(Pass, fmt.Sprintf("%s on 127.0.0.1:%d · make check finds it in .env", testDBName, testDBPort))
			return nil
		},
		Undo: UndoKind,
	}
}

// listening tells whether something listens on a port of the loopback.
func (in *Install) listening(port int) (string, bool) {
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := in.M.ReadFile(file)
		if err != nil {
			continue
		}
		if slices.Contains(listening(string(raw)), port) {
			return "another process", true
		}
	}
	return "", false
}

// ---- S13a: the collector's restart ----

func (in *Install) rev() string {
	if v := in.f().Version; v != "" {
		return v
	}
	return "unknown"
}

func (in *Install) collectorStep() *Step {
	return &Step{ID: "collector-restart", Title: "Restart the collector",
		// The tree is written down before the restart is asked for, with the
		// time it was asked at: a restart said no to, or cut short, leaves the
		// collector older than that and the next run restarts it, while a
		// restart an administrator ran after the stop leaves it younger.
		Done: func(r *Run) (bool, error) {
			e := r.lastEntry(Rev)
			if e.Target != in.rev() || in.hostChanged || in.agentUnit {
				return false, nil
			}
			asked, err := strconv.ParseInt(metaValue(e.Meta, "asked"), 10, 64)
			if err != nil {
				return true, nil
			}
			since, ok := in.agentSince()
			return ok && since >= asked-1, nil
		},
		Apply: func(r *Run) error {
			// The collector runs from the tree: a module that does not
			// import keeps it from starting at all.
			if _, err := r.Exec(Cmd{Argv: []string{"python3", "importcheck.py"}, Dir: filepath.Join(in.clone(), "agent"), Limit: 2 * time.Minute}); err != nil {
				return fail("the collector would not come up: a module of agent/ does not import", err)
			}
			if in.agentStarted {
				r.Say(Pass, in.agent()+" started in the root part, on this tree")
				return r.Record(Rev, in.rev(), "")
			}
			in.restarted = r.clock().Now()
			if err := r.Record(Rev, in.rev(), "asked="+strconv.FormatInt(in.restarted.Unix(), 10)); err != nil {
				return err
			}
			return r.AsRoot("Root command", "Restarts the collector, which runs from the clone: "+in.agent()+".",
				"restart-agent", "--user", in.user())
		},
		Verify: func(r *Run) error {
			if in.restarted.IsZero() {
				return nil
			}
			stateJSON := filepath.Join(in.state(), "state.json")
			ok, err := r.Until(30*time.Second, time.Second, func() (bool, error) {
				st, err := in.M.Stat(stateJSON)
				return err == nil && st.Mod.After(in.restarted), nil
			})
			if err != nil {
				return err
			}
			if !ok {
				journal, _ := r.Exec(Cmd{Argv: []string{"journalctl", "-u", in.agent(), "-n", "30", "--no-pager"}, Limit: 30 * time.Second})
				return &Failed{Diagnosis: "the collector did not write " + stateJSON + " in 30 s after its restart: journalctl -u " + in.agent() + " -n 30",
					Tail: lastLines(journal, tailLines)}
			}
			r.Say(Pass, in.agent()+" restarted · "+stateJSON+" fresh")
			return r.Record(Rev, in.rev(), "")
		},
		Undo: UndoKind,
	}
}

// agentSince is when the collector's main process started, in seconds of
// the clock: the boot time of /proc/stat and the start of the process in
// ticks of a hundredth of a second after it. False when it does not run.
func (in *Install) agentSince() (int64, bool) {
	out, err := in.M.Run("systemctl", "show", "-p", "MainPID", "--value", in.agent())
	pid := strings.TrimSpace(out)
	if err != nil || pid == "" || pid == "0" {
		return 0, false
	}
	raw, err := in.M.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		return 0, false
	}
	// The name in parentheses may hold spaces: the fields are counted from
	// after it, the state first and the start the twentieth.
	s := string(raw)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 20 {
		return 0, false
	}
	ticks, err := strconv.ParseInt(f[19], 10, 64)
	if err != nil {
		return 0, false
	}
	stat, err := in.M.ReadFile("/proc/stat")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(stat), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if boot, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return boot + ticks/100, true
			}
		}
	}
	return 0, false
}
