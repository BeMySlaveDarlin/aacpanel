package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/action"
)

var (
	softWait  = 15 * time.Second
	pollEvery = time.Second
)

type agentProc struct {
	Session string
	Agent   int
	Konsole int
	Dir     string
	// Home marks the host's main session: the one running in the home directory.
	Home bool
}

func (e *Executor) sessionClose(ctx context.Context, target string) (string, error) {
	p, err := findAgent(target)
	if err != nil {
		return "", err
	}
	if s, err := findOneLiveSession(target); err == nil && onStream(s) {
		return e.closeGently(ctx, s, p, true)
	}
	return e.closeAgent(ctx, p)
}

// sessionRestart ends a session the gentle way and starts a new one in the
// same place with an empty context. The panel names the project the session
// belongs to, with its launch parameters from the map and the message after a
// restart as its first one: a project session started without them would come
// up in another setup — the console instead of the feed, another model,
// possibly another account. Without a project only the host's main session is
// restarted, since its launch is fixed: the home directory, the same name.
func (e *Executor) sessionRestart(ctx context.Context, target string, want *action.Project) (string, error) {
	p, err := lookupAgent(target)
	if err != nil {
		return "", err
	}
	if want != nil {
		return e.restartFromMap(ctx, target, p, want)
	}
	if !p.Home {
		return "", fmt.Errorf(
			"session %s runs in %s, not in the home directory: a restart from scratch is for the host's "+
				"main session only. Other sessions are closed here and opened again from the projects map, "+
				"which carries their launch parameters",
			p.Session, p.Dir)
	}

	closed, err := e.closeAgent(ctx, p)
	if err != nil {
		return "", fmt.Errorf("the restart stopped at closing, nothing was started: %w", err)
	}

	rep, err := e.runLauncher(ctx, homeProject(p.Dir, p.Session), "")
	if err != nil {
		return "", fmt.Errorf("%s; the new session did not start: %w", closed, err)
	}
	return closed + "; " + describeConsole(rep) + " with an empty context", nil
}

func (e *Executor) restartFromMap(ctx context.Context, target string, p agentProc, want *action.Project) (string, error) {
	next, err := chooseProject(target, want)
	if err != nil {
		return "", err
	}
	var closed string
	if s, err := findOneLiveSession(target); err == nil && onStream(s) {
		closed, err = e.closeGently(ctx, s, p, true)
		if err != nil {
			return "", fmt.Errorf("the restart stopped at closing, nothing was started: %w", err)
		}
	} else if closed, err = e.closeAgent(ctx, p); err != nil {
		return "", fmt.Errorf("the restart stopped at closing, nothing was started: %w", err)
	}
	rep, err := e.runLauncher(ctx, next, "")
	if err != nil {
		return "", fmt.Errorf("%s; the new session did not start: %w", closed, err)
	}
	return closed + "; " + describeConsole(rep) + " with an empty context and the project's parameters", nil
}

func (e *Executor) closeAgent(ctx context.Context, p agentProc) (string, error) {
	tmuxName := ""
	if pane, err := tmuxPaneFor(ctx, p.Agent); err == nil {
		tmuxName = tmuxSessionOf(pane.Target)
	}

	if p.Konsole > 0 {
		_ = e.signal(p.Konsole, syscall.SIGTERM)
	}
	if err := e.signal(p.Agent, syscall.SIGTERM); err != nil {
		return "", fmt.Errorf("could not send TERM to agent %d of session %s: %w", p.Agent, p.Session, err)
	}

	if e.waitGone(ctx, p.Agent, e.softWait()) {
		_ = killTmuxSession(ctx, tmuxName)
		return fmt.Sprintf("session %s closed gracefully, the transcript is complete", p.Session), nil
	}
	return "", fmt.Errorf(
		"session %s did not close within %s: agent %d is still alive. "+
			"Only kill -9 is left — it can leave the transcript incomplete, and /resume will not bring it back",
		p.Session, e.softWait(), p.Agent)
}

func (e *Executor) sessionKill(ctx context.Context, target string) (string, error) {
	p, err := findAgent(target)
	if err != nil {
		return "", err
	}

	if p.Konsole > 0 {
		_ = e.signal(p.Konsole, syscall.SIGKILL)
	}
	if err := e.signal(p.Agent, syscall.SIGKILL); err != nil {
		return "", fmt.Errorf("could not send KILL to agent %d of session %s: %w", p.Agent, p.Session, err)
	}
	if e.waitGone(ctx, p.Agent, 5*time.Second) {
		return fmt.Sprintf("session %s killed; the transcript may have stayed incomplete", p.Session), nil
	}
	return "", fmt.Errorf("session %s: agent %d survived KILL — look on the host", p.Session, p.Agent)
}

func (e *Executor) waitGone(ctx context.Context, pid int, limit time.Duration) bool {
	alive := func() bool { return e.signal(pid, 0) == nil }

	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if !alive() {
			return true
		}
		select {
		case <-ctx.Done():
			return !alive()
		case <-time.After(e.pollEvery()):
		}
	}
	return !alive()
}

func (e *Executor) signal(pid int, sig syscall.Signal) error {
	if sig != 0 && isAncestor(pid) {
		return fmt.Errorf("refusing to send %v to process %d: it is the executor itself or its ancestor", sig, pid)
	}
	if e.sendSignal != nil {
		return e.sendSignal(pid, sig)
	}
	return syscall.Kill(pid, sig)
}

func (e *Executor) softWait() time.Duration {
	if e.soft > 0 {
		return e.soft
	}
	return softWait
}

func (e *Executor) pollEvery() time.Duration {
	if e.poll > 0 {
		return e.poll
	}
	return pollEvery
}

// findAgent finds a live session by name for the actions that end it. The
// host's main session is not among them: the panel does not close it, just as
// it does not stop its own container.
func findAgent(target string) (agentProc, error) {
	p, err := lookupAgent(target)
	if err != nil {
		return agentProc{}, err
	}
	if p.Home {
		return agentProc{}, fmt.Errorf(
			"session %s runs in the home directory — it is the host's main session, and it is not closed from here",
			p.Session)
	}
	return p, nil
}

func lookupAgent(target string) (agentProc, error) {
	if strings.TrimSpace(target) == "" {
		return agentProc{}, fmt.Errorf("the session name is empty")
	}
	home, _ := os.UserHomeDir()

	pids, err := claudePIDs()
	if err != nil {
		return agentProc{}, err
	}

	var known []string
	for _, pid := range pids {
		name, ok := sessionName(pid)
		if !ok {
			continue
		}
		known = append(known, name)
		if name != target {
			continue
		}

		dir := procCwd(pid)
		if isAncestor(pid) {
			return agentProc{}, fmt.Errorf(
				"session %s is the one the executor itself runs in: closing it would leave it unable to report the result",
				name)
		}
		return agentProc{
			Session: name, Agent: pid, Konsole: konsoleOf(pid), Dir: dir,
			Home: home != "" && dir == strings.TrimRight(home, "/"),
		}, nil
	}

	if len(known) == 0 {
		return agentProc{}, fmt.Errorf("there is no session %q: not a single named claude session is running", target)
	}
	return agentProc{}, fmt.Errorf("there is no session %q among the running ones: %s", target, strings.Join(known, ", "))
}
