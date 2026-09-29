package install

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// ---- update ----

// UpdatedEnv is where an update that moved the clone tells the installer it
// hands over to — the one built from the new tree — which commit the clone
// was on: that one does not move the clone again, and names the settings
// the new tree brought.
const UpdatedEnv = "AACP_INSTALL_UPDATED"

// Moved is what an update did to the clone.
type Moved struct {
	From, To string // the commits before and after
	// Says is how it moved, for the feed: the branch pulled, the releases.
	Says string
	// Ahead is a release of a later paradigm, which an update takes only
	// with --to.
	Ahead string
}

// git runs git in the clone for an update and gives what it printed,
// trimmed; a failure stops the update with what git said.
func git(r *Run, clone string, args ...string) (string, error) {
	out, err := r.Exec(Cmd{Argv: append([]string{"git", "-C", clone}, args...), Env: []string{"GIT_TERMINAL_PROMPT=0"}, Limit: 5 * time.Minute})
	return strings.TrimSpace(out), err
}

// MoveClone takes the clone forward: a clone on a branch — main, as a
// developer has it — pulls what is there without a merge; a clone on a
// release goes to the newest release of its paradigm, or of the paradigm
// --to names (paradigm below zero is none). Changes of the person's own in
// the tracked files stop it before anything moves.
func MoveClone(r *Run, clone string, paradigm int) (Moved, error) {
	if dirty, err := git(r, clone, "status", "--porcelain", "--untracked-files=no"); err != nil {
		return Moved{}, fail("git status fails in "+clone, err)
	} else if dirty != "" {
		return Moved{}, &Failed{Diagnosis: "the clone has changes of its own, and an update would have to mix them in",
			Fix: append([]string{"git -C " + clone + " status names them: commit them or put them aside, then ./install.sh update."}, strings.Split(dirty, "\n")...)}
	}
	from, err := git(r, clone, "rev-parse", "HEAD")
	if err != nil {
		return Moved{}, fail("git rev-parse HEAD fails in "+clone, err)
	}
	m := Moved{From: from, To: from}
	if branch, err := git(r, clone, "symbolic-ref", "-q", "--short", "HEAD"); err == nil && branch != "" {
		if paradigm >= 0 {
			return m, &Failed{Diagnosis: "--to moves a clone that is on a release, and this one is on the branch " + branch,
				Fix: []string{"./install.sh update pulls " + branch + "; git -C " + clone + " checkout vP.M.m puts it on a release."}}
		}
		if _, err := git(r, clone, "pull", "--ff-only"); err != nil {
			return m, fail("git pull --ff-only fails on "+branch+": the branch went apart from its remote", err,
				"git -C "+clone+" status and git log say how; ./install.sh update pulls once it goes forward only.")
		}
		m.Says = "the branch " + branch + ", pulled"
	} else {
		at, _ := git(r, clone, "tag", "--points-at", "HEAD")
		current, ok := NewestRelease(strings.Fields(at))
		if !ok {
			return m, &Failed{Diagnosis: "the clone is on no release and on no branch: an update does not know where it goes",
				Fix: []string{"git -C " + clone + " checkout main, or a release: git -C " + clone + " checkout vP.M.m"}}
		}
		if _, err := git(r, clone, "fetch", "--tags", "--quiet"); err != nil {
			return m, fail("git fetch --tags fails: the releases were not read", err)
		}
		list, err := git(r, clone, "tag", "--list", "v*")
		if err != nil {
			return m, fail("git tag --list fails", err)
		}
		if paradigm < 0 {
			paradigm = current.Paradigm
		}
		target, err := PickUpdate(current, strings.Fields(list), paradigm)
		if err != nil {
			return m, &Failed{Diagnosis: err.Error()}
		}
		if target.Ahead != nil {
			m.Ahead = target.Ahead.String()
		}
		if target.To == current {
			m.Says = current.String() + ", the newest release of paradigm " + fmt.Sprint(current.Paradigm)
			return m, nil
		}
		if _, err := git(r, clone, "-c", "advice.detachedHead=false", "checkout", "--quiet", target.To.String()); err != nil {
			return m, fail("git checkout "+target.To.String()+" fails", err)
		}
		m.Says = current.String() + " → " + target.To.String()
	}
	if m.To, err = git(r, clone, "rev-parse", "HEAD"); err != nil {
		return m, fail("git rev-parse HEAD fails in "+clone, err)
	}
	return m, nil
}

// NewKeys are the keys the .env template of the clone has now and had not
// at the commit an update came from, that the .env does not set: a new
// setting the person may want. The installer sets the keys it owns itself.
func NewKeys(r *Run, clone, from string, env map[string]string) []string {
	before, err := git(r, clone, "show", from+":.env.example")
	if err != nil {
		return nil
	}
	now, err := git(r, clone, "show", "HEAD:.env.example")
	if err != nil {
		return nil
	}
	had := templateLines([]byte(before))
	var out []string
	for _, k := range slices.Sorted(maps.Keys(templateLines([]byte(now)))) {
		if _, ok := had[k]; ok {
			continue
		}
		if _, set := env[k]; !set {
			out = append(out, k)
		}
	}
	return out
}

// updateStep is the first step of an update: the images of the stack that
// are pulled rather than built, fresh from their registries, and what the
// new tree asks of the person.
func (in *Install) updateStep() *Step {
	return &Step{ID: "update", Title: "Pull the images",
		Apply: func(r *Run) error {
			services := []string{"socket-proxy", "aacpanel-db"}
			if in.S.Has("tailscale") {
				services = append(services, "tailscale")
			}
			if _, err := r.Exec(Cmd{Argv: in.docker(append([]string{"compose", "pull"}, services...)...), Dir: in.clone()}); err != nil {
				why, fix := stackDiagnosis(err)
				if why == "docker compose failed" {
					why = "docker compose pull failed: the registry did not answer"
				}
				return fail(why, err, fix...)
			}
			r.Say(Pass, "pulled: "+strings.Join(services, ", "))
			if in.UpdatedFrom != "" {
				_, env, _ := in.readEnv(r)
				if keys := NewKeys(r, in.clone(), in.UpdatedFrom, env); len(keys) > 0 {
					r.Remind("New settings in .env.example since the last install: " + strings.Join(keys, ", ") +
						". They are optional; the file says what each does.")
				}
			}
			r.Remind("When claude updates itself and the feed of a session on the stream breaks: python3 deploy/claude/stream-contract.py names the request that changed.")
			return nil
		},
	}
}
