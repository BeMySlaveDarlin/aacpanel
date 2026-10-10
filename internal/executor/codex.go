package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/launcher"
)

// A codex session is a thread of the codex daemon of a contour, held by the
// daemon rather than by anything the panel started. It is reached only through
// the daemon's protocol: a message is a turn, or waits in the panel's queue
// while one runs; a stop is an interrupt — of the agent's own thread for an
// agent the thread started — an approval is a reply to the
// daemon's request, a setting is a change of the thread's settings, a close
// is an interrupt and the panel leaving the thread. None of the ways of a claude
// session — keys into tmux, signals to a pid, the socket of a holder — apply
// to it, and an action the panel has no codex way for is refused rather than
// tried the claude way.

// StartCodex links the executor to the codex daemons of the contours until
// ctx ends.
func (e *Executor) StartCodex(ctx context.Context) {
	e.codex = codex.Start(ctx, registry.CodexHomes(), codexTerminals)
}

// listCodexPanes lists the panes of the user's tmux with the command each was
// started with.
var listCodexPanes = func(ctx context.Context) (string, error) {
	return userTmux.run(ctx, "list-panes", "-a", "-F", codexPanes)
}

// codexTerminals tells which of the threads the launcher started codex in tmux
// on, by the command tmux keeps for each pane — the way a close finds them —
// with the name of the tmux session. No tmux server is no terminal.
func codexTerminals(ctx context.Context, ids []string) (map[string]string, error) {
	out, err := listCodexPanes(ctx)
	if err != nil {
		if noTmuxServer(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	found := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		name, command, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		for _, id := range ids {
			if _, seen := found[id]; !seen && launcher.LaunchedCodex(command, id) {
				found[id] = name
			}
		}
	}
	return found, nil
}

// sessionTarget says whether an action names a live session as its target.
func sessionTarget(k action.Kind) bool {
	switch k {
	case action.TaskStop, action.AgentStop, action.WindowOpen, action.WindowClose, action.SecretPut:
		return true
	}
	return strings.HasPrefix(string(k), "session.")
}

// codexSession is the codex thread a target names, nil when it names none. A
// claude session of the same name wins: it was named by a person or by the
// panel, and a codex thread only by the tail of its id.
func (e *Executor) codexSession(target string) (*codex.Thread, error) {
	found := e.codex.Find(target)
	if len(found) == 0 {
		return nil, nil
	}
	if claude, _, err := liveSessionsByName(target); err == nil && len(claude) > 0 {
		return nil, nil
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("there are two sessions named %s right now — it is unclear which one to take", target)
	}
	return &found[0], nil
}

// CodexModels lists the models codex offers, as the daemon of the contour's
// home lists them, or the first daemon that answers when no contour is named:
// the screens pick a codex model from it before any session runs.
func (e *Executor) CodexModels(ctx context.Context, contour string) ([]action.CodexModel, error) {
	list, err := e.codex.Models(ctx, contour)
	if err != nil {
		return nil, err
	}
	out := make([]action.CodexModel, 0, len(list))
	for _, m := range list {
		out = append(out, action.CodexModel{Model: m.Model, Name: m.Name, Efforts: m.Efforts, Effort: m.Effort})
	}
	return out, nil
}

func codexRefusal(name string) error {
	return fmt.Errorf("%s is a codex session: the panel does not do this for codex yet", name)
}

// notCodex refuses a question about a codex session the panel has no codex
// answer to.
func (e *Executor) notCodex(target string) error {
	th, err := e.codexSession(target)
	if err != nil {
		return err
	}
	if th != nil {
		return codexRefusal(th.Name)
	}
	return nil
}

func (e *Executor) codexAction(ctx context.Context, th codex.Thread, req action.Request) (string, error) {
	switch req.Kind {
	case action.SessionSend:
		if req.Asked != "" {
			return codexAnswerAsked(ctx, th, codex.Message{ID: req.MessageID, Text: req.Text})
		}
		return codexSend(ctx, th, codex.Message{ID: req.MessageID, Text: req.Text})
	case action.SessionLetter:
		return e.codexLetterTo(ctx, th, req.From, req.FromCodex, req.Text)
	case action.SessionFile:
		return codexFile(ctx, th, req.Text, req.Files)
	case action.SessionUnqueue:
		return codexUnqueue(th, req.MessageID)
	case action.SessionSet:
		return codexSet(ctx, th, req.Setting)
	case action.SessionStop:
		return codexStop(ctx, th)
	case action.SessionEscape:
		return codexEscape(ctx, th)
	case action.SessionPermit:
		return codexPermit(ctx, th, req.Permit)
	case action.SessionAnswer:
		return codexAnswer(ctx, th, req.Answer)
	case action.SessionDismiss:
		return codexDismiss(ctx, th, req.Answer)
	case action.SessionCommand:
		return codexCommand(ctx, th, req.Command)
	case action.SessionRename:
		return codexRename(ctx, th, req.Rename)
	case action.TaskStop:
		return codexTaskStop(ctx, th, req.Work)
	case action.AgentStop:
		return codexAgentStop(ctx, th, req.Work)
	case action.SessionClose:
		return e.codexClose(ctx, th)
	case action.SessionRestart:
		return "", fmt.Errorf("%s is a codex session: %s", th.Name, action.CodexNotRestarted)
	}
	return "", codexRefusal(th.Name)
}

// codexSend gives a thread a message: a turn of its own on a free thread, and
// the panel's queue on a busy one, which sends it once the turn that runs
// ends — the way claude's own queue waits, rather than into the turn that
// runs.
func codexSend(ctx context.Context, th codex.Thread, m codex.Message) (string, error) {
	place, err := th.Link.Send(ctx, th.ID, m)
	if err != nil {
		return "", fmt.Errorf("session %s did not take the message: %w", th.Name, err)
	}
	if place == 0 {
		return fmt.Sprintf("sent to %s (free — a turn started with it), %d characters", th.Name,
			len([]rune(m.Text))), nil
	}
	return fmt.Sprintf("%s is busy: the message waits in the panel's queue, %s, and goes as a turn of its own "+
		"once the turn that runs ends", th.Name, ordinal(place)), nil
}

// codexAnswerAsked gives a thread the answer to a question it asked without
// waiting: into the turn that runs, or as a turn of its own on a free thread.
func codexAnswerAsked(ctx context.Context, th codex.Thread, m codex.Message) (string, error) {
	steered, err := th.Link.Answer(ctx, th.ID, m)
	if err != nil {
		return "", fmt.Errorf("session %s did not take the answer: %w", th.Name, err)
	}
	if steered {
		return fmt.Sprintf("the answer went to %s into the turn that runs, %d characters", th.Name,
			len([]rune(m.Text))), nil
	}
	return fmt.Sprintf("the answer went to %s (free — a turn started with it), %d characters", th.Name,
		len([]rune(m.Text))), nil
}

func ordinal(n int) string {
	if n == 1 {
		return "first"
	}
	return fmt.Sprintf("number %d", n)
}

// codexUnqueue takes a message back from the panel's queue of a thread. A
// message that has gone is a turn of the thread, and the answer says so.
func codexUnqueue(th codex.Thread, messageID string) (string, error) {
	if !th.Link.Unqueue(th.ID, messageID) {
		return "", fmt.Errorf("the message is already delivered: %s has read it, and it can no longer be taken back", th.Name)
	}
	return fmt.Sprintf("the message was taken back from the queue of %s before it was read", th.Name), nil
}

// codexFile puts the files where a claude session gets them and gives the
// thread a message with them, the caption first: a picture goes into the turn
// as a file codex reads itself, any other file as its path in the words. A
// busy thread gets the message in the panel's queue, files and all.
func codexFile(ctx context.Context, th codex.Thread, caption string, files []action.File) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no file arrived: there is nothing to send")
	}
	m := codex.Message{}
	var paths, named, bare []string
	for i := range files {
		path, err := storeFile(&files[i])
		if err != nil {
			return "", fmt.Errorf("%w%s", err, landed(paths))
		}
		paths = append(paths, path)
		if err := storePreview(path, files[i].Preview); err != nil {
			bare = append(bare, fmt.Sprintf("%s (%v)", files[i].Name, err))
		}
		if picture(files[i].Data) {
			m.Images = append(m.Images, path)
		} else {
			named = append(named, path)
		}
	}
	m.Text = strings.Join(append(nonEmpty(caption), named...), "\n")
	detail, err := codexSend(ctx, th, m)
	if err != nil {
		return "", fmt.Errorf("%w%s", err, landed(paths))
	}
	detail += " · " + describeFiles(files, paths)
	if len(bare) > 0 {
		detail += "; the feed shows a path in place of " + strings.Join(bare, ", ")
	}
	return detail, nil
}

