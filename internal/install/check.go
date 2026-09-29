package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
)

// ---- S14: the check ----

// link is one link of the chain the panel works through, as the check
// finds it: whether it holds, the line that says so, and what to look at
// when it does not.
type chainLink struct {
	mark Mark
	text string
	fix  string
}

func pass(format string, args ...any) chainLink {
	return chainLink{mark: Pass, text: fmt.Sprintf(format, args...)}
}
func broken(fix, format string, args ...any) chainLink {
	return chainLink{mark: Stop, text: fmt.Sprintf(format, args...), fix: fix}
}

// askSocket is where the collector takes the questions of sessions.
const askSocket = "/run/aacpanel-agent/ask.sock"

// stateFresh is how old the collector's snapshot may be and still count as
// written: the collector writes every few seconds.
const stateFresh = 60 * time.Second

// ExecSocket is where the executor listens.
func (in *Install) ExecSocket() string {
	if in.Sock != "" {
		return in.Sock
	}
	return filepath.Join(in.runtime(), "aacpanel-exec", "sock")
}

// localOn tells whether the panel's local listener is on: a key there and
// empty turns it off.
func localOn(env map[string]string) bool {
	v, ok := env["AACP_LOCAL_ADDR"]
	return !ok || strings.TrimSpace(v) != ""
}

// page gets a page of the local listener and reads it into out.
func (in *Install) page(path string, out any) (int, error) {
	status, body, err := in.M.Fetch(in.panelURL() + path)
	if err != nil {
		return 0, err
	}
	if status == 200 && out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return status, fmt.Errorf("%s gave no JSON to read: %w", path, err)
		}
	}
	return status, nil
}

// CheckStep is S14: the chain the panel works through, a line a link, from
// the service's answer to the contours on the map. It changes nothing; a
// broken link stops the run with what to look at.
func (in *Install) CheckStep() *Step {
	return &Step{ID: "check", Title: "Check",
		Verify: func(r *Run) error {
			links := in.chain(r)
			var fixes []string
			for _, l := range links {
				r.Say(l.mark, l.text)
				if l.mark == Stop && l.fix != "" && !slices.Contains(fixes, l.fix) {
					fixes = append(fixes, l.fix)
				}
			}
			n := 0
			for _, l := range links {
				if l.mark == Stop {
					n++
				}
			}
			if n > 0 {
				return &Failed{Diagnosis: fmt.Sprintf("%s of the chain %s broken", count(n, "link"), map[bool]string{true: "is", false: "are"}[n == 1]), Fix: fixes}
			}
			return nil
		},
	}
}

// chain is every link of the check, in the order the panel leans on them.
func (in *Install) chain(r *Run) []chainLink {
	_, env, err := in.readEnv(r)
	if err != nil {
		return []chainLink{broken("./install.sh writes it; run it again.", "%s is not there: the panel has no settings", in.dotEnvPath())}
	}
	local := localOn(env)
	out := []chainLink{in.healthLink(env, local), in.stateLink(r), in.listLink(r), in.socketLink(local)}
	out = append(out, in.databaseLink(r, local), in.containersLink(r, env), in.collectorLink(r), in.executorLink(r),
		in.askLink())
	out = append(out, in.wiringLink()...)
	out = append(out, in.mapLink(local))
	if env["AACP_TAILSCALE"] == "1" {
		out = append(out, in.tailnetLink(r))
	}
	return out
}

func (in *Install) healthLink(env map[string]string, local bool) chainLink {
	url := in.healthz(env)
	where := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/healthz")
	const fix = "docker compose ps and docker compose logs aacpanel; a wrong AACP_BIND in .env keeps the address from answering."
	if status, err := in.M.Reach(url); err != nil || status != 200 {
		return broken(fix, "/healthz does not answer on %s (%s)", where, answered(status, err))
	}
	if !local {
		return pass("/healthz ok on %s · the local listener is off", where)
	}
	if status, err := in.M.Reach(in.panelURL() + "/healthz"); err != nil || status != 200 {
		return broken(fix, "/healthz ok on %s, and the local listener does not answer (%s)", where, answered(status, err))
	}
	return pass("/healthz ok on %s and on :%d", where, PortLocal)
}

