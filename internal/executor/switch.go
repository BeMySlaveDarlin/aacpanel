package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

	closed, err := e.closeAgent(ctx, proc)
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
	return detail, nil
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
	var keep carried
	if st.Mode != "" {
		keep.Mode = st.Mode
		keep.From = append(keep.From, "mode "+st.Mode+" from the stream")
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
	if mode := transcriptMode(s.SessionID); mode != "" {
		keep.Mode = mode
		keep.From = append(keep.From, "mode "+mode+" from the transcript")
	}
	return keep, nil
}

// consoleModel reads the model and the effort a console shows right now. The
// transcript learns of a change only with the next request, the status line
// at once — and only the status line names the model the way it was picked,
// with its context window.
func consoleModel(sessionID string) (model, effort string, ok bool) {
	for _, dir := range sessionModelsDirs() {
		raw, err := os.ReadFile(filepath.Join(dir, sessionID+".json"))
		if err != nil {
			continue
		}
		var seen struct {
			SessionID string `json:"sessionId"`
			Model     struct {
				ID string `json:"id"`
			} `json:"model"`
			Effort string `json:"effort"`
		}
		if json.Unmarshal(raw, &seen) != nil || (seen.SessionID != "" && seen.SessionID != sessionID) {
			continue
		}
		return seen.Model.ID, seen.Effort, true
	}
	return "", "", false
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

// transcriptMode reads the permission mode a console was last in off the end
// of its transcript: every message a person sends carries it.
func transcriptMode(sessionID string) string {
	for _, conf := range registry.ConfigDirs() {
		found, _ := filepath.Glob(filepath.Join(conf, "projects", "*", sessionID+".jsonl"))
		for _, path := range found {
			if mode := lastMode(path); mode != "" {
				return mode
			}
		}
	}
	return ""
}

func lastMode(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > transcriptTailBytes {
		_, _ = f.Seek(info.Size()-transcriptTailBytes, io.SeekStart)
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"permissionMode"`)) {
			continue
		}
		var rec struct {
			Mode string `json:"permissionMode"`
		}
		if json.Unmarshal(lines[i], &rec) == nil && rec.Mode != "" {
			return rec.Mode
		}
	}
	return ""
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
