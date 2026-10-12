package executor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

// panelTermSocket is the socket of the tmux server the terminals of the panel
// live on. The collector knows a claude typed into one of them by it, so the
// name is written once more in agent/ctx.py, and a test holds the two together.
const panelTermSocket = "aacpanel-term"

// panelTmux is the tmux server the terminals of the panel live on: a socket of
// their own, apart from the sessions the panel starts. Only terminals with
// the panel's id and place get its rename and desktop window controls.
var panelTmux tmuxServer = panelTermSocket

// termNameOption keeps the name a person gave a terminal; without one the
// terminal goes by its window, which names itself after what runs in it.
const termNameOption = "@aacp_name"

// panelTermFormat lists a terminal as fields between tabs. The command goes
// last: tmux cleans the names it keeps of tabs, and passes the name of a
// process on as the process gave it.
var panelTermFormat = strings.Join([]string{
	"#{session_name}", "#{session_created}", "#{session_activity}", "#{session_attached}",
	"#{" + launcher.TermPlaceOption + "}", "#{" + termNameOption + "}", "#{window_name}",
	"#{pane_current_command}",
}, "\t")

const panelTermFields = 8

// panelShells are the commands a terminal shows while nothing runs in it: tmux
// names the command of a pane after the process in its foreground, and a shell
// waiting at its prompt is the shell itself.
var panelShells = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true, "ksh": true, "mksh": true,
	"tcsh": true, "csh": true, "nu": true, "elvish": true, "xonsh": true,
}

// termLastRunes is the ceiling on the last line of a screen a terminal is
// listed with: the card shows one line of it, and a screen as wide as a monitor
// need not ride along in every list.
const termLastRunes = 160

// termCaptureTimeout bounds the reading of one screen, well under a tmux call
// of its own: the line is a glance, and a screen slow to come leaves its
// terminal without one rather than holding up the list.
var termCaptureTimeout = time.Second

// Terms lists every live server the user owns. Only the panel's own terminals
// get a screen preview: a host with many unrelated sessions costs one list per
// server, not a capture per terminal.
func (e *Executor) Terms(ctx context.Context) ([]action.Term, error) {
	entries, err := tmuxTerms(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]action.Term, 0, len(entries))
	for _, entry := range entries {
		t := entry.Term
		if !t.Foreign {
			t.Last = termLast(ctx, t.ID)
		}
		list = append(list, t)
	}
	return list, nil
}

// termBusy reports that something other than the shell runs in a terminal.
func termBusy(command string) bool {
	return !panelShells[command]
}

// termLast reads the screen of a terminal and gives its last line. A screen
// that could not be read is no line: the terminal is listed all the same.
func termLast(ctx context.Context, id string) string {
	callCtx, cancel := context.WithTimeout(ctx, termCaptureTimeout)
	defer cancel()
	screen, err := panelTmux.run(callCtx, "capture-pane", "-p", "-t", "="+id+":")
	if err != nil {
		return ""
	}
	return lastLine(screen)
}

// lastLine is the last line of a screen that is not blank, trimmed and cut to
// termLastRunes. A screen fills from the top, so the rows under the cursor are
// empty, and the last one with text is where the terminal stands.
func lastLine(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return cutRunes(line, termLastRunes)
		}
	}
	return ""
}

// panelTerms lists the terminals on the panel's tmux server, and whether
// something other than the shell runs in each. A session there without an id
// of the panel or without its place was not started by the panel, and is left
// out: nothing the panel does can name it.
func panelTerms(ctx context.Context) ([]action.Term, error) {
	out, err := panelTmux.run(ctx, "list-sessions", "-F", panelTermFormat)
	if err != nil {
		if noTmuxServer(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux did not list the terminals of the panel: %w", err)
	}
	var list []action.Term
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", panelTermFields)
		if len(f) != panelTermFields || !action.TermID(f[0]) || f[4] == "" {
			continue
		}
		created, _ := strconv.ParseInt(f[1], 10, 64)
		activity, _ := strconv.ParseInt(f[2], 10, 64)
		clients, _ := strconv.Atoi(f[3])
		name := f[5]
		if name == "" {
			name = f[6]
		}
		list = append(list, action.Term{
			ID: f[0], Place: f[4], Name: name, Command: f[7], Busy: termBusy(f[7]),
			Activity: activity, Created: created, Clients: clients,
		})
	}
	return list, nil
}