// pictures are the kinds of picture codex reads from a file into the turn:
// the ones a model takes. A picture of another kind — a HEIC from a phone —
// goes by its path, as any other file.
var pictures = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// picture says a file is a picture codex takes, by what its bytes are rather
// than by its name.
func picture(data []byte) bool {
	return slices.Contains(pictures, http.DetectContentType(data))
}

func nonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

// codexSet changes a setting of a thread for its next turns: a model with its
// effort or without, an effort, a mode or the plan. A default is not the
// panel's to save for codex: its config.toml holds it.
func codexSet(ctx context.Context, th codex.Thread, set *action.Setting) (string, error) {
	switch {
	case set == nil:
		return "", fmt.Errorf("no setting arrived: there is nothing to change")
	case set.Scope == action.ScopeDefault:
		return "", fmt.Errorf("%s is a codex session: its defaults are in the config.toml of its codex home, "+
			"and the panel changes only the session", th.Name)
	case set.Mode != "" && !slices.Contains(codex.Modes, set.Mode):
		return "", fmt.Errorf("%s is a codex session: it has no %s mode; its modes are %s", th.Name, set.Mode,
			strings.Join(codex.Modes, ", "))
	}
	done, err := th.Link.Configure(ctx, th.ID, codex.Setting{Model: set.Model, Effort: set.Effort, Mode: set.Mode,
		Plan: set.Plan})
	if err != nil {
		return "", fmt.Errorf("session %s: %w", th.Name, err)
	}
	switch {
	case done.Later:
		return fmt.Sprintf("the codex daemon does not change a running thread here, so %s goes with the next "+
			"message to %s", modelText(set.Model, done.Effort), th.Name), nil
	case set.Model != "" || set.Effort != "":
		return fmt.Sprintf("%s runs %s from its next turn", th.Name, modelText(set.Model, done.Effort)), nil
	case set.Plan != nil && *set.Plan:
		return fmt.Sprintf("%s plans from its next turn: it explores and asks, and changes nothing", th.Name), nil
	case set.Plan != nil:
		return fmt.Sprintf("%s acts again from its next turn", th.Name), nil
	}
	return fmt.Sprintf("%s runs in the %s mode from its next turn", th.Name, set.Mode), nil
}