func answered(status int, err error) string {
	if err != nil {
		return firstLine(err.Error())
	}
	return fmt.Sprintf("status %d", status)
}

func (in *Install) stateLink(r *Run) chainLink {
	path := filepath.Join(in.state(), "state.json")
	fix := "systemctl status " + in.agent() + " and journalctl -u " + in.agent() + " -n 50; most often the clone moved and AACP_REPO in host.env names where it was."
	st, err := in.M.Stat(path)
	if err != nil {
		return broken(fix, "%s is not there: the collector does not write", path)
	}
	age := r.clock().Now().Sub(st.Mod).Round(time.Second)
	if age > stateFresh {
		return broken(fix, "%s is %s old: the collector does not write", path, age)
	}
	return pass("state.json is %s old", max(age, 0))
}

func (in *Install) listLink(r *Run) chainLink {
	out, err := r.Exec(Cmd{Argv: []string{in.execPath(), "-list"}, Limit: 30 * time.Second})
	const fix = "./install.sh builds the executor again."
	if err != nil {
		return broken(fix, "%s -list fails: %s", in.short(in.execPath()), firstLine(errText(err)))
	}
	kinds := strings.Fields(out)
	for _, k := range kinds {
		if !action.Valid(action.Kind(k)) {
			return broken(fix, "%s -list names %q, which this tree does not know: the executor is of another tree", in.short(in.execPath()), k)
		}
	}
	return pass("aacpanel-exec -list names %d actions", len(kinds))
}

func (in *Install) socketLink(local bool) chainLink {
	sock := in.ExecSocket()
	fix := "systemctl --user status aacpanel-exec; AACP_UID in .env is the owner the panel opens the socket as."
	st, err := in.M.Stat(sock)
	switch {
	case err != nil || st.Mode&fs.ModeSocket == 0:
		return broken(fix, "the executor's socket %s is not there", sock)
	case st.UID != in.uid():
		return broken(fix, "the executor's socket belongs to uid %d, not %d", st.UID, in.uid())
	case !local:
		return pass("executor socket: owner %s", in.user())
	}
	var ex struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	if status, err := in.page("/api/exec", &ex); err != nil || status != 200 {
		return broken(fix, "executor socket: owner %s · /api/exec does not answer (%s)", in.user(), answered(status, err))
	}
	if !ex.Available {
		return broken(fix, "executor socket: owner %s · the panel does not reach it: %s", in.user(), ex.Reason)
	}
	return pass("executor socket: owner %s · /api/exec says available", in.user())
}

func (in *Install) databaseLink(r *Run, local bool) chainLink {
	const fix = "AACP_APP_ROLE and AACP_APP_PASSWORD in .env go both or neither; docker compose ps aacpanel-db is healthy."
	fails := in.authFailures(r)
	if !local {
		if fails > 0 {
			return broken(fix, "database: authentication failed %s", count(fails, "time"))
		}
		return pass("database: authentication failed: 0 · /api/profiles not asked, the local listener is off")
	}
	status, err := in.page("/api/profiles", nil)
	switch {
	case err != nil || status != 200:
		return broken(fix, "database: /api/profiles answers %s, not 200", answered(status, err))
	case fails > 0:
		return broken(fix, "database: /api/profiles 200 · authentication failed %s", count(fails, "time"))
	}
	return pass("database: /api/profiles 200 · authentication failed: 0")
}

// stackContainers are the containers of the stack, the tailnet's when it
// is on.
func stackContainers(env map[string]string) []string {
	out := []string{"aacpanel", "aacpanel-db", "aacpanel-socket-proxy"}
	if env["AACP_TAILSCALE"] == "1" {
		out = append(out, tsContainer)
	}
	return out
}

