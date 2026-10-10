package action

import (
	"context"
	"strings"
)

// AskCodexModels asks for the models codex offers: the catalogue the codex
// daemon of a home lists, with the efforts each model takes. It names no
// session — a launch parameter is picked before any session runs — and asks
// no account: the catalogue is codex's own.
const AskCodexModels = "codex.models"

// CodexModel is one model of the codex catalogue: the name codex takes, the
// one a person reads, the efforts it takes and the one it starts at.
type CodexModel struct {
	Model   string   `json:"model"`
	Name    string   `json:"name"`
	Efforts []string `json:"efforts"`
	Effort  string   `json:"effort,omitempty"`
}

// CodexNotRestarted is why a codex session is not restarted: the daemon of
// its home keeps the thread, and no process of the panel's is there to start
// again.
const CodexNotRestarted = "its thread is kept by the codex daemon, and there is nothing to restart — " +
	"close it and start another with New"

// CodexModelsAsker is an executor that knows the models codex offers. A
// contour names the daemon to ask; empty asks the first that answers.
type CodexModelsAsker interface {
	CodexModels(ctx context.Context, contour string) ([]CodexModel, error)
}

// CodexCommands are the commands of codex's own terminal client a codex
// session takes from the panel, the way Commands are claude's. compact is in
// both lists; the others are codex's alone, and a claude session refuses them.
// A review and a goal say what they do in a field of their own, never by
// parsing words: review looks at what Review names, goal does what Goal says,
// and stop stops every background terminal of the session, as codex's /stop.
var CodexCommands = map[string]bool{
	"compact": true,
	"review":  true,
	"goal":    true,
	"stop":    true,
}

// What a review looks at.
const (
	ReviewUncommitted = "uncommitted"
	ReviewBranch      = "branch"
	ReviewCommit      = "commit"
	ReviewCustom      = "custom"
)

