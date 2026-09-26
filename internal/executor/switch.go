package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/action"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

// A switch closes a live session on one side and resumes the same
// conversation on the other: the id, the history and the name stay, the
// process does not. What lives only inside the process — a turn in progress,
// an open question, messages waiting in the queue, background tasks — does
// not survive it, so a switch happens on the boundary of a turn and says what
// it would lose before losing it.

const (
	sessionModelsEnv = "AACP_SESSION_MODELS"
	// How much of the end of a transcript is read for its last permission
	// mode. Every message a person sends carries the mode, so the tail of a
	// conversation has it unless the tail is one huge tool result.
	transcriptTailBytes = 512 << 10
)

// carried is what the session changed on its own since its start and what the
// next process must be started with to be the same session.
type carried struct {
	Model  string
	Effort string
	Mode   string
	// Where each value was read, for the report: a value the panel guessed
	// is not the same as a value it read.
	From []string
}

func (e *Executor) sessionSwitch(ctx context.Context, target string, sw *action.Switch, want *action.Project) (string, error) {
	if sw == nil || want == nil {
		return "", fmt.Errorf("a switch needs where to move the session and its project")
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if s.SessionID == "" {
		return "", fmt.Errorf("session %s has no conversation id the panel can resume", s.Name)
	}
	proc, err := findAgent(target)
	if err != nil {
		return "", err
	}
	p, err := chooseProject(target, want)
	if err != nil {
		return "", err
	}
	if filepath.Clean(s.CWD) != p.Path {
		return "", fmt.Errorf("session %s runs in %s, while its project is %s — the panel would resume it somewhere else",
			s.Name, s.CWD, p.Path)
	}

	var keep carried
	var lost string
	held := onStream(s)
	switch sw.To {
	case action.SwitchConsole:
		if !held {
			return "", fmt.Errorf("session %s is in the console already", s.Name)
		}
		keep, lost, err = leavingStream(ctx, s, sw.Force)
	case action.SwitchStream:
		if held {
			return "", fmt.Errorf("session %s is in the feed already", s.Name)
		}
		if err = shownElsewhere(ctx, s); err != nil {
			return "", err
		}
		keep, err = leavingConsole(s)
	default:
		return "", fmt.Errorf("a session moves to %q or %q, not to %q", action.SwitchConsole, action.SwitchStream, sw.To)
	}
	if err != nil {
		return "", err
	}

	transport := launcher.TransportTmux
	if sw.To == action.SwitchStream {
		transport = launcher.TransportStream
	}
	p.Launch, err = switchedLaunch(p.Launch, transport, keep)
	if err != nil {
		return "", err
	}

	closed, err := e.closeGently(ctx, s, proc, held)
	if err != nil {
		return "", fmt.Errorf("the switch stopped at closing, nothing was started: %w", err)
	}
	rep, err := e.runLauncher(ctx, p, s.SessionID)
	if err != nil {
		return "", fmt.Errorf("%s; the conversation did not come up on the other side: %w — "+
			"it is whole on disk, resume it from the archive", closed, err)
	}

	where := "in the console"
	if sw.To == action.SwitchStream {
		where = "in the feed"
	}
	detail := fmt.Sprintf("session %s moved %s, conversation %s", rep.Session, where, s.SessionID)
	if len(keep.From) > 0 {
		detail += "; kept: " + strings.Join(keep.From, ", ")
	}
	if lost != "" {
		detail += "; " + lost
	}
	for _, w := range rep.Warnings {
		detail += "; WARNING: " + w
	}
	if sw.Window {
		// The session is in the console already: a window that did not open
		// is a warning about the window, not a failed switch.
		opened, err := e.openWindowTo(ctx, p.Path, rep.Session)
		if err != nil {
			opened = "WARNING: the window did not open (" + err.Error() + ") — open it with the button in the header"
		}
		detail += "; " + opened
	}
	return detail, nil
}

// shownElsewhere stops a console from leaving while a terminal outside the
// panel shows it: the switch would end the conversation there, under the eyes
// of whoever reads it. A console outside tmux runs in a terminal of its own; in
// tmux, a window on the host or an ssh attached to it counts, and the terminal
// of the panel does not.
func shownElsewhere(ctx context.Context, s liveSession) error {
	pane, err := tmuxPaneFor(ctx, s.PID)
	if err != nil {
		return fmt.Errorf("session %s does not live in tmux, so it runs in a terminal of its own and a switch "+
			"would end it there — close it in that terminal and resume the conversation in the feed: %w", s.Name, err)
	}
	clients, err := foreignClients(ctx, tmuxSessionOf(pane.Target))
	if err != nil {
		return fmt.Errorf("whether a window shows session %s is unknown: %w", s.Name, err)
	}
	if len(clients) > 0 {
		return fmt.Errorf("a window on the host shows session %s and holds it in the console: close the window first",
			s.Name)
	}
	return nil
}

// closeGently ends a session the way it ends best. A session on the stream is
// asked to end the way a finished `claude -p` does: its input is closed, it
// writes the rest of its transcript and exits cleanly, and its holder leaves
// nothing behind — a signal would read as a launch that failed when the
// session is young. A signal is kept for a console, and for a stream session
// that did not end on its own.
func (e *Executor) closeGently(ctx context.Context, s liveSession, proc agentProc, held bool) (string, error) {
	if held {
		if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpClose}); err == nil &&
			e.waitGone(ctx, proc.Agent, e.softWait()) {
			return fmt.Sprintf("session %s closed gracefully, the transcript is complete", proc.Session), nil
		}
	}
	return e.closeAgent(ctx, proc)
}