func (in *Install) containersLink(r *Run, env map[string]string) chainLink {
	names := stackContainers(env)
	const fix = "docker compose ps and docker compose logs; ./install.sh brings the stack up again."
	out, _ := r.Exec(Cmd{Argv: in.docker(append([]string{"inspect", "-f",
		"{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}"}, names...)...), Limit: time.Minute})
	seen := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			seen[strings.TrimPrefix(f[0], "/")] = strings.Join(f[1:], " ")
		}
	}
	var bad []string
	for _, n := range names {
		switch state := seen[n]; state {
		case "running", "running healthy":
		case "":
			bad = append(bad, n+" is not there")
		default:
			bad = append(bad, n+" is "+state)
		}
	}
	if len(bad) > 0 {
		return broken(fix, "containers: %s", strings.Join(bad, ", "))
	}
	return pass("containers healthy: %s", strings.Join(names, ", "))
}

// unitState is how systemctl answers of a unit: active, enabled.
func (in *Install) unitState(r *Run, user bool, unit string) (active, enabled string) {
	scope := []string{"systemctl"}
	if user {
		scope = append(scope, "--user")
	}
	return in.ask(r, append(slices.Clone(scope), "is-active", unit)...), in.ask(r, append(slices.Clone(scope), "is-enabled", unit)...)
}

func (in *Install) collectorLink(r *Run) chainLink {
	active, enabled := in.unitState(r, false, in.agent())
	if active != "active" || enabled != "enabled" {
		return broken("journalctl -u "+in.agent()+" -n 30; ./install.sh puts the unit in place again.",
			"%s is %s and %s", in.agent(), orNone(active), orNone(enabled))
	}
	return pass("%s active and enabled", in.agent())
}

func (in *Install) executorLink(r *Run) chainLink {
	active, enabled := in.unitState(r, true, execService)
	const fix = "journalctl --user -u aacpanel-exec -n 30; without linger the user's units stop at the last logout."
	if active != "active" || enabled != "enabled" {
		return broken(fix, "%s is %s and %s", execService, orNone(active), orNone(enabled))
	}
	if _, err := in.M.Stat(lingerPath(in.user())); err != nil {
		return broken(fix, "%s active and enabled · linger is off for %s", execService, in.user())
	}
	return pass("%s active and enabled · linger on", execService)
}

