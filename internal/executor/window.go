package executor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

const clientFormat = "#{client_tty} #{client_pid}"

type tmuxClient struct {
	TTY string
	PID int
}

func (e *Executor) windowOpen(ctx context.Context, target string) (string, error) {
	pane, err := windowPane(ctx, target)
	if err != nil {
		return "", err
	}

	dir, err := paneDir(ctx, pane.Target)
	if err != nil {
		return "", err
	}

	return e.openWindowTo(ctx, dir, tmuxSessionOf(pane.Target))
}

// openWindowTo opens a terminal window on the host attached to a tmux session
// and waits for it to attach.
func (e *Executor) openWindowTo(ctx context.Context, dir, name string) (string, error) {
	rep, err := e.runWindowOpener(ctx, dir, name)
	if err != nil {
		return "", err
	}
	detail := describeWindow(rep)

	if !waitForeignClient(ctx, name) {
		detail += fmt.Sprintf("; WARNING: nobody attached to the session within %s — the window may not have opened",
			windowWait)
	}
	return detail, nil
}

var (
	windowWait = 5 * time.Second
	windowPoll = 200 * time.Millisecond
)

func waitForeignClient(ctx context.Context, session string) bool {
	deadline := time.Now().Add(windowWait)
	for {
		if clients, err := foreignClients(ctx, session); err == nil && len(clients) > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(windowPoll):
		}
	}
}

func (e *Executor) windowClose(ctx context.Context, target string) (string, error) {
	pane, err := windowPane(ctx, target)
	if err != nil {
		return "", err
	}
	name := tmuxSessionOf(pane.Target)

	clients, err := foreignClients(ctx, name)
	if err != nil {
		return "", err
	}
	if len(clients) == 0 {
		return "session " + name + " had no windows", nil
	}

	var closed, failed []string
	for _, c := range clients {
		if _, err := tmuxRun(ctx, "detach-client", "-t", c.TTY); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", c.TTY, err))
			continue
		}
		closed = append(closed, c.TTY)
	}

	switch {
	case len(closed) == 0:
		return "", fmt.Errorf("the window of session %s did not close: %s", name, strings.Join(failed, "; "))
	case len(failed) > 0:
		return "", fmt.Errorf("detached %s, failed to detach %s",
			strings.Join(closed, ", "), strings.Join(failed, "; "))
	case len(closed) == 1:
		return "the window of session " + name + " is closed", nil
	default:
		return fmt.Sprintf("windows closed for session %s: %d", name, len(closed)), nil
	}
}

// Window answers the window question for the named session.
func (e *Executor) Window(ctx context.Context, target string) (*action.Window, error) {
	pane, err := windowPane(ctx, target)
	if err != nil {
		return nil, err
	}
	clients, err := foreignClients(ctx, tmuxSessionOf(pane.Target))
	if err != nil {
		return nil, err
	}
	return &action.Window{Open: len(clients) > 0}, nil
}

func windowPane(ctx context.Context, target string) (tmuxPane, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return tmuxPane{}, err
	}
	pane, err := tmuxPaneFor(ctx, s.PID)
	if err != nil {
		return tmuxPane{}, fmt.Errorf(
			"session %s does not live in tmux — windows for it cannot be opened or closed: %w", s.Name, err)
	}
	return pane, nil
}

func paneDir(ctx context.Context, target string) (string, error) {
	out, err := tmuxRun(ctx, "display", "-p", "-t", target, "#{pane_current_path}")
	if err != nil {
		return "", fmt.Errorf("tmux did not tell the directory of pane %s: %w", target, err)
	}
	dir := strings.TrimSpace(out)
	if !strings.HasPrefix(dir, "/") {
		return "", fmt.Errorf("tmux named %q as the directory of pane %s — there is nowhere to open a window", dir, target)
	}
	return dir, nil
}

func foreignClients(ctx context.Context, session string) ([]tmuxClient, error) {
	if session == "" {
		return nil, fmt.Errorf("the tmux session name is empty: whose clients to count is unknown")
	}
	out, err := tmuxRun(ctx, "list-clients", "-t", session, "-F", clientFormat)
	if err != nil {
		return nil, fmt.Errorf("tmux did not tell about the clients of session %s: %w", session, err)
	}

	self := os.Getpid()
	var clients []tmuxClient
	for _, line := range strings.Split(out, "\n") {
		tty, raw, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || tty == "" {
			continue
		}
		pid, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		if parent, ok := procParent(pid); ok && parent == self {
			continue
		}
		clients = append(clients, tmuxClient{TTY: tty, PID: pid})
	}
	return clients, nil
}

func describeWindow(rep launcher.Report) string {
	detail := "a window to session " + rep.Session + " is open"
	for _, w := range rep.Warnings {
		detail += "; WARNING: " + w
	}
	return detail
}
