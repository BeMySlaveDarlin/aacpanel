package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// Models answers what a session can be switched to. A session on the stream
// is asked nothing new: its holder kept the list claude gave at the handshake,
// with the efforts each model takes. A terminal session has no such list to
// give, and the answer says where it lives so the panel knows to fall back.
func (e *Executor) Models(ctx context.Context, target string) (*action.Models, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return &action.Models{Transport: action.SwitchConsole}, nil
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return nil, err
	}
	return &action.Models{
		Transport: action.SwitchStream,
		List:      initModels(st.Init),
		Mode:      st.Mode,
		Effort:    st.Effort,
		Picked:    st.Picked,
	}, nil
}

// initModels reads the models out of claude's answer to the handshake.
func initModels(init json.RawMessage) []action.Model {
	var body struct {
		Models []struct {
			Value       string   `json:"value"`
			Resolved    string   `json:"resolvedModel"`
			Name        string   `json:"displayName"`
			Description string   `json:"description"`
			Efforts     []string `json:"supportedEffortLevels"`
		} `json:"models"`
	}
	if len(init) == 0 || json.Unmarshal(init, &body) != nil {
		return nil
	}
	out := make([]action.Model, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Value == "" {
			continue
		}
		out = append(out, action.Model{Value: m.Value, Resolved: m.Resolved, Name: m.Name,
			Description: m.Description, Efforts: m.Efforts})
	}
	return out
}

// sessionSet changes the one setting a pick names. A model and an effort go
// the way a person would type them — the same slash command, into a terminal
// or onto the stream — with what their scope adds; the mode, which has no
// command, goes its own.
func (e *Executor) sessionSet(ctx context.Context, target string, set *action.Setting) (string, error) {
	switch {
	case set == nil:
		return "", fmt.Errorf("no setting arrived: there is nothing to change")
	case set.Model != "":
		return e.setModel(ctx, target, set.Model, set.Scope)
	case set.Effort != "":
		return e.setEffort(ctx, target, set.Effort, set.Scope)
	}
	return e.sessionMode(ctx, target, set.Mode)
}

// sessionMode puts a live session into a permission mode. On the stream that
// is claude's own request; a terminal has no request for it, only a key that
// cycles through the modes on its screen.
func (e *Executor) sessionMode(ctx context.Context, target, mode string) (string, error) {
	if !slices.Contains(action.Modes, mode) {
		return "", fmt.Errorf("there is no permission mode %q", mode)
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if !onStream(s) {
		return "", fmt.Errorf("session %s runs in a terminal: its mode is switched on its own screen, "+
			"with shift+tab, and the panel does not press it yet", s.Name)
	}
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "set_permission_mode",
		Fields: map[string]any{"mode": mode}}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s runs in the %s mode now", s.Name, mode), nil
}

// switchHookAsk is what claude's confirmation says when a hook, not the price
// of the cache, is why it asks.
const switchHookAsk = "A PreModelSwitch hook asked you to confirm"

// setting follows a model or an effort typed into a console. Claude asks
// before it changes either one of a conversation it holds cached for the
// current one: the next answer reads the whole history again. The person
// already chose in the panel's picker, so that question is answered for them
// and the price is said in the reply. A hook that asks for the confirmation
// asks the person, and the panel does not answer in their place.
//
// The words of the command are no proof it went in: claude runs it at once
// even while it answers, and then it is neither in the composer nor in the
// conversation. The status line is — it is drawn again with the change.
type setting struct {
	sid     string
	effort  bool
	value   string
	title   string
	before  consoleView
	known   bool
	sent    int64
	pressed time.Time
	notes   []string
	seen    *consoleView
}

func newSetting(s liveSession, cmd *action.Command) *setting {
	set := &setting{sid: s.SessionID, value: cmd.Arg, sent: time.Now().Unix()}
	switch cmd.Name {
	case "model":
		set.title = "Switch model?"
	case "effort":
		set.effort, set.title = true, "Change effort level?"
	default:
		return nil
	}
	if cmd.Arg == "" {
		return nil
	}
	set.before, set.known = consoleSeen(s.SessionID)
	return set
}

func (set *setting) watch() *sendWatch {
	if set == nil {
		return nil
	}
	return &sendWatch{took: set.took, dialog: set.dialog}
}

// took reads the status line: an effort is taken when it shows the one asked
// for, a model when the model or its window differ from what it showed before
// the command. A model is not matched by name — the picker names it the way
// the command takes it, the status line by its id. Ultracode is not a level
// the status line names: it shows xhigh, and only a line drawn since the
// command tells the command did it.
func (set *setting) took() bool {
	v, ok := consoleSeen(set.sid)
	if !ok {
		return false
	}
	if set.effort && set.value == action.Ultracode {
		if v.Effort != "xhigh" || v.At < set.sent {
			return false
		}
	} else if set.effort {
		if v.Effort != set.value {
			return false
		}
	} else if !set.known || v.At < set.sent || (v.Model == set.before.Model && v.Window == set.before.Window) {
		return false
	}
	set.seen = &v
	return true
}

func (set *setting) dialog(ctx context.Context, t term, screen string) (bool, error) {
	hook, ok := switchDialogOf(screen, set.title)
	if !ok {
		return false, nil
	}
	if !set.pressed.IsZero() {
		if time.Since(set.pressed) > freeWait {
			return false, fmt.Errorf("the session still shows its confirmation after it was answered: %s", screenTail(screen))
		}
		return true, nil
	}
	what := "model"
	if set.effort {
		what = "effort level"
	}
	if hook {
		if err := t.send(ctx, "2"); err != nil {
			return false, err
		}
		return false, fmt.Errorf("the %s was not changed: a hook asks the person to confirm it, and the panel does not "+
			"confirm in their place; the session went back to its composer — change it there", what)
	}
	if err := t.send(ctx, "1"); err != nil {
		return false, err
	}
	set.pressed = time.Now()
	set.notes = append(set.notes, "claude held the conversation cached for the previous "+what+
		": the next answer reads the whole history again")
	return true, nil
}

// said is what the reply adds about the change: the price that was agreed to,
// and what the status line shows now.
func (set *setting) said() string {
	if set == nil {
		return ""
	}
	out := ""
	for _, n := range set.notes {
		out += "; " + n
	}
	if set.seen != nil {
		switch {
		case set.effort && set.value == action.Ultracode:
			out += "; the console runs on ultracode now, for this session only (its status line says xhigh)"
		case set.effort:
			out += "; the console shows effort " + set.seen.Effort + " now"
		case set.seen.Name != "":
			out += "; the console shows " + set.seen.Name + " now"
		default:
			out += "; the console shows " + set.seen.Model + " now"
		}
	}
	return out
}

// switchDialogOf reads claude's confirmation of a change of the model or the
// effort: its heading, the row that agrees and the row that goes back. It
// says whether a hook, rather than the cache, is what asks.
func switchDialogOf(screen, title string) (hook, ok bool) {
	var titled, yes, no bool
	for _, line := range strings.Split(screen, "\n") {
		body := strings.Trim(line, " \t│┃|")
		if mark := cursorAt(body); mark != "" {
			body = strings.TrimSpace(body[len(mark):])
		}
		switch {
		case body == title:
			titled = true
		case strings.HasPrefix(body, "1. Yes, switch to "):
			yes = true
		case body == "2. No, go back":
			no = true
		}
	}
	return strings.Contains(squeeze(screen), squeeze(switchHookAsk)), titled && yes && no
}
