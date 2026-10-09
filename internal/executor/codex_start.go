package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/codex"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/launcher"
)

// codexEnv names the codex program in the machine description, the way
// AACP_CLAUDE names claude's: a router in front of codex chooses the home by
// the directory and would overrule the one the panel names.
const codexEnv = "AACP_CODEX"

// How long a daemon the executor started has to put up its socket, and a
// link to take its connection.
var (
	daemonWait = 30 * time.Second
	linkWait   = 15 * time.Second
	upPoll     = 100 * time.Millisecond
)

// codexOpen starts a codex session of a project: a thread in the daemon of
// the project's contour, the daemon started first where it does not run, with
// the model, the effort, the approvals and the sandbox the map chose. In the
// daemon the panel is the thread's client and holds it until it closes it,
// and sends the first message as its first turn. In tmux codex resumes the
// thread in a terminal and holds it there, the first message as its prompt;
// the thread is named after the project's session, which writes it to the
// disk for codex to resume.
func (e *Executor) codexOpen(ctx context.Context, p project, c launcher.CodexParams, warns []string) (string, string, error) {
	home, ok := registry.CodexHomeOf(p.ConfigDir)
	if !ok {
		return "", "", fmt.Errorf("contour %s has no codex home — %s names none after it — so codex has no daemon "+
			"to start a session of %s in", home.Contour, registry.CodexHomesEnv, p.Path)
	}
	link := e.codex.Home(home.Dir)
	if link == nil {
		return "", "", fmt.Errorf("the executor keeps no link to the codex home %s", home.Dir)
	}
	started, err := e.codexDaemon(ctx, link, p.Path)
	if err != nil {
		return "", "", err
	}

	tmux := c.Transport == launcher.TransportTmux
	begin := codex.Begin{CWD: p.Path, Model: c.Model, Approval: c.Approval, Sandbox: c.Sandbox, Effort: c.Effort}
	if tmux {
		begin.Name = p.Session
	} else {
		begin.Message, begin.Hold = c.Intent, true
	}
	id, err := link.Start(ctx, begin)
	if id == "" {
		return "", "", fmt.Errorf("codex did not start a thread in %s: %w", p.Path, err)
	}
	name := codex.SessionName(id)
	if err != nil {
		return "", name, fmt.Errorf("session %s started in %s, and %w", name, p.Path, err)
	}

	detail := fmt.Sprintf("session %s started in the codex daemon of contour %s", name, home.Contour)
	if tmux {
		rep, err := e.runCodexTerminal(ctx, p, home.Dir, id)
		if err != nil {
			return "", name, fmt.Errorf("thread %s started, and codex did not come up in tmux: %w — the daemon "+
				"unloads the thread once nobody joins it", name, err)
		}
		detail = fmt.Sprintf("session %s started: codex in tmux session %s, on the daemon of contour %s",
			name, rep.Session, home.Contour)
		warns = append(warns, rep.Warnings...)
	}
	if c.Intent != "" {
		detail += "; opening message: " + shortIntent(c.Intent)
	}
	if started {
		detail += "; the daemon of " + home.Dir + " was not running and was started"
	}
	for _, w := range warns {
		detail += "; WARNING: " + w
	}
	return detail, name, nil
}

// runCodexTerminal starts codex in tmux on a thread, through the launcher in
// a transient unit as claude is: a tmux server started from the executor
// would live in its unit and go down with it.
func (e *Executor) runCodexTerminal(ctx context.Context, p project, home, thread string) (launcher.Report, error) {
	bin, err := launcherPath()
	if err != nil {
		return launcher.Report{}, err
	}
	program, err := codexBin()
	if err != nil {
		return launcher.Report{}, err
	}
	return runInUnit(ctx, bin, launchFlag, "starting codex", launcher.Spec{
		Dir: p.Path, Session: p.Session, Launch: p.Launch,
		Codex: &launcher.CodexSpec{Thread: thread, Home: home, Bin: program},
	})
}

// codexDaemon makes sure the daemon of a home runs and the link to it is
// connected; it reports whether it started the daemon. A home without its
// control socket has no daemon, and the executor starts one in a transient
// unit of its own: started from the executor it would live in the executor's
// unit and its sandbox, and from a session in that session's unit, going down
// with whichever stops first.
func (e *Executor) codexDaemon(ctx context.Context, link *codex.Link, dir string) (bool, error) {
	e.daemons.Lock()
	defer e.daemons.Unlock()
	started := false
	if !link.Up() {
		bin, err := codexBin()
		if err != nil {
			return false, err
		}
		if err := startDaemon(ctx, bin, link.Home(), dir); err != nil {
			return false, err
		}
		started = true
		if err := waitUp(ctx, link, bin); err != nil {
			return false, err
		}
	}
	wctx, cancel := context.WithTimeout(ctx, linkWait)
	defer cancel()
	return started, link.Ready(wctx)
}