// leavingStream says what a session on the stream takes to the console, or
// why it cannot go now. A turn in progress, a request waiting for a person and
// messages in the queue stop the switch: each of them is lost in a way the
// person did not choose. Background tasks stop it too, unless the person was
// shown them and agreed.
func leavingStream(ctx context.Context, s liveSession, force bool) (carried, string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return carried{}, "", err
	}
	switch {
	case st.Busy:
		return carried{}, "", fmt.Errorf("session %s is answering right now: a switch would cut the answer off. "+
			"Switch once it finishes, or stop it first", s.Name)
	case len(st.Pending) > 0:
		tools := make([]string, 0, len(st.Pending))
		for _, p := range st.Pending {
			tools = append(tools, p.Tool)
		}
		return carried{}, "", fmt.Errorf("session %s is waiting for an answer (%s): answer it or put it away first — "+
			"a switch would drop the request", s.Name, strings.Join(tools, ", "))
	case len(st.Queue) > 0:
		return carried{}, "", fmt.Errorf("session %s has %s in its queue: let it take them first — "+
			"a switch would lose them", s.Name, plural(len(st.Queue), "message", "messages"))
	case len(st.Tasks) > 0 && !force:
		return carried{}, "", fmt.Errorf("session %s runs %s that stop with the switch: %s",
			s.Name, plural(len(st.Tasks), "background task", "background tasks"), taskList(st.Tasks))
	}
	// A mode the start named is carried whatever it is now. Without one, claude
	// reports its own name for the default at the handshake, and only a change
	// since then is the session's own.
	var keep carried
	if st.Mode != "" && (startMode(s.PID) != "" || st.Mode != st.StartMode) {
		keep.Mode = st.Mode
		keep.From = append(keep.From, "mode "+st.Mode+" from the stream")
	}
	if st.Picked != "" {
		keep.Model = st.Picked
		keep.From = append(keep.From, "model "+st.Picked+" picked in the feed")
	}
	if st.Effort != "" {
		keep.Effort = st.Effort
		keep.From = append(keep.From, "effort "+st.Effort+" picked in the feed")
	}
	lost := ""
	if len(st.Tasks) > 0 {
		lost = fmt.Sprintf("%s stopped: %s", plural(len(st.Tasks), "background task", "background tasks"), taskList(st.Tasks))
	}
	return keep, lost, nil
}

func taskList(tasks []stream.Task) string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		name := t.Description
		if name == "" {
			name = t.ID
		}
		out = append(out, name)
	}
	return strings.Join(out, "; ")
}

