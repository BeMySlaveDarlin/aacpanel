package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
	"aacpanel/internal/session"
)

// The tools' names on the panel's server: the model calls them
// mcp__aacpanel__observer_start and so on. They are not watch_*: a project
// may keep watchers of its own under that word, and the panel's stand beside
// them under a word of their own.
const (
	StartName = "observer_start"
	ListName  = "observer_list"
	StopName  = "observer_stop"
	TailName  = "observer_tail"
)

// The tools' lines in the server's word to every session that has them.
const (
	StartInstructions = "Use observer_start to watch CI, a deploy or logs beyond this turn and wake on output " +
		"or exit; end the turn instead of polling."
	ListInstructions = "Use observer_list to check existing watches before starting another or choosing one to stop."
	StopInstructions = "Use observer_stop when a watch is finished or unwanted; read needed logs first, as stopping deletes them."
	TailInstructions = "Use observer_tail to inspect a watch's output or diagnose its exit, not to poll for a wake."
)

// startDescription is the tool's own word to the model; busy says when a
// busy session reads a letter.
func startDescription(busy string) string {
	return "Runs a lightweight watch on the host and sends this session a letter on output or exit. Give name, " +
		"command (sh -c, at most 4000 bytes) and wake: line, match or exit; match needs pattern in Go regexp " +
		"syntax. Exit always sends a letter. Optional cwd is absolute or relative to this session's directory; " +
		"only PATH is inherited. gather groups lines for 1-3600 seconds, default 10; text says what to do when " +
		"woken. Use a waiting command such as gh run watch or tail -F: limits of a few hundred megabytes and " +
		"a twentieth of a core exclude builds and heavy jobs; run those separately and watch their result. " +
		"Use alarm_set for a time, notify to call the person. Continue independent work or end the turn; " +
		"do not poll. Letters are from the observer, not the person; " + busy +
		" It survives a panel restart, has no time limit, but ends when this session closes or restarts. " +
		"At most " + fmt.Sprint(PerSession) + " observers per session; inspect with observer_list or observer_tail, " +
		"then observer_stop when done. " + wakeTransports
}

const (
	claudeBusy     = "on the stream, a busy session reads it once its turn is over."
	codexBusy      = "a busy thread reads it at the next step of the turn that runs."
	wakeTransports = "A stream session takes the letter through its holder, a codex thread through its daemon. " +
		"In tmux it types the wake with its origin only into a ready composer; a dialog or unreadable screen " +
		"refuses it, with bounded retries. A stream holder that cannot take letters needs a restart."
)

// The tools' own words to the model, the start's aside.
var (
	listDescription = "Call with no arguments to list this session's observers: names, commands, wake rules, " +
		"wake counts and exit results. Check before adding a watch or selecting one to read or stop. Ended " +
		"commands stay listed with readable logs until observer_stop. Use observer_tail for output, alarm_list " +
		"for timed wakes. This is a snapshot, not a wait: continue work or end the turn for a letter, without polling."
	stopDescription = "Pass name from observer_list to stop this session's observer and delete its log, even " +
		"if the command already ended. Read needed output with observer_tail first. Use when the watch is " +
		"finished or unwanted, not to pause it: restarting needs observer_start. For a timed wake use " +
		"alarm_cancel. After success continue work; no reply from the observer is needed."
	tailDescription = "Pass name from observer_list to read its command's last output lines; optional lines " +
		"is 1-" + fmt.Sprint(TailMax) + ", default " + fmt.Sprint(TailDefault) + ". It also reports the watch's " +
		"state and keeps the log. Use after a wake or to diagnose a failed command, then act on the result. " +
		"Do not poll this tool to wait: the observer sends a letter. Use observer_list for all watches and " +
		"observer_stop to remove one after reading its output."
)

// Tools are the observer and alarm tools of the panel's server for a claude
// session, talking to the executor on the socket at path.
func Tools(path string) []mcp.Tool {
	return tools(path, claudeBusy)
}

// CodexTools are the same tools for a thread of codex.
func CodexTools(path string) []mcp.Tool {
	return tools(path, codexBusy)
}

