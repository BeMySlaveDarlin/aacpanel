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
	Command string `json:"command,omitempty"`
	Session string `json:"session"`
	// Socket names the tmux server of the panel's terminals, kept apart from
	// the user's own, where the sessions of claude live.
	Socket string `json:"socket"`
}

// Term starts a terminal of the panel: a tmux session on the server of its
// own socket, in the given directory, running the user's default shell or the
// command of a human step. A command keeps its dead pane so the service can read its status and final output,
// and runs with no input.
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
	args := []string{"-L", spec.Socket, "new-session", "-d", "-s", spec.Session, "-c", spec.Dir}
	// A command reads nothing: its input is /dev/null, so a question it asks
	// meets the end of input at once, and a program that asks only on a
	// terminal does not ask. A step waiting on a prompt nobody watches would
	// hold its chain for good.
	if spec.Command != "" {
		args = append(args, "sh", "-c", "exec </dev/null\n"+spec.Command)
	}
	// tmux processes this command queue before reaping an exited pane.
	if spec.Command != "" {
		args = append(args, ";", "set-option", "-w", "-t", target, "remain-on-exit", "on")
	}
	return append(args, []string{
		";", "set-option", "-t", target, TermPlaceOption, spec.Dir,
		";", "set-option", "-w", "-t", target, "automatic-rename", "on",
	}...)
}