func modelText(model, effort string) string {
	switch {
	case model != "" && effort != "":
		return model + " at the " + effort + " effort"
	case model != "":
		return model
	}
	return "the " + effort + " effort"
}

func codexStop(ctx context.Context, th codex.Thread) (string, error) {
	stopped, err := th.Link.Interrupt(ctx, th.ID)
	if err != nil {
		return "", fmt.Errorf("session %s was not stopped: %w", th.Name, err)
	}
	if !stopped {
		return fmt.Sprintf("%s: there was nothing to interrupt", th.Name), nil
	}
	return fmt.Sprintf("%s stopped: the turn was interrupted", th.Name), nil
}

// codexAgentStop stops an agent a thread started, by the id of the agent's
// own thread: the turn it runs is interrupted there. The thread that started
// it is told nothing, and the agent waits for a task more.
func codexAgentStop(ctx context.Context, th codex.Thread, work *action.Work) (string, error) {
	if work == nil {
		return "", fmt.Errorf("it is not said which agent to stop")
	}
	stopped, err := th.Link.InterruptAgent(ctx, th.ID, work.ID)
	if err != nil {
		return "", fmt.Errorf("the agent %s of %s was not stopped: %w", work.ID, th.Name, err)
	}
	if !stopped {
		return fmt.Sprintf("the agent %s of %s was not running a turn any more", work.ID, th.Name), nil
	}
	return fmt.Sprintf("the agent %s of %s is stopped: its turn was interrupted", work.ID, th.Name), nil
}

