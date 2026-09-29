package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"aacpanel/internal/hostcfg"
)

// TermPlaceOption is the tmux session option a terminal of the panel keeps
// its place in: the directory it was started in, which the shell may leave.
const TermPlaceOption = "@aacp_place"

// TermSpec is a request to start a terminal of the panel.
type TermSpec struct {
	Dir     string `json:"dir"`
	Session string `json:"session"`
	// Socket names the tmux server of the panel's terminals, kept apart from
	// the user's own, where the sessions of claude live.
	Socket string `json:"socket"`
}

// Term starts a terminal of the panel: a tmux session on the server of its
// own socket, in the given directory, running the user's default shell. It is
// given no command line, so the shell is all that runs, and what the shell
// does is what the person types into it. The window names itself after what
// runs in it.
//
// It runs from a unit of its own, as a session does: the first terminal starts
// the server, and a server started inside the executor would hand every shell
// the executor's sandbox and die with its restart.
func Term(spec TermSpec) (Report, error) {
	if spec.Session == "" {
		return Report{}, fmt.Errorf("a terminal without a name: there is nothing to start")
	}
	if spec.Socket == "" {
		return Report{}, fmt.Errorf("terminal %s names no socket of its own: it would start among the sessions of claude",
			spec.Session)
	}
	if spec.Dir == "" || !strings.HasPrefix(spec.Dir, "/") {
		return Report{}, fmt.Errorf("terminal directory %q is not absolute", spec.Dir)
	}

	host := hostcfg.Load()
	env, warns := childEnv(os.Environ(), Params{}, host.Display, host.Lang, "")

	tmuxBin := tool(tmuxEnv, "tmux")
	cmd := exec.Command(tmuxBin, termArgv(spec)...)
	cmd.Dir = spec.Dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		said := strings.TrimSpace(string(out))
		if said == "" {
			return Report{}, fmt.Errorf("tmux did not start terminal %s (%s): %w", spec.Session, tmuxBin, err)
		}
		return Report{}, fmt.Errorf("tmux did not start terminal %s (%s): %w: %s", spec.Session, tmuxBin, err, said)
	}
	return Report{Session: spec.Session, Dir: spec.Dir, Warnings: warns}, nil
}

// termArgv starts the session and sets it up in one command sequence, so a
// list of the terminals never shows one without its place.
func termArgv(spec TermSpec) []string {
	target := "=" + spec.Session + ":"
	return []string{
		"-L", spec.Socket,
		"new-session", "-d", "-s", spec.Session, "-c", spec.Dir,
		";", "set-option", "-t", target, TermPlaceOption, spec.Dir,
		";", "set-option", "-w", "-t", target, "automatic-rename", "on",
	}
}