// noTmuxServer tells a server that is not running, was never started, or went
// while it was asked from a failure: either way there is nothing on it.
//
// A server goes by itself once its last session is closed and its last client
// has left, and a client that dialled it in that last moment sits in the queue
// of a socket nobody accepts from any more: the server's exit drops it, and
// tmux answers "server exited unexpectedly". The terminals listed right after
// the last one was closed meet exactly that, in a few lists out of a hundred on
// a busy host.
func noTmuxServer(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no server running") || strings.Contains(msg, "error connecting to") ||
		strings.Contains(msg, "server exited unexpectedly")
}

// findPanelTerm finds a terminal of the panel by its id: the id has the form
// of one, and the terminal is on the panel's server.
func findPanelTerm(ctx context.Context, id string) (action.Term, error) {
	if !action.TermID(id) {
		return action.Term{}, fmt.Errorf("%q is not the id of a terminal of the panel", id)
	}
	list, err := panelTerms(ctx)
	if err != nil {
		return action.Term{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return action.Term{}, fmt.Errorf("there is no terminal %s among those of the panel", id)
}

// termPlace checks the directory a terminal of the panel opens in: an existing
// directory whose real path is inside the home directory or a project root.
func termPlace(place string) (string, error) {
	if place == "" || !strings.HasPrefix(place, "/") {
		return "", fmt.Errorf("the place %q of a terminal is not absolute", place)
	}
	roots := projectRoots()
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(home, "/") && filepath.Clean(home) != "/" {
		roots = append(roots, filepath.Clean(home))
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("neither the home directory nor a project root is set: a terminal has nowhere to open")
	}
	return dirInside(place, roots)
}

func (e *Executor) termStart(ctx context.Context, id, place string) (string, error) {
	if !action.TermID(id) {
		return "", fmt.Errorf("%q is not the id of a terminal of the panel", id)
	}
	dir, err := termPlace(place)
	if err != nil {
		return "", err
	}
	rep, err := e.runTermStarter(ctx, dir, id)
	if err != nil {
		return "", err
	}
	detail := "terminal " + id + " started in " + dir
	for _, w := range rep.Warnings {
		detail += "; WARNING: " + w
	}
	return detail, nil
}

func (e *Executor) termClose(ctx context.Context, id string) (string, error) {
	t, err := findPanelTerm(ctx, id)
	if err != nil {
		return "", err
	}
	if err := refuseClaudeTerm(ctx, panelTmux, t.ID); err != nil {
		return "", err
	}
	if _, err := panelTmux.run(ctx, "kill-session", "-t", "="+t.ID); err != nil {
		return "", fmt.Errorf("terminal %s did not close: %w", t.ID, err)
	}
	return "terminal " + t.ID + " is closed", nil
}

func (e *Executor) termRename(ctx context.Context, id, name string) (string, error) {
	t, err := findPanelTerm(ctx, id)
	if err != nil {
		return "", err
	}
	target := "=" + t.ID + ":"
	if _, err := panelTmux.run(ctx,
		"set-option", "-t", target, termNameOption, name,
		";", "set-option", "-w", "-t", target, "automatic-rename", "off"); err != nil {
		return "", fmt.Errorf("terminal %s kept its name: %w", t.ID, err)
	}
	return fmt.Sprintf("terminal %s is called %q now", t.ID, name), nil
}

func (e *Executor) termConsole(ctx context.Context, id string) (string, error) {
	t, err := findPanelTerm(ctx, id)
	if err != nil {
		return "", err
	}
	return e.openWindowTo(ctx, panelTmux, t.Place, t.ID)
}

// A tmux id binds a session to this incarnation of its server. Names and even
// tmux session ids can be reused after a server exits; the socket inode, server pid and
// session creation time keep a stale card from closing the replacement.
type listedTerm struct {
	action.Term
	server tmuxServer
	target string
	panes  map[int]string
}

var termListTimeout = time.Second

// The pane pids cover every window, including inactive ones. Matching the live
// session processes through their parents costs no further tmux calls.
var allTermFormat = strings.Join([]string{
	"#{session_name}", "#{session_created}", "#{session_activity}", "#{session_attached}",
	"#{" + launcher.TermPlaceOption + "}", "#{" + termNameOption + "}", "#{window_name}",
	"#{session_id}", "#{pane_current_path}", "#{W:#{P:#{pane_pid} }}", "#{pid}", "#{pane_current_command}",
}, "\t")

func tmuxTerms(ctx context.Context) ([]listedTerm, error) {
	base := os.Getenv("TMUX_TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	dir := filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()))
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || !ownedTmuxEntry(info) {
		return nil, fmt.Errorf("the user's tmux directory is not an owned directory")
	}
	sockets, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var list []listedTerm
	for _, socket := range sockets {
		info, err := socket.Info()
		if err != nil || info.Mode()&os.ModeSocket == 0 || !ownedTmuxEntry(info) {
			continue
		}
		server := tmuxServer(socket.Name())
		callCtx, cancel := context.WithTimeout(ctx, termListTimeout)
		out, err := server.run(callCtx, "list-sessions", "-F", allTermFormat)
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			f := strings.SplitN(line, "\t", 12)
			if len(f) != 12 {
				continue
			}
			created, _ := strconv.ParseInt(f[1], 10, 64)
			activity, _ := strconv.ParseInt(f[2], 10, 64)
			clients, _ := strconv.Atoi(f[3])
			st := info.Sys().(*syscall.Stat_t)
			digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d:%d:%s\x00%s\x00%s", server, st.Dev, st.Ino, f[10], f[7], f[1])))
			term := action.Term{ID: fmt.Sprintf("tm-%x", digest), Foreign: true, Server: socket.Name(),
				Name: f[0], Place: f[8], Command: f[11], Busy: termBusy(f[11]),
				Created: created, Activity: activity, Clients: clients}
			if server == panelTmux && action.TermID(f[0]) && f[4] != "" {
				term.ID, term.Foreign, term.Server, term.Place, term.Name = f[0], false, "", f[4], f[5]
				if term.Name == "" {
					term.Name = f[6]
				}
			}
			panes := map[int]string{}
			for _, field := range strings.Fields(f[9]) {
				if pid, err := strconv.Atoi(field); err == nil {
					panes[pid] = f[7]
				}
			}
			list = append(list, listedTerm{Term: term, server: server, target: f[7], panes: panes})
		}
	}
	sessions, err := terminalSessions()
	if err != nil {
		return nil, err
	}
	panes := map[int]string{}
	for i := range list {
		for pid := range list[i].panes {
			panes[pid] = strconv.Itoa(i)
		}
	}
	for _, session := range sessions {
		if target, found := paneUp(panes, session.PID); found {
			i, _ := strconv.Atoi(target)
			if list[i].Session == "" {
				list[i].Session, list[i].SessionID = session.Name, session.SessionID
			}
		}
	}
	return list, nil
}

func ownedTmuxEntry(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid()) && info.Mode()&os.ModeSymlink == 0
}

// An absent sessions directory means no sessions; an unreadable one must not
// turn a Claude session into a terminal that can be killed without its close.
func terminalSessions() ([]liveSession, error) {
	var list []liveSession
	for _, dir := range sessionsDirs() {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, session := range sessionsFromDir(dir, entries) {
			if !onStream(session) {
				list = append(list, session)
			}
		}
	}
	return list, nil
}

func findTmuxTerm(ctx context.Context, id string) (listedTerm, error) {
	if !action.TmuxID(id) {
		return listedTerm{}, fmt.Errorf("invalid tmux terminal id")
	}
	list, err := tmuxTerms(ctx)
	if err != nil {
		return listedTerm{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return listedTerm{}, fmt.Errorf("the tmux terminal is no longer listed")
}

func (e *Executor) tmuxClose(ctx context.Context, id string) (string, error) {
	t, err := findTmuxTerm(ctx, id)
	if err != nil {
		return "", err
	}
	if t.Session != "" {
		return "", fmt.Errorf("Claude session %s must be closed through session.close", t.Session)
	}
	if _, err := t.server.run(ctx, "kill-session", "-t", t.target); err != nil {
		return "", err
	}
	return fmt.Sprintf("terminal %s on %s is closed", t.Name, t.Server), nil
}

func refuseClaudeTerm(ctx context.Context, server tmuxServer, name string) error {
	sessions, err := terminalSessions()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return nil
	}
	panes, err := server.panes(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if target, found := paneUp(panes, session.PID); found && tmuxSessionOf(target) == name {
			return fmt.Errorf("Claude session %s must be closed through session.close", session.Name)
		}
	}
	return nil
}
