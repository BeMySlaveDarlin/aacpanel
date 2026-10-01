package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

// panelTermSocket is the socket of the tmux server the terminals of the panel
// live on. The collector knows a claude typed into one of them by it, so the
// name is written once more in agent/ctx.py, and a test holds the two together.
const panelTermSocket = "aacpanel-term"

// panelTmux is the tmux server the terminals of the panel live on: a socket of
// their own, apart from the sessions the panel starts, so that what is listed
// there is what the panel started and nothing else.
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

// Terms lists the terminals of the panel, each with the last line on its
// screen.
func (e *Executor) Terms(ctx context.Context) ([]action.Term, error) {
	list, err := panelTerms(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Last = termLast(ctx, list[i].ID)
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

// noTmuxServer tells a server that is not running, or was never started, from
// a failure: either way there are no terminals.
func noTmuxServer(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no server running") || strings.Contains(msg, "error connecting to")
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