func orNone(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func (in *Install) askLink() chainLink {
	st, err := in.M.Stat(askSocket)
	if err != nil || st.Mode&fs.ModeSocket == 0 {
		return broken("journalctl -u "+in.agent()+" -n 30: the collector opens the socket of the questions.",
			"%s is not there: a session's question does not reach the phone", askSocket)
	}
	return pass("ask.sock in place")
}

// wiringLink is claude's wiring in every account: the question hook once and
// the limits in the status line hold the chain. The panel's server in the
// account serves sessions started by hand — the panel gives its own
// sessions the server at their start — so one missing is a warning.
func (in *Install) wiringLink() []chainLink {
	var bad, dirs, bare []string
	for _, dir := range in.accounts() {
		dirs = append(dirs, in.short(dir))
		raw, err := in.M.ReadFile(in.settingsPath(dir))
		if err != nil {
			bad = append(bad, in.short(dir)+" has no settings.json")
			continue
		}
		if n, err := wiredHooks(raw, "PreToolUse", "agent/ask-hook.py"); err != nil || n != 1 {
			bad = append(bad, fmt.Sprintf("%s runs the question hook %d times", in.short(dir), n))
		}
		if !runs(statusLineOf(raw), statusScript) {
			bad = append(bad, in.short(dir)+" has no limits snapshot in its status line")
		}
		if have, ok := in.mcpHas(dir); !ok || !have.same(in.mcpWant()) {
			bare = append(bare, in.short(dir))
		}
	}
	if len(bad) > 0 {
		return []chainLink{broken("./install.sh wires the accounts again.", "claude wiring: %s", strings.Join(bad, "; "))}
	}
	if len(bare) > 0 {
		return []chainLink{pass("claude wiring in %s: hook once, status line chained", strings.Join(dirs, ", ")),
			{mark: Warn, text: "no aacpanel server of this install in " + strings.Join(bare, ", ") +
				": the sessions the panel starts get it at their start, a session started by hand has none of the panel's tools"}}
	}
	return []chainLink{pass("claude wiring in %s: hook once, status line chained, MCP server listed", strings.Join(dirs, ", "))}
}

func (in *Install) mapLink(local bool) chainLink {
	if !local {
		return chainLink{mark: Warn, text: "the map is not asked: the local listener is off (AACP_LOCAL_ADDR is empty in .env)"}
	}
	v, ok := mapNow(in.M, in.panelURL())
	switch {
	case !ok:
		return broken("docker compose logs aacpanel", "the map does not read through the local listener")
	case len(v.Profiles) == 0:
		// The panel works without one; its projects screen waits for it.
		return chainLink{mark: Warn, text: "the map has no contour: the projects screen stays empty until one is taken on the Profiles screen"}
	}
	return pass("the map has %s", count(len(v.Profiles), "contour"))
}

func (in *Install) tailnetLink(r *Run) chainLink {
	st, ok := in.tsStatus(r)
	const fix = "docker logs aacpanel-tailscale; MagicDNS and HTTPS Certificates on in the tailnet's DNS page."
	if !ok || st.BackendState != "Running" {
		return broken(fix, "tailscale: the node is %s", orNone(st.BackendState))
	}
	serve, err := r.Exec(Cmd{Argv: in.docker("exec", tsContainer, "tailscale", "serve", "status"), Limit: 30 * time.Second})
	if err != nil || !strings.Contains(serve, in.tsTarget()) {
		return broken(fix, "tailscale: %s running, and serve does not point at the panel", st.dns())
	}
	return pass("tailscale: %s running, serve points at the panel", st.dns())
}

// ---- the test session ----

// CheckSession is the name of the test session: the one session the check
// opens, and so the one it closes.
const CheckSession = "aacpanel-check"

// SessionQuestion is whether to open the test session. Its suggested answer
// is yes where claude can work under the first account: a session of an
// account not signed in stops at the sign-in and never shows.
func (in *Install) SessionQuestion() Question {
	q := Question{ID: "session", Prompt: "Open a test session to check the whole chain?", Form: One,
		Options: []Option{
			{Value: yes, Label: "Yes", Detail: "The executor opens " + CheckSession + ", the panel sees it, and the executor closes it — only it."},
			{Value: no, Label: "No", Detail: "./install.sh check --session runs it later."},
		},
		Flag: "--check-session", Values: []string{yes, no}, Default: yes}
	if dirs := in.accounts(); len(dirs) > 0 {
		if signed, _ := in.S.SignedIn(dirs[0]); !signed {
			q.Default = no
			q.Options[1].Detail = "claude is not signed in to " + in.short(dirs[0]) + ": its session would stop at the sign-in."
		}
	}
	return q
}

// SessionStep is the test session of the check: asked for, or given by the
// command line.
func (in *Install) SessionStep() *Step {
	return &Step{ID: "check-session", Title: "Test session",
		Apply: func(r *Run) error {
			v, err := r.Answer(in.SessionQuestion())
			if err != nil {
				return err
			}
			if v != yes {
				r.Say(Note, "skipped: ./install.sh check --session opens it later")
				return nil
			}
			return in.testSession(r)
		},
	}
}

// live tells whether the collector's snapshot holds a session of the name.
func (in *Install) live(name string) bool {
	raw, err := in.M.ReadFile(filepath.Join(in.state(), "state.json"))
	if err != nil {
		return false
	}
	var snap struct {
		Sessions []struct {
			Session string `json:"session"`
		} `json:"sessions"`
	}
	if json.Unmarshal(raw, &snap) != nil {
		return false
	}
	for _, s := range snap.Sessions {
		if s.Session == name {
			return true
		}
	}
	return false
}

// sessionDir is where the test session opens: the clone, when the executor
// may open a session there, else the first place it may.
func (in *Install) sessionDir() string {
	raw, _ := in.M.ReadFile(in.hostEnvPath())
	roots := hostcfg.Parse(raw)[hostcfg.ProjectRootsEnv]
	list := strings.Split(roots, ":")
	if strings.TrimSpace(roots) == "" {
		list = []string{in.home()}
	}
	for _, root := range list {
		if root != "" && (in.clone() == root || strings.HasPrefix(in.clone(), strings.TrimSuffix(root, "/")+"/")) {
			return in.clone()
		}
	}
	return list[0]
}

// execDo asks the executor through its socket, as the panel does.
func (in *Install) execDo(r *Run, req action.Request) (action.Response, error) {
	id, err := NewSecret(8)
	if err != nil {
		return action.Response{}, err
	}
	req.ID = "install-" + id
	if r.Journal != nil {
		_ = r.Journal.Line("> %s %s", req.Kind, req.Target)
	}
	ctx, cancel := context.WithTimeout(r.context(), 2*time.Minute)
	defer cancel()
	resp, err := action.NewClient(in.ExecSocket(), 2*time.Minute).Do(ctx, req)
	if err == nil && !resp.OK {
		err = errors.New(resp.Error)
	}
	return resp, err
}

// testSession opens CheckSession through the executor, waits for the panel
// to see it and closes it. A session of that name already there is not the
// check's: it is left as it is and the check does not run.
func (in *Install) testSession(r *Run) error {
	if in.live(CheckSession) {
		return &Failed{Diagnosis: "a session named " + CheckSession + " is live already, and it is not this check's: it is left as it is",
			Fix: []string{"Close it in the panel, then ./install.sh check --session."}}
	}
	began := r.clock().Now()
	project := &action.Project{Path: in.sessionDir(), Session: CheckSession}
	if dirs := in.accounts(); len(dirs) > 0 && filepath.Clean(dirs[0]) != filepath.Join(in.home(), ".claude") {
		project.ConfigDir = dirs[0]
	}
	launch, _ := json.Marshal(map[string]string{"transport": in.S.valueOr("transport", "stream")})
	project.Launch = launch
	if _, err := in.execDo(r, action.Request{Kind: action.SessionOpen, Target: CheckSession, Project: project}); err != nil {
		return &Failed{Diagnosis: "the executor did not open " + CheckSession + ": " + firstLine(err.Error()),
			Fix: []string{"journalctl --user -u aacpanel-exec -n 30"}}
	}
	r.Say(Pass, CheckSession+" opened through the executor in "+in.short(project.Path))
	seen, err := r.Until(90*time.Second, 2*time.Second, func() (bool, error) { return in.live(CheckSession), nil })
	if err == nil && seen {
		r.Say(Pass, fmt.Sprintf("the panel saw it in its snapshot after %s", r.clock().Now().Sub(began).Round(time.Second)))
	}
	if _, cerr := in.execDo(r, action.Request{Kind: action.SessionClose, Target: CheckSession}); cerr != nil {
		return &Failed{Diagnosis: "the executor did not close " + CheckSession + ": " + firstLine(cerr.Error()),
			Fix: []string{"Close it in the panel: it is the check's own."}}
	}
	r.Say(Pass, CheckSession+" closed — only it")
	if err != nil {
		return err
	}
	if !seen {
		return &Failed{Diagnosis: CheckSession + " opened, and the collector's snapshot did not show it in 90 s",
			Fix: []string{"journalctl -u " + in.agent() + " -n 30"}}
	}
	return nil
}