// tools are the four tools of the observers and the three of the alarms. The
// start is not allowed: it runs a command on the host, and claude asks the
// person first as it asks before a command of the shell. The others touch
// only what this session started — a list, a log, an observer it no longer
// needs — and are allowed; so are the alarms, which run nothing.
func tools(path, busy string) []mcp.Tool {
	return append([]mcp.Tool{
		{
			Name: StartName, Title: "Start an observer", Description: startDescription(busy),
			InputSchema: startSchema(), Instructions: StartInstructions,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return start(ctx, path, bind, args)
			},
		},
		{
			Name: ListName, Title: "List observers", Description: listDescription,
			InputSchema:  map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			Instructions: ListInstructions, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return list(ctx, path, bind)
			},
		},
		{
			Name: StopName, Title: "Stop an observer", Description: stopDescription,
			InputSchema: nameSchema("observer", nil), Instructions: StopInstructions, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return stop(ctx, path, bind, args)
			},
		},
		{
			Name: TailName, Title: "Read an observer's log", Description: tailDescription,
			InputSchema: nameSchema("observer", map[string]any{"lines": map[string]any{"type": "integer", "minimum": 1,
				"maximum": TailMax, "description": "How many of the last lines to read."}}),
			Instructions: TailInstructions, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				return tailOf(ctx, path, bind, args)
			},
		},
	}, alarmTools(path, busy)...)
}

func startSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "pattern": namePattern.String(), "maxLength": NameMax,
				"description": "What the observer is called: its letters are signed with it, and it is stopped by it."},
			"command": map[string]any{"type": "string", "minLength": 1, "maxLength": CommandMax,
				"description": "The command, as sh -c runs it."},
			"cwd": map[string]any{"type": "string",
				"description": "The directory it runs in, absolute or under this session's; this session's left out."},
			"wake": map[string]any{"type": "string", "enum": []string{string(EveryLine), string(Matching), string(AtExit)},
				"description": "What wakes this session: every line, a line the pattern finds, or only the end. " +
					"The end of the command wakes it whatever this is."},
			"pattern": map[string]any{"type": "string", "maxLength": PatternMax,
				"description": "For wake match: the regular expression a line is matched by, in the syntax of Go's regexp."},
			"gather": map[string]any{"type": "integer", "minimum": 1, "maximum": GatherMax,
				"description": fmt.Sprintf("Seconds the lines after the first are gathered into one letter; %d left out.",
					GatherDefault)},
			"text": map[string]any{"type": "string", "maxLength": TextMax,
				"description": "What every letter says first: what to do when woken."},
		},
		"required":             []string{"name", "command", "wake"},
		"additionalProperties": false,
	}
}

func nameSchema(of string, more map[string]any) map[string]any {
	props := map[string]any{"name": map[string]any{"type": "string", "description": "The name of the " + of + "."}}
	for k, v := range more {
		props[k] = v
	}
	return map[string]any{"type": "object", "properties": props, "required": []string{"name"},
		"additionalProperties": false}
}

// callerOf is the session the server serves, as the executor is told of it.
func callerOf(bind mcp.Bind) (Caller, mcp.Binding, error) {
	b, err := bind()
	if err != nil {
		return Caller{}, b, err
	}
	if b.SessionID == "" {
		return Caller{}, b, errors.New("the conversation of this session is not known yet — claude has not " +
			"written the file of this session. Call again in a moment")
	}
	c := Caller{Conversation: b.SessionID, PID: b.PID, Name: b.Name}
	if b.Codex {
		c.Codex, c.Home, c.Dir = true, b.Place.ConfigDir, b.Place.Dir
	}
	return c, b, nil
}