// Review is what codex's /review looks at: the uncommitted changes, the work
// against a base branch, one commit (with its title, for the feed), or what
// the person describes in words of their own.
type Review struct {
	Target       string `json:"target"`
	Branch       string `json:"branch,omitempty"`
	Commit       string `json:"commit,omitempty"`
	Title        string `json:"title,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// What goal does.
const (
	GoalSet    = "set"
	GoalPause  = "pause"
	GoalResume = "resume"
	GoalClear  = "clear"
)

// Goal is what codex's /goal does: sets an objective, with a budget of
// tokens or none; pauses the goal, resumes it, or clears it.
type Goal struct {
	Do        string `json:"do"`
	Objective string `json:"objective,omitempty"`
	Budget    int64  `json:"budget,omitempty"`
}

// The ceilings of what a codex command carries.
const (
	branchMax     = 200
	goalBudgetMax = 1_000_000_000
)

func (c *Command) validateCodex() error {
	switch {
	case c.Review != nil && c.Name != "review":
		return badRequest("command /%s looks at no review", c.Name)
	case c.Goal != nil && c.Name != "goal":
		return badRequest("command /%s sets no goal", c.Name)
	case c.Name == "review" && c.Review == nil:
		return badRequest("a review that does not say what to look at")
	case c.Name == "goal" && c.Goal == nil:
		return badRequest("a goal that does not say what to do")
	}
	if r := c.Review; r != nil {
		switch r.Target {
		case ReviewUncommitted:
		case ReviewBranch:
			if err := refName(r.Branch, "branch"); err != nil {
				return err
			}
		case ReviewCommit:
			if len(r.Commit) < 4 || len(r.Commit) > 64 || strings.Trim(strings.ToLower(r.Commit), "0123456789abcdef") != "" {
				return badRequest("a commit is named by its hash, not %q", r.Commit)
			}
			if err := oneLine(r.Title, "the title of the commit", branchMax); err != nil {
				return err
			}
		case ReviewCustom:
			if strings.TrimSpace(r.Instructions) == "" {
				return badRequest("a review of your own says what to look at")
			}
			if len([]rune(r.Instructions)) > AskTextMax {
				return badRequest("what to review is longer than %d characters", AskTextMax)
			}
			if err := safeText(r.Instructions); err != nil {
				return err
			}
		default:
			return badRequest("a review looks at %s, %s, %s or %s, not %q", ReviewUncommitted, ReviewBranch,
				ReviewCommit, ReviewCustom, r.Target)
		}
		if (r.Branch != "" && r.Target != ReviewBranch) || ((r.Commit != "" || r.Title != "") && r.Target != ReviewCommit) ||
			(r.Instructions != "" && r.Target != ReviewCustom) {
			return badRequest("a review of %s carries only what that review looks at", r.Target)
		}
	}
	if g := c.Goal; g != nil {
		switch g.Do {
		case GoalSet:
			if strings.TrimSpace(g.Objective) == "" {
				return badRequest("a goal without its objective")
			}
			if len([]rune(g.Objective)) > AskTextMax {
				return badRequest("the objective is longer than %d characters", AskTextMax)
			}
			if err := safeText(g.Objective); err != nil {
				return err
			}
			if g.Budget < 0 || g.Budget > goalBudgetMax {
				return badRequest("a budget of %d tokens is outside 0..%d", g.Budget, goalBudgetMax)
			}
		case GoalPause, GoalResume, GoalClear:
			if g.Objective != "" || g.Budget != 0 {
				return badRequest("to %s a goal takes no objective and no budget", g.Do)
			}
		default:
			return badRequest("a goal is set, paused, resumed or cleared, not %q", g.Do)
		}
	}
	return nil
}

// refName checks the name of a git branch the way a command line would take
// it: one word, no way out of a path.
func refName(name, what string) error {
	if name == "" {
		return badRequest("a review against a %s that names none", what)
	}
	if len(name) > branchMax || name[0] == '-' || strings.Contains(name, "..") {
		return badRequest("%q is not a name of a %s", name, what)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_./+@", r):
		default:
			return badRequest("the %s %q holds %q", what, name, r)
		}
	}
	return nil
}

func oneLine(s, what string, limit int) error {
	if len([]rune(s)) > limit {
		return badRequest("%s is longer than %d characters", what, limit)
	}
	if strings.ContainsAny(s, "\n\r") {
		return badRequest("%s spans several lines", what)
	}
	return safeText(s)
}

// CodexName says a session name is the one the panel gives a codex thread:
// codex- and the last eight hex digits of its id.
func CodexName(name string) bool {
	tail, ok := strings.CutPrefix(name, "codex-")
	return ok && len(tail) == 8 && strings.Trim(tail, "0123456789abcdef") == ""
}

// threadNameMax is how long a name given to a codex thread may be: it is a
// title shown beside the session, not the key the session is found by.
const threadNameMax = 120

// safeThreadName checks a name given to a codex thread: any printable words,
// in any script and with spaces, on one line.
func safeThreadName(s string) error {
	if strings.TrimSpace(s) == "" {
		return badRequest("the new name of the thread is empty")
	}
	if len([]rune(s)) > threadNameMax {
		return badRequest("the new name is longer than %d characters", threadNameMax)
	}
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return badRequest("the new name contains a control character %q", r)
		}
	}
	return nil
}

// ValidSessionName checks a name given to a claude session: it becomes the
// key the panel and the host find the session by.
func ValidSessionName(s string) error { return safeSessionName(s) }

// AskProcesses asks a codex session for the background terminals its turns
// left running.
const AskProcesses = "processes"

// Process is a background terminal of a codex session: a command a turn
// started and left running, where it runs, its pid, and what it takes — CPU
// as a percentage and memory in kilobytes, absent where codex could not read
// them. The id is what task.stop names it by.
type Process struct {
	ID      string   `json:"id"`
	Command string   `json:"command"`
	CWD     string   `json:"cwd"`
	PID     *int64   `json:"pid,omitempty"`
	CPU     *float64 `json:"cpu,omitempty"`
	RSS     *int64   `json:"rssKb,omitempty"`
}

// ProcessesAsker is an executor that knows the background terminals of a
// codex session.
type ProcessesAsker interface {
	Processes(ctx context.Context, target string) ([]Process, error)
}
