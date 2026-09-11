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

// TermOpener opens terminal bridges to live sessions.
type TermOpener struct{}

// NewTermOpener creates a TermOpener.
func NewTermOpener() *TermOpener { return &TermOpener{} }

// Open attaches to the tmux pane of the named session.
func (o *TermOpener) Open(ctx context.Context, target string, cols, rows uint16) (termlink.Terminal, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}

	pane, err := tmuxPaneFor(ctx, s.PID)
	if err != nil {
		return nil, fmt.Errorf(
			"session %s does not live in tmux — there is nothing to attach to; a terminal is available "+
				"for sessions started by the panel (%v)",
			s.Name, err)
	}

	if _, err := tmuxRun(ctx, "set-window-option", "-t", pane.Target, "aggressive-resize", "on"); err != nil {
		log.Printf("terminal: aggressive-resize did not turn on for %s: %v", pane.Target, err)
	}
	if _, err := tmuxRun(ctx, "set-option", "-t", pane.Target, "mouse", "on"); err != nil {
		log.Printf("terminal: the mouse did not turn on for %s: %v", pane.Target, err)
	}

	win := tmuxWindowOf(pane.Target)
	size := tmuxWindowSize(ctx, win)
	sizeOpt := tmuxWindowSizeOption(ctx, win)
	sizePinned := pinWindowToSmallest(ctx, win)

	pty, err := openPTY(cols, rows)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(tmuxBin(), "attach-session", "-t", pane.Target)
	cmd.Env = termEnv()
	pty.attach(cmd)
	if err := cmd.Start(); err != nil {
		pty.Close()
		return nil, fmt.Errorf("tmux did not attach to %s: %w", pane.Target, err)
	}
	pty.closeSlave()

	t := &tmuxTerm{
		pty: pty, cmd: cmd, target: pane.Target, session: s.Name, gone: make(chan struct{}),
		window: win, winSize: size, winSizeOpt: sizeOpt, sizePinned: sizePinned,
	}
	go func() {
		cmd.Wait()
		close(t.gone)
	}()
	return t, nil
}

const windowSizeSmallest = "smallest"

func pinWindowToSmallest(ctx context.Context, window string) bool {
	if window == "" || tmuxWindowSizeRule(ctx, window) == "manual" {
		return false
	}
	if _, err := tmuxRun(ctx, "set-option", "-t", window, "-w", "window-size", windowSizeSmallest); err != nil {
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
	return env
}

type tmuxTerm struct {
	pty        *ptyPair
	cmd        *exec.Cmd
	target     string
	session    string
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

func (t *tmuxTerm) Detail() string {
	return fmt.Sprintf("%s (pane %s)", t.session, t.target)
}

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

	if t.winSize != "" && !tmuxSessionHasClients(ctx, tmuxSessionOf(t.window)) {
		if now := tmuxWindowSize(ctx, t.window); now != "" && now != t.winSize {
			if cols, rows, ok := strings.Cut(t.winSize, "x"); ok {
				if _, err := tmuxRun(ctx, "resize-window", "-t", t.window, "-x", cols, "-y", rows); err != nil {
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
		_, err = tmuxRun(ctx, "set-option", "-t", t.window, "-uw", "window-size")
	} else {
		_, err = tmuxRun(ctx, "set-option", "-t", t.window, "-w", "window-size", t.winSizeOpt)
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