func startDaemon(ctx context.Context, bin, home, dir string) error {
	env := cleanEnv(os.Environ())
	cmd := exec.CommandContext(ctx, systemdRunPath(), daemonArgs(env, bin, home, dir)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		said := shortenLines(strings.TrimSpace(string(out)))
		return fmt.Errorf("the codex daemon of %s was not started (%s app-server daemon start): %w: %s", home, bin, err, said)
	}
	return nil
}

// daemonArgs start `codex app-server daemon start` in a transient unit of its
// own, in the project's directory and with the home named. The command puts
// the daemon in the background and ends; the unit leaves what it started
// running, as the launcher's unit leaves the tmux server.
func daemonArgs(env []string, bin, home, dir string) []string {
	args := []string{
		"--user",
		"--wait",
		"--collect",
		"--property=KillMode=process",
		"--description=codex app-server daemon of " + home,
		"--working-directory=" + dir,
	}
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); passEnv(name) {
			args = append(args, "--setenv="+kv)
		}
	}
	return append(args, "--setenv=CODEX_HOME="+home, "--", bin, "app-server", "daemon", "start")
}

// waitUp waits for the control socket of a daemon the executor started. A
// socket that does not come is most often a program that chose another home.
func waitUp(ctx context.Context, link *codex.Link, bin string) error {
	deadline := time.Now().Add(daemonWait)
	for !link.Up() {
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the codex daemon was started, and no control socket appeared in %s within %s: "+
				"%s may choose its home by itself — name the codex program itself in %s", link.Home(), daemonWait,
				bin, codexEnv)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the codex daemon of %s did not come up: the action ran out of time", link.Home())
		case <-time.After(upPoll):
		}
	}
	return nil
}

// codexBin is the codex program: the one the machine description names, or
// codex from the executor's PATH.
func codexBin() (string, error) {
	if p := os.Getenv(codexEnv); p != "" {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("the machine description starts codex through %s=%s, and that is no program "+
				"on the host — until the path is fixed, codex is not started", codexEnv, p)
		}
		return p, nil
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("codex not found: not in %s, not in PATH (%s) — the panel calls codex with the PATH "+
			"of its own unit; put the path into %s of the machine description", codexEnv, os.Getenv("PATH"), codexEnv)
	}
	return path, nil
}

// codexClose lets a codex session go: the turn it runs is interrupted, the
// panel leaves the thread, and the tmux session the launcher started codex in
// on it is closed by its exact name. The thread is not archived:
// the daemon unloads it once no client is left, and codex resume brings it
// back.
func (e *Executor) codexClose(ctx context.Context, th codex.Thread) (string, error) {
	interrupted, err := th.Link.Close(ctx, th.ID)
	if err != nil {
		return "", fmt.Errorf("session %s was not closed: %w", th.Name, err)
	}
	detail := fmt.Sprintf("session %s closed: the panel left the thread, and the daemon unloads it once no client "+
		"is left — codex resume brings it back", th.Name)
	if interrupted {
		detail += "; the turn that ran was interrupted"
	}
	closed, err := closeCodexTerminal(ctx, th.ID)
	if err != nil {
		return detail + "; WARNING: codex in tmux was not closed: " + err.Error(), nil
	}
	for _, name := range closed {
		detail += "; tmux session " + name + " closed with codex in it"
	}
	return detail, nil
}

// codexPanes lists the panes of the user's tmux with the command each was
// started with.
const codexPanes = "#{session_name}\t#{pane_start_command}"

// closeCodexTerminal kills the tmux sessions the launcher started codex in on
// a thread, each by its exact name.
func closeCodexTerminal(ctx context.Context, thread string) ([]string, error) {
	out, err := userTmux.run(ctx, "list-panes", "-a", "-F", codexPanes)
	if err != nil {
		if noTmuxServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var closed []string
	for _, line := range strings.Split(out, "\n") {
		name, command, ok := strings.Cut(line, "\t")
		if !ok || !launcher.LaunchedCodex(command, thread) || slices.Contains(closed, name) {
			continue
		}
		if _, err := userTmux.run(ctx, "kill-session", "-t", "="+name); err != nil {
			return closed, fmt.Errorf("tmux session %s: %w", name, err)
		}
		closed = append(closed, name)
	}
	return closed, nil
}