// codexEscape puts away what the session holds a person to: an approval is
// declined — the turn goes on without what it asked for — or cancelled where
// the request offers no decline; a grant of permissions is refused, a question
// put away to be answered in the conversation, and a request of an MCP server
// cancelled.
func codexEscape(ctx context.Context, th codex.Thread) (string, error) {
	pending := th.Link.Pending(th.ID)
	if len(pending) == 0 {
		return fmt.Sprintf("%s: nothing stands in the way, the conversation is free", th.Name), nil
	}
	put := 0
	for _, r := range pending {
		var answer any
		switch {
		case r.Grants():
			answer = r.Deny()
		case r.Elicits():
			answer = codex.Elicit("cancel", nil)
		case r.Asks():
			answer = r.Dismiss()
		default:
			decision := json.RawMessage(`"cancel"`)
			for _, d := range r.Decisions() {
				if string(d) == `"decline"` {
					decision = d
				}
			}
			answer = map[string]any{"decision": decision}
		}
		err := th.Link.Reply(ctx, th.ID, r.Key(), answer)
		switch {
		case errors.Is(err, codex.ErrAnswered):
			continue
		case err != nil:
			return "", fmt.Errorf("session %s: %w", th.Name, err)
		}
		put++
	}
	return fmt.Sprintf("%s: %s put away", th.Name, plural(put, "request", "requests")), nil
}

// codexPermission reads the oldest request a codex session waits on that is
// a choice among a few — an approval, a grant of permissions, a yes or a no of
// an MCP server — as the permission the screen already knows how to draw. A
// question or a form is not one: the feed draws it from the state of the
// session. The options of an approval are the decisions the daemon offers, in
// its order; the fingerprint is the id of the request, so a press meant for
// one request never lands on the next.
func codexPermission(ctx context.Context, th codex.Thread) (*action.Permission, error) {
	r, ok := firstChoice(th.Link.Pending(th.ID))
	if !ok {
		return nil, nil
	}
	d := &action.Permission{
		Tool:        r.Tool(),
		Action:      codexLines(ctx, th, r),
		Options:     codexOptions(r),
		Fingerprint: r.Key(),
		Note:        []string{},
		Raw:         []string{},
	}
	if r.Elicits() {
		d.Note = append(d.Note, elicitNotes(r)...)
		d.URL = r.URL
	}
	if r.Network != nil {
		d.Note = append(d.Note, fmt.Sprintf("network access to %s over %s", r.Network.Host, r.Network.Protocol))
	}
	if r.GrantRoot != "" {
		d.Note = append(d.Note, "and writing under "+r.GrantRoot+" for the rest of the session")
	}
	if r.Reason != "" {
		d.Note = append(d.Note, "asked because: "+r.Reason)
	}
	return d, nil
}