// ask makes a call to the executor, saying what to do when it does not answer.
func ask(ctx context.Context, path string, req Request) (Reply, error) {
	reply, err := Ask(ctx, path, req)
	if err != nil {
		return reply, fmt.Errorf("the executor of the panel does not answer at %s (%v): is aacpanel-exec "+
			"running, and new enough to keep observers and alarms?", path, err)
	}
	if !reply.OK {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

func start(ctx context.Context, path string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Wake    When   `json:"wake"`
		Pattern string `json:"pattern"`
		Gather  int    `json:"gather"`
		Text    string `json:"text"`
	}
	if err := strictly(raw, &args); err != nil {
		return "No observer was started: the arguments are not the tool's (" + err.Error() + ").", true
	}
	c, b, err := callerOf(bind)
	if err != nil {
		return "No observer was started: " + err.Error() + ".", true
	}
	dir := args.Cwd
	if dir == "" || !filepath.IsAbs(dir) {
		dir = filepath.Join(b.Place.Dir, dir)
	}
	spec := Spec{Name: args.Name, Command: args.Command, Dir: dir, Wake: args.Wake, Pattern: args.Pattern,
		Gather: args.Gather, Text: args.Text, Path: os.Getenv("PATH")}
	if _, err := spec.Check(); err != nil {
		return "No observer was started: " + err.Error() + ".", true
	}
	reply, err := ask(ctx, path, Request{Op: OpStart, Caller: c, Spec: &spec})
	if err != nil {
		return "No observer was started: " + err.Error(), true
	}
	v := reply.Observer
	text := "The executor started the observer and said nothing of it: observer_list shows it."
	if v == nil {
		return journalWork(ctx, b, action.ObserverStart, action.WorkRecord{Name: spec.Name, Command: spec.Command, Cwd: spec.Dir}, text), false
	}
	text = fmt.Sprintf("Observer %s started: `%s` in %s, %s; lines are gathered for %d s. It runs in unit %s. "+
		"This session gets a letter from it when it wakes, and observer_tail %s reads what the command printed.",
		v.Name, oneLine(v.Command, 200), v.Dir, wakeWords(*v), v.Gather, v.Unit, v.Name)
	return journalWork(ctx, b, action.ObserverStart, action.WorkRecord{Name: v.Name, Command: v.Command, Cwd: v.Dir}, text), false
}

// The command goes only over the private socket. This second call records a
// fact: losing the panel must not turn a successful start into another start.
func journalWork(ctx context.Context, b mcp.Binding, kind action.Kind, record action.WorkRecord, text string) string {
	record.From = b.SessionID
	name := b.Name
	if name == "" {
		name = b.SessionID
	}
	if err := session.Here().RecordWork(ctx, kind, name, record); err != nil {
		return text + "\nThe work started, not written to the panel's journal: " + err.Error()
	}
	return text + "\nWritten to the panel's journal."
}

func list(ctx context.Context, path string, bind mcp.Bind) (string, bool) {
	c, _, err := callerOf(bind)
	if err != nil {
		return "The observers are not listed: " + err.Error() + ".", true
	}
	reply, err := ask(ctx, path, Request{Op: OpList, Caller: c})
	if err != nil {
		return "The observers are not listed: " + err.Error(), true
	}
	if len(reply.Observers) == 0 {
		return "This session has no observers.", false
	}
	lines := []string{"The observers of this session:"}
	for _, v := range reply.Observers {
		lines = append(lines, "- "+describe(v))
	}
	return strings.Join(lines, "\n"), false
}

func stop(ctx context.Context, path string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Name string `json:"name"`
	}
	if err := strictly(raw, &args); err != nil || args.Name == "" {
		return "Nothing was stopped: the tool takes the name of an observer and nothing else.", true
	}
	c, _, err := callerOf(bind)
	if err != nil {
		return "Nothing was stopped: " + err.Error() + ".", true
	}
	if _, err := ask(ctx, path, Request{Op: OpStop, Caller: c, Name: args.Name}); err != nil {
		return "Nothing was stopped: " + err.Error(), true
	}
	return "Observer " + args.Name + " stopped: its command is stopped and its log is gone.", false
}

func tailOf(ctx context.Context, path string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Name  string `json:"name"`
		Lines int    `json:"lines"`
	}
	if err := strictly(raw, &args); err != nil || args.Name == "" {
		return "Nothing was read: the tool takes the name of an observer and how many lines.", true
	}
	c, _, err := callerOf(bind)
	if err != nil {
		return "Nothing was read: " + err.Error() + ".", true
	}
	reply, err := ask(ctx, path, Request{Op: OpTail, Caller: c, Name: args.Name, Lines: args.Lines})
	if err != nil {
		return "Nothing was read: " + err.Error(), true
	}
	head := "Observer " + args.Name
	if reply.Observer != nil {
		head = describe(*reply.Observer)
	}
	if len(reply.Lines) == 0 {
		return head + "\nIts command has printed nothing yet.", false
	}
	return fmt.Sprintf("%s\nThe last %d lines of its log:\n%s", head, len(reply.Lines),
		strings.Join(reply.Lines, "\n")), false
}

// strictly reads the arguments of a call, refusing one the tool does not take.
func strictly(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

func wakeWords(v View) string {
	switch v.Wake {
	case Matching:
		return "waking this session on a line matching `" + v.Pattern + "` and at its end"
	case AtExit:
		return "waking this session when it ends"
	}
	return "waking this session on every line and at its end"
}

// describe is one observer on one line.
func describe(v View) string {
	s := fmt.Sprintf("%s: `%s` in %s, %s; started %s", v.Name, oneLine(v.Command, 120), v.Dir, wakeWords(v),
		v.Started.Local().Format(time.DateTime))
	switch v.Woken {
	case 0:
		s += ", has not woken it yet"
	case 1:
		s += ", woke it once at " + v.LastWake.Local().Format(time.TimeOnly)
	default:
		s += fmt.Sprintf(", woke it %d times, last at %s", v.Woken, v.LastWake.Local().Format(time.TimeOnly))
	}
	if v.Ended != nil {
		s += "; the command ended: " + v.Ended.String()
	}
	return s + fmt.Sprintf("; its log holds %d bytes", v.Log)
}
