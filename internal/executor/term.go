package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/termlink"
)

const (
	termShutdown = 2 * time.Second
	termTerm     = "xterm-256color"
)

// TermOpener opens terminal bridges to live sessions and to the terminals of the panel.
type TermOpener struct{}

// NewTermOpener creates a TermOpener.
func NewTermOpener() *TermOpener { return &TermOpener{} }

// Open attaches to the tmux pane of the named session, on the server the pane
// lives on.
func (o *TermOpener) Open(ctx context.Context, target string, cols, rows uint16) (termlink.Terminal, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}

	pane, err := tmuxPaneFor(ctx, s.PID)
	if err != nil {
		return nil, fmt.Errorf(
			"session %s does not live in tmux — there is nothing to attach to; a terminal is available "+
				"for sessions started by the panel and for claude typed into a terminal of the panel (%v)",
			s.Name, err)
	}
	return bridge(ctx, pane.Server, pane.Target, fmt.Sprintf("%s (pane %s)", s.Name, pane.Target), cols, rows)
}

// OpenTerm attaches to a terminal of the panel, found by its id among those on
// the panel's own tmux server.
func (o *TermOpener) OpenTerm(ctx context.Context, id string, cols, rows uint16) (termlink.Terminal, error) {
	t, err := findPanelTerm(ctx, id)
	if err != nil {
		return nil, err
	}
	return bridge(ctx, panelTmux, "="+t.ID+":", fmt.Sprintf("terminal %s in %s", t.ID, t.Place), cols, rows)
}

func bridge(ctx context.Context, srv tmuxServer, target, detail string, cols, rows uint16) (termlink.Terminal, error) {
	if _, err := srv.run(ctx, "set-window-option", "-t", target, "aggressive-resize", "on"); err != nil {
		log.Printf("terminal: aggressive-resize did not turn on for %s: %v", target, err)
	}
	if _, err := srv.run(ctx, "set-option", "-t", target, "mouse", "on"); err != nil {
		log.Printf("terminal: the mouse did not turn on for %s: %v", target, err)
	}

	win := tmuxWindowOf(target)
	size := srv.windowSize(ctx, win)
	sizeOpt := srv.windowSizeOption(ctx, win)
	sizePinned := pinWindowToSmallest(ctx, srv, win)

	pty, err := openPTY(cols, rows)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(tmuxBin(), srv.argv("attach-session", "-t", target)...)
	cmd.Env = termEnv()
	pty.attach(cmd)
	if err := cmd.Start(); err != nil {
		pty.Close()
		return nil, fmt.Errorf("tmux did not attach to %s: %w", target, err)
	}
	pty.closeSlave()

	t := &tmuxTerm{
		pty: pty, cmd: cmd, srv: srv, detail: detail, gone: make(chan struct{}),
		window: win, winSize: size, winSizeOpt: sizeOpt, sizePinned: sizePinned,
	}
	go func() {
		cmd.Wait()
		close(t.gone)
	}()
	return t, nil
}

const windowSizeSmallest = "smallest"

func pinWindowToSmallest(ctx context.Context, srv tmuxServer, window string) bool {
	if window == "" || srv.windowSizeRule(ctx, window) == "manual" {
		return false
	}
	if _, err := srv.run(ctx, "set-option", "-t", window, "-w", "window-size", windowSizeSmallest); err != nil {
		log.Printf("terminal: window %s stayed with whoever pressed a key last: %v", window, err)
		return false
	}
	return true
}

func termEnv() []string {
	lang := hostcfg.Load().Lang
	if lang == "" {
		lang = hostcfg.DefaultLang
	}
	env := []string{
		"TERM=" + termTerm,
		"LANG=" + lang,
		"PATH=" + os.Getenv("PATH"),
	}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	// The client looks for the server where the other calls of tmux found it.
	if dir := os.Getenv("TMUX_TMPDIR"); dir != "" {
		env = append(env, "TMUX_TMPDIR="+dir)
	}
	return env
}

type tmuxTerm struct {
	pty        *ptyPair
	cmd        *exec.Cmd
	srv        tmuxServer
	detail     string
	window     string
	winSize    string
	winSizeOpt string
	sizePinned bool
	gone       chan struct{}
	once       sync.Once
}

func (t *tmuxTerm) Read(p []byte) (int, error) { return t.pty.master.Read(p) }

func (t *tmuxTerm) Write(p []byte) (int, error) { return t.pty.master.Write(p) }

func (t *tmuxTerm) Resize(cols, rows uint16) error { return t.pty.resize(cols, rows) }

func (t *tmuxTerm) Kind() string { return "tmux" }

func (t *tmuxTerm) Detail() string { return t.detail }

func (t *tmuxTerm) Close() error {
	t.once.Do(func() {
		t.pty.Close()
		select {
		case <-t.gone:
		case <-time.After(termShutdown):
			if t.cmd.Process != nil {
				t.cmd.Process.Kill()
			}
			<-t.gone
		}
		t.restoreWindow()
	})
	return nil
}

func (t *tmuxTerm) restoreWindow() {
	if t.window == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), termShutdown)
	defer cancel()

	restoreRule := t.sizePinned

	if t.winSize != "" && !t.srv.sessionHasClients(ctx, tmuxSessionOf(t.window)) {
		if now := t.srv.windowSize(ctx, t.window); now != "" && now != t.winSize {
			if cols, rows, ok := strings.Cut(t.winSize, "x"); ok {
				if _, err := t.srv.run(ctx, "resize-window", "-t", t.window, "-x", cols, "-y", rows); err != nil {
					log.Printf("terminal: window %s stayed %s instead of %s: %v", t.window, now, t.winSize, err)
				} else {
					restoreRule = true
				}
			}
		}
	}
	if !restoreRule {
		return
	}
	var err error
	if t.winSizeOpt == "" {
		_, err = t.srv.run(ctx, "set-option", "-t", t.window, "-uw", "window-size")
	} else {
		_, err = t.srv.run(ctx, "set-option", "-t", t.window, "-w", "window-size", t.winSizeOpt)
	}
	if err != nil {
		log.Printf("terminal: window %s kept someone else's window-size: %v", t.window, err)
	}
}

// TermSocket returns the terminal socket path next to the action socket.
func TermSocket(actionSocket string) string {
	if actionSocket == "" {
		return ""
	}
	dir := actionSocket
	if i := strings.LastIndex(actionSocket, "/"); i >= 0 {
		dir = actionSocket[:i]
	}
	return dir + "/term.sock"
}