// codexLines is what the request is about to do: the command, or the change
// to files. A change is not in the request — the item it names holds it, and
// the daemon is asked for it.
func codexLines(ctx context.Context, th codex.Thread, r codex.Request) []string {
	var lines []string
	switch {
	case r.Grants():
		lines = r.Asked()
	case r.ToolCall() != nil:
		lines = r.ToolCall().Lines()
	case r.Elicits():
		lines = elicitLines(r)
	case !r.FileChange():
		lines = strings.Split(r.Command, "\n")
	default:
		changes, err := th.Link.Changes(ctx, th.ID, r.TurnID, r.ItemID)
		if err != nil {
			return []string{"the change itself was not read: " + err.Error()}
		}
		for _, c := range changes {
			head := c.Path
			switch {
			case c.Kind.Type == "add":
				head = "new file " + c.Path
			case c.Kind.Type == "delete":
				head = "deleted " + c.Path
			case c.Kind.MovePath != "":
				head = c.Path + " → " + c.Kind.MovePath
			}
			lines = append(lines, head)
			lines = append(lines, strings.Split(strings.TrimRight(c.Diff, "\n"), "\n")...)
		}
	}
	if len(lines) > permLines {
		lines = append(lines[:permLines], fmt.Sprintf("… %d more lines", len(lines)-permLines))
	}
	return lines
}

func codexOptions(r codex.Request) []action.PermOption {
	var out []action.PermOption
	for i, c := range codexChoices(r) {
		out = append(out, action.PermOption{N: i + 1, Text: c.text, Lasting: c.lasting})
	}
	return out
}

// decisionText words a decision the way codex's own TUI offers it.
func decisionText(raw json.RawMessage) (string, bool) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		switch name {
		case "accept":
			return "Yes", false
		case "acceptForSession":
			return "Yes, and don't ask again this session", true
		case "decline":
			return "No, go on without it", false
		case "cancel":
			return "No, and stop: tell Codex what to do differently", false
		}
		return name, false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return string(raw), false
	}
	if body, ok := obj["acceptWithExecpolicyAmendment"]; ok {
		var v struct {
			Prefix []string `json:"execpolicy_amendment"`
		}
		_ = json.Unmarshal(body, &v)
		return fmt.Sprintf("Yes, and don't ask again for commands that start with `%s`", strings.Join(v.Prefix, " ")), true
	}
	if body, ok := obj["applyNetworkPolicyAmendment"]; ok {
		var v struct {
			Amendment struct {
				Host   string `json:"host"`
				Action string `json:"action"`
			} `json:"network_policy_amendment"`
		}
		_ = json.Unmarshal(body, &v)
		return fmt.Sprintf("Yes, and %s %s", v.Amendment.Action, v.Amendment.Host), true
	}
	for key := range obj {
		return key, false
	}
	return string(raw), false
}

// codexPermit answers the request the person looked at with the reply of the
// item they pressed: a decision of an approval as the daemon offered it, a
// grant of permissions, or the answer to an MCP server.
func codexPermit(ctx context.Context, th codex.Thread, pick *action.Permit) (string, error) {
	if pick == nil {
		return "", fmt.Errorf("it is not said which item to press")
	}
	pending := th.Link.Pending(th.ID)
	if _, ok := firstChoice(pending); !ok {
		return "", fmt.Errorf("session %s is asking nothing — the request is already answered", th.Name)
	}
	var r *codex.Request
	for i := range pending {
		if pending[i].Key() == pick.Fingerprint && !pending[i].Asks() {
			r = &pending[i]
		}
	}
	if r == nil {
		return "", fmt.Errorf("the request of session %s changed while you were looking — look again", th.Name)
	}
	choices := codexChoices(*r)
	if pick.Option < 1 || pick.Option > len(choices) {
		return "", fmt.Errorf("there is no item %d in the request of session %s", pick.Option, th.Name)
	}
	chosen := choices[pick.Option-1]
	err := th.Link.Reply(ctx, th.ID, r.Key(), chosen.result)
	switch {
	case errors.Is(err, codex.ErrAnswered):
		return "", fmt.Errorf("session %s is asking nothing — the request is already answered", th.Name)
	case err != nil:
		return "", fmt.Errorf("session %s: %w", th.Name, err)
	}
	return fmt.Sprintf("%s answered in session %s: %s", r.Tool(), th.Name, chosen.text), nil
}
