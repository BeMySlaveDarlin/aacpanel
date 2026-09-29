package install

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ---- taking over what is in place ----

// Adopted marks a line of the manifest for a thing the installer found in
// place rather than made: an install by hand, or the root part an
// administrator ran for a person without sudo. Uninstall takes such a thing
// away only where it knows it for the panel's beyond doubt — a unit by its
// name, the compose project, a hook by the tail of its path, the server by
// its name — and leaves linger it did not turn on, whatever happens.
const Adopted = "adopted"

// trace is a thing of the panel the machine may hold without the manifest
// knowing it.
type trace struct {
	kind         Kind
	target, meta string
}

// traces are the things of the panel this machine holds, found by the same
// looks the steps take before they change anything.
func (in *Install) traces(r *Run) []trace {
	var out []trace
	there := func(path string) bool { _, err := in.M.Stat(path); return err == nil }
	if there(in.state()) {
		out = append(out, trace{Dir, in.state(), ""})
	}
	if there(in.hostEnvPath()) {
		out = append(out, trace{File, in.hostEnvPath(), "data"})
	}
	if there(collectorUnitPath) {
		out = append(out, trace{SysUnit, collectorUnitPath, ""})
	}
	if in.ask(r, "systemctl", "is-enabled", in.agent()) == "enabled" {
		out = append(out, trace{Enabled, "system " + in.agent(), ""})
	}
	if there(lingerPath(in.user())) {
		out = append(out, trace{Linger, in.user(), ""})
	}
	if there(in.execPath()) {
		out = append(out, trace{File, in.execPath(), ""})
	}
	for _, unit := range userUnits {
		path := filepath.Join(in.userUnitDir(), unit)
		if there(path) {
			out = append(out, trace{UserUnit, path, ""})
		}
		if !strings.HasSuffix(unit, ".service") || unit == execService {
			if in.ask(r, "systemctl", "--user", "is-enabled", unit) == "enabled" {
				out = append(out, trace{Enabled, "user " + unit, ""})
			}
		}
	}
	if there(in.dotEnvPath()) {
		out = append(out, trace{File, in.dotEnvPath(), "data"})
	}
	if in.ask(r, in.docker("ps", "-aq", "--filter", "label=com.docker.compose.project="+Project)...) != "" {
		out = append(out, trace{Compose, Project, "project"})
	}
	for _, v := range []string{DBVolume, TSVolume} {
		if _, err := r.Exec(Cmd{Argv: in.docker("volume", "inspect", v), Quiet: true, Limit: time.Minute}); err == nil {
			out = append(out, trace{Volume, v, "data"})
		}
	}
	if _, err := r.Exec(Cmd{Argv: in.docker("inspect", testDBName), Quiet: true, Limit: time.Minute}); err == nil {
		out = append(out, trace{TestDB, testDBName, ""})
	}
	for _, dir := range in.accounts() {
		path := in.settingsPath(dir)
		if raw, err := in.M.ReadFile(path); err == nil {
			if _, wired, err := Unwire(raw); err == nil && wired {
				out = append(out, trace{JSON, path, ""})
			}
		}
		if _, ok := in.mcpHas(dir); ok {
			out = append(out, trace{MCP, dir, "claude=" + in.claude() + " file=" + in.mcpFile(dir)})
		}
	}
	return out
}

// userUnits are the panel's units of the user: the executor, and the weekly
// prune of docker with its timer.
var userUnits = []string{execService, gcService, gcTimer}

func (in *Install) userUnitDir() string {
	return filepath.Join(in.home(), ".config", "systemd", "user")
}

func lingerPath(user string) string { return "/var/lib/systemd/linger/" + user }

// unowned are the traces the manifest does not hold.
func (in *Install) unowned(r *Run) []trace {
	var out []trace
	for _, t := range in.traces(r) {
		if !r.Recorded(t.kind, t.target) {
			out = append(out, t)
		}
	}
	return out
}

// adoptStep takes over what is in place and the manifest does not hold:
// an install by hand, or a root part an administrator ran. Nothing on the
// machine changes; the manifest learns of each thing, marked adopted, before
// a step after it changes it — and so records no line of its own for it.
func (in *Install) adoptStep() *Step {
	return &Step{ID: "adopt", Title: "Take over what is in place",
		Done: func(r *Run) (bool, error) { return len(in.unowned(r)) == 0, nil },
		Apply: func(r *Run) error {
			found := in.unowned(r)
			var names []string
			for _, t := range found {
				if err := r.Adopt(t.kind, t.target, t.meta); err != nil {
					return err
				}
				names = append(names, in.short(t.target))
			}
			r.Say(Pass, fmt.Sprintf("%s taken into the manifest as found: %s", count(len(found), "thing"), strings.Join(names, " · ")))
			if slicesHasKind(found, Linger) {
				r.Say(Note, "linger was on before the installer: uninstall leaves it on")
			}
			return nil
		},
		Undo: UndoKind,
	}
}

func slicesHasKind(ts []trace, k Kind) bool {
	for _, t := range ts {
		if t.kind == k {
			return true
		}
	}
	return false
}
