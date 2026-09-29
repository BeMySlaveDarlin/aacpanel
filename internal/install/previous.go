package install

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/hostcfg"
)

// previous finds what an earlier install left and so the mode of the run: the
// manifest makes it an upgrade, traces without one an install by hand to
// adopt, nothing a fresh install. Two traces stop the run instead: a
// database volume whose password is gone, and a machine description that
// belongs to another clone.
func (c *checker) previous() {
	home := c.f.Account.Home
	stops := len(c.out)

	hostEnv := filepath.Join(c.f.StateDir, "host.env")
	if raw, err := c.m.ReadFile(hostEnv); err == nil {
		c.f.Traces = append(c.f.Traces, hostEnv)
		if repo := hostcfg.Parse(raw)[hostcfg.RepoEnv]; repo != "" && !c.same(repo, c.f.Clone) {
			c.add(Stop, "stop: %s names another clone (%s): run the installer from there or change AACP_REPO.",
				hostEnv, repo)
		}
	}
	dotEnv := filepath.Join(c.f.Clone, ".env")
	env, envErr := c.m.ReadFile(dotEnv)
	if envErr == nil {
		c.f.Traces = append(c.f.Traces, dotEnv)
	}
	for _, p := range []string{
		filepath.Join(home, "bin", "aacpanel-exec"),
		filepath.Join(home, ".config", "systemd", "user", "aacpanel-exec.service"),
		"/etc/systemd/system/aacpanel-agent@.service",
	} {
		if _, err := c.m.Stat(p); err == nil {
			c.f.Traces = append(c.f.Traces, p)
		}
	}
	if c.dockerUp {
		var ours []string
		for _, ct := range c.dockerPS() {
			if ct.project == Project {
				ours = append(ours, ct.name)
			}
		}
		if len(ours) > 0 {
			c.f.Traces = append(c.f.Traces, "compose project "+Project+": "+strings.Join(ours, ", "))
		}
		if out, err := c.m.Run("docker", "volume", "ls", "--format", "{{.Name}}"); err == nil &&
			slices.Contains(strings.Fields(out), DBVolume) {
			c.f.Traces = append(c.f.Traces, "volume "+DBVolume)
			if envErr != nil || hostcfg.Parse(env)["AACP_DB_PASSWORD"] == "" {
				c.add(Stop, "stop: the database volume %s exists, but the .env with its password is missing. "+
					"Put back the .env of that install, or delete the volume with everything in it "+
					"(docker volume rm %s: the map, the journal, the enrolled devices) and run again.",
					DBVolume, DBVolume)
			}
		}
	}

	_, manifest := c.m.Stat(filepath.Join(c.f.InstallDir, ManifestName))
	switch {
	case manifest == nil:
		c.f.Mode = Upgrade
	case len(c.f.Traces) > 0:
		c.f.Mode = Adopt
	default:
		c.f.Mode = Fresh
	}
	if c.stoppedSince(stops) {
		return
	}
	switch c.f.Mode {
	case Upgrade:
		c.add(Pass, "the manifest of an earlier install is here: this run updates it")
	case Adopt:
		c.add(Pass, "the panel is here, installed without the installer: this run takes it over")
	default:
		c.add(Pass, "no trace of the panel: a fresh install")
	}
}

// same tells whether two paths are one place, symlinks resolved.
func (c *checker) same(a, b string) bool {
	if ra, err := c.m.Real(a); err == nil {
		a = ra
	}
	if rb, err := c.m.Real(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// Check is the step "Check the machine": the lines Inspect found, said in
// their order. It changes nothing, so it is a step of Verify alone, and it
// fails when a line stops the install.
func Check(in Inspection) *Step {
	return &Step{ID: "preflight", Title: "Check the machine", Verify: func(r *Run) error {
		for _, f := range in.Findings {
			r.Say(f.Mark, f.Text)
		}
		if n := in.Stops(); n > 0 {
			return &Stopped{Lines: n}
		}
		return nil
	}}
}

// Stopped is the check failing: the lines under the step say why.
type Stopped struct{ Lines int }

func (s *Stopped) Error() string {
	if s.Lines == 1 {
		return "one line of the check stops the install"
	}
	return strconv.Itoa(s.Lines) + " lines of the check stop the install"
}