// leavingConsole says what a session in the console takes to the feed, or
// why it cannot go now. A console shows its background work only on its
// screen, and the panel does not open that screen to count it: the person
// was shown it in the feed before pressing.
func leavingConsole(s liveSession) (carried, error) {
	switch s.Status {
	case "busy":
		return carried{}, fmt.Errorf("session %s is answering right now: a switch would cut the answer off. "+
			"Switch once it finishes, or stop it first", s.Name)
	case "waiting":
		return carried{}, fmt.Errorf("session %s is waiting for an answer in a dialog: answer it or put it away first — "+
			"a switch would drop it", s.Name)
	}
	var keep carried
	if model, effort, ok := consoleModel(s.SessionID); ok {
		keep.Model, keep.Effort = model, effort
		if model != "" {
			keep.From = append(keep.From, "model "+model+" from the status line")
		}
		if effort != "" {
			keep.From = append(keep.From, "effort "+effort+" from the status line")
		}
	}
	if mode := transcriptMode(s.SessionID, s.Started); mode != "" {
		keep.Mode = mode
		keep.From = append(keep.From, "mode "+mode+" from the transcript")
	} else if mode := startMode(s.PID); mode != "" {
		keep.Mode = mode
		keep.From = append(keep.From, "mode "+mode+" from its start")
	}
	return keep, nil
}

// startMode is the mode a session was started with, when its start named one.
func startMode(pid int) string {
	args, err := procArgs(pid)
	if err != nil {
		return ""
	}
	for i, a := range args {
		if a == "--permission-mode" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--permission-mode="); ok {
			return v
		}
	}
	return ""
}

// consoleModel reads the model and the effort a console shows right now. The
// transcript learns of a change only with the next request, the status line
// at once — and only the status line names the model the way it was picked,
// with its context window.
func consoleModel(sessionID string) (model, effort string, ok bool) {
	v, ok := consoleSeen(sessionID)
	return v.Model, v.Effort, ok
}

// consoleView is what the status line of a console showed when it was last
// drawn, and when that was.
type consoleView struct {
	At     int64
	Model  string
	Name   string
	Effort string
	Window int
}

func consoleSeen(sessionID string) (consoleView, bool) {
	for _, dir := range sessionModelsDirs() {
		raw, err := os.ReadFile(filepath.Join(dir, sessionID+".json"))
		if err != nil {
			continue
		}
		var seen struct {
			At        int64  `json:"at"`
			SessionID string `json:"sessionId"`
			Model     struct {
				ID   string `json:"id"`
				Name string `json:"displayName"`
			} `json:"model"`
			Effort string `json:"effort"`
			Window int    `json:"contextWindow"`
		}
		if json.Unmarshal(raw, &seen) != nil || (seen.SessionID != "" && seen.SessionID != sessionID) {
			continue
		}
		return consoleView{At: seen.At, Model: seen.Model.ID, Name: seen.Model.Name,
			Effort: seen.Effort, Window: seen.Window}, true
	}
	return consoleView{}, false
}

func sessionModelsDirs() []string {
	if p := os.Getenv(sessionModelsEnv); p != "" {
		return []string{p}
	}
	var out []string
	for _, conf := range registry.ConfigDirs() {
		out = append(out, filepath.Join(conf, "session-models"))
	}
	return out
}

// transcriptMode asks the collector for the permission mode a console was
// last in: every message a person sends carries it, and the transcript is the
// collector's to read, not the executor's. Only a message sent since the
// console started counts — an older one was written by another process of the
// same conversation. A mode changed after the last message is not in the
// transcript at all.
func transcriptMode(sessionID string, since time.Time) string {
	if sessionID == "" {
		return ""
	}
	reply, err := askSeen(seenSocket(), seenReq{Session: sessionID, Ask: seenAskMode, Since: since.UnixMilli()})
	if err != nil || !reply.OK || !reply.Found {
		return ""
	}
	return reply.Mode
}

// switchedLaunch is the project's launch with the other transport and what
// the session carries on top. The opening message is dropped: it opened the
// conversation once, and a resumed conversation would read it as a new
// request.
func switchedLaunch(raw json.RawMessage, transport string, keep carried) (json.RawMessage, error) {
	launch := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &launch); err != nil {
			return nil, fmt.Errorf("the launch parameters of the project were not read: %w", err)
		}
	}
	launch["transport"] = transport
	delete(launch, "intent")
	if keep.Model != "" {
		launch["model"] = keep.Model
	}
	if keep.Effort != "" {
		launch["effort"] = keep.Effort
	}
	if keep.Mode != "" {
		launch["permissionMode"] = keep.Mode
	}
	return json.Marshal(launch)
}
