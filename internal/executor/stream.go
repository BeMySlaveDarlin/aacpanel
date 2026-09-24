package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// A session on the stream is answered with structure: a message is a line
// the holder writes, a question and a permission are requests with an id, and
// nothing is typed or read off a screen. The actions over it are the same
// actions over a terminal; what differs is only how they reach claude.

const (
	// What the model reads when a person refused a tool from the panel. It is
	// the whole of what the model learns about why.
	refusedByPanel = "The person refused this from the panel."
	// What the model reads when a person put a question away to answer it in
	// the conversation instead.
	dismissedByPanel = "The person put the question away and will answer in the conversation."
	// How many lines of a tool's input a permission shows. A command or an
	// edit longer than that is shown cut, and says so.
	permLines = 40
)

// onStream says whether a live session is held on the stream.
func onStream(s liveSession) bool {
	_, held := stream.Held(s.SessionID, s.PID)
	return held
}

func streamAsk(ctx context.Context, s liveSession, req stream.Request) (stream.Reply, error) {
	reply, err := stream.Ask(ctx, s.SessionID, req)
	if err != nil {
		return reply, fmt.Errorf("session %s is not answering on the stream: %w", s.Name, err)
	}
	if !reply.OK {
		return reply, fmt.Errorf("session %s: %s", s.Name, reply.Error)
	}
	return reply, nil
}

func streamState(ctx context.Context, s liveSession) (stream.State, error) {
	reply, err := streamAsk(ctx, s, stream.Request{Op: stream.OpState})
	if err != nil {
		return stream.State{}, err
	}
	if reply.State == nil {
		return stream.State{}, fmt.Errorf("session %s answered without its state", s.Name)
	}
	return *reply.State, nil
}

func streamSend(ctx context.Context, s liveSession, text string) (string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpSend, Text: text}); err != nil {
		return "", err
	}
	where := "free — it will read it right away"
	if st.Busy {
		where = "busy — the message waits in its queue"
	}
	return fmt.Sprintf("sent to %s on the stream (%s), %d characters", s.Name, where, len([]rune(text))), nil
}

// streamCommand sends a slash command the way claude -p takes one: as a
// message. Clearing is the exception: on the stream it starts a conversation
// under a new id that the holder does not keep, and the session would drop
// off the panel.
func streamCommand(ctx context.Context, s liveSession, cmd *action.Command) (string, error) {
	if cmd.Name == "clear" {
		return "", fmt.Errorf("/clear is not sent to %s: on the stream it starts a conversation under a new id, "+
			"and the session would drop off the panel. Close the session and open a new one instead", s.Name)
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	line := commandLine(cmd)
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpSend, Text: line}); err != nil {
		return "", err
	}
	if st.Busy {
		return fmt.Sprintf("%s sent to %s on the stream: it is busy, the command waits in its queue", line, s.Name), nil
	}
	return fmt.Sprintf("%s sent to %s on the stream", line, s.Name), nil
}

func streamInterrupt(ctx context.Context, s liveSession) (string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	if !st.Busy {
		return fmt.Sprintf("%s: there was nothing to interrupt", s.Name), nil
	}
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "interrupt"}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s stopped: the answer was interrupted", s.Name), nil
}

// streamEscape puts away what the session holds a person to: on the stream
// that is a request waiting for an answer, not a screen. With none, there is
// nothing to give back — the stream has no composer to lose.
func streamEscape(ctx context.Context, s liveSession) (string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	if len(st.Pending) == 0 {
		return fmt.Sprintf("%s: nothing stands in the way, the conversation is free", s.Name), nil
	}
	for _, p := range st.Pending {
		if err := refuse(ctx, s, p.RequestID, refusedByPanel); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%s: %s put away", s.Name, plural(len(st.Pending), "request", "requests")), nil
}

func refuse(ctx context.Context, s liveSession, requestID, why string) error {
	body, _ := json.Marshal(map[string]any{"behavior": "deny", "message": why})
	_, err := streamAsk(ctx, s, stream.Request{Op: stream.OpRespond, RequestID: requestID, Response: body})
	return err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// ------------------------------------------------------------ permissions

// firstPermission is the request a person is asked to allow or refuse. A
// question is not one: it has a card of its own.
func firstPermission(st stream.State) (stream.Pending, bool) {
	for _, p := range st.Pending {
		if p.Tool != "AskUserQuestion" {
			return p, true
		}
	}
	return stream.Pending{}, false
}

// streamPermission reads a waiting request as the permission the screen
// already knows how to draw. Its fingerprint is the id of the request: a
// press meant for one request never lands on the next.
func streamPermission(ctx context.Context, s liveSession) (*action.Permission, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return nil, err
	}
	p, ok := firstPermission(st)
	if !ok {
		return nil, nil
	}
	d := &action.Permission{
		Tool:        p.Tool,
		Action:      permissionLines(p),
		Fingerprint: p.RequestID,
		Note:        []string{},
		Raw:         []string{},
	}
	if p.Description != "" {
		d.Note = append(d.Note, p.Description)
	}
	if p.Reason != "" {
		d.Note = append(d.Note, "asked because: "+p.Reason)
	}
	d.Options = permissionOptions(p)
	return d, nil
}

func permissionOptions(p stream.Pending) []action.PermOption {
	if p.Tool == "ExitPlanMode" {
		return []action.PermOption{
			{N: 1, Text: "Yes, go ahead with the plan"},
			{N: 2, Text: "No, keep planning"},
		}
	}
	out := []action.PermOption{{N: 1, Text: "Yes"}}
	if len(suggestions(p)) > 0 {
		out = append(out, action.PermOption{N: 2, Text: "Yes, and don't ask again for this", Lasting: true})
	}
	return append(out, action.PermOption{N: len(out) + 1, Text: "No"})
}

func suggestions(p stream.Pending) []json.RawMessage {
	var out []json.RawMessage
	if len(p.Suggestions) == 0 || json.Unmarshal(p.Suggestions, &out) != nil {
		return nil
	}
	return out
}

// permissionLines is what the tool is about to do, in the lines a terminal
// dialog would show: the command, the file and its change, the plan.
func permissionLines(p stream.Pending) []string {
	var in map[string]any
	_ = json.Unmarshal(p.Input, &in)
	str := func(key string) string {
		v, _ := in[key].(string)
		return v
	}
	var lines []string
	switch {
	case str("command") != "":
		lines = strings.Split(str("command"), "\n")
	case str("plan") != "":
		lines = strings.Split(str("plan"), "\n")
	case str("file_path") != "":
		lines = []string{str("file_path")}
		for _, l := range strings.Split(str("old_string"), "\n") {
			if l != "" {
				lines = append(lines, "- "+l)
			}
		}
		for _, l := range strings.Split(firstOf(str("new_string"), str("content")), "\n") {
			if l != "" {
				lines = append(lines, "+ "+l)
			}
		}
	default:
		pretty, _ := json.MarshalIndent(in, "", "  ")
		lines = strings.Split(string(pretty), "\n")
	}
	if len(lines) > permLines {
		lines = append(lines[:permLines], fmt.Sprintf("… %d more lines", len(lines)-permLines))
	}
	return lines
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func streamPermit(ctx context.Context, s liveSession, pick *action.Permit) (string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	p, ok := firstPermission(st)
	if !ok {
		return "", fmt.Errorf("session %s is asking nothing — the request is already answered", s.Name)
	}
	if p.RequestID != pick.Fingerprint {
		return "", fmt.Errorf("the request of session %s changed while you were looking — look again", s.Name)
	}
	options := permissionOptions(p)
	var chosen *action.PermOption
	for i := range options {
		if options[i].N == pick.Option {
			chosen = &options[i]
		}
	}
	if chosen == nil {
		return "", fmt.Errorf("there is no item %d in the request of session %s", pick.Option, s.Name)
	}
	last := chosen.N == options[len(options)-1].N
	if last {
		if err := refuse(ctx, s, p.RequestID, refusedByPanel); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s refused in session %s", p.Tool, s.Name), nil
	}
	answer := map[string]any{"behavior": "allow", "updatedInput": json.RawMessage(orEmpty(p.Input))}
	if chosen.Lasting {
		answer["updatedPermissions"] = suggestions(p)
	}
	body, _ := json.Marshal(answer)
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpRespond, RequestID: p.RequestID, Response: body}); err != nil {
		return "", err
	}
	if chosen.Lasting {
		return fmt.Sprintf("%s allowed in session %s, and not asked again", p.Tool, s.Name), nil
	}
	return fmt.Sprintf("%s allowed in session %s", p.Tool, s.Name), nil
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// ------------------------------------------------------------ questions

type askInput struct {
	Questions []struct {
		Question string `json:"question"`
		Multi    bool   `json:"multiSelect"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"questions"`
}

func askPending(st stream.State, toolUseID string) (stream.Pending, bool) {
	for _, p := range st.Pending {
		if p.Tool == "AskUserQuestion" && p.ToolUseID == toolUseID {
			return p, true
		}
	}
	return stream.Pending{}, false
}

// streamAnswer answers a question with what was picked, by the words of the
// options rather than by keys: every layout of a question — one or several,
// with previews or without, own words — is the same answer on the stream.
func streamAnswer(ctx context.Context, s liveSession, toolUseID string, picks [][]int, texts []string) (string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	p, ok := askPending(st, toolUseID)
	if !ok {
		return "", fmt.Errorf("session %s is asking something else now: the previous question was answered outside the panel", s.Name)
	}
	var in askInput
	if err := json.Unmarshal(p.Input, &in); err != nil || len(in.Questions) == 0 {
		return "", fmt.Errorf("the question of session %s was not read: %v", s.Name, err)
	}
	answers, said, err := answersFor(in, picks, texts)
	if err != nil {
		return "", err
	}
	var full map[string]any
	_ = json.Unmarshal(p.Input, &full)
	full["answers"] = answers
	body, _ := json.Marshal(map[string]any{"behavior": "allow", "updatedInput": full})
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpRespond, RequestID: p.RequestID, Response: body}); err != nil {
		return "", err
	}
	return fmt.Sprintf("answer sent to %s: %s", s.Name, said), nil
}

func answersFor(in askInput, picks [][]int, texts []string) (map[string]string, string, error) {
	if len(picks) != len(in.Questions) {
		return nil, "", fmt.Errorf("there are %d questions, but %d answers arrived", len(in.Questions), len(picks))
	}
	answers := map[string]string{}
	var said []string
	for i, q := range in.Questions {
		own := ""
		if i < len(texts) {
			own = strings.TrimSpace(texts[i])
		}
		if own != "" {
			if len(picks[i]) > 0 {
				return nil, "", fmt.Errorf("question %d has both a picked option and a free-form answer", i+1)
			}
			answers[q.Question] = own
			said = append(said, fmt.Sprintf("in your own words, %d characters", len([]rune(own))))
			continue
		}
		if !q.Multi && len(picks[i]) > 1 {
			return nil, "", fmt.Errorf("question %d takes a single choice, but %d options arrived", i+1, len(picks[i]))
		}
		var labels []string
		for _, n := range picks[i] {
			if n < 1 || n > len(q.Options) {
				return nil, "", fmt.Errorf("question %d has only %d options, and %d was picked", i+1, len(q.Options), n)
			}
			labels = append(labels, q.Options[n-1].Label)
		}
		if len(labels) == 0 {
			said = append(said, "skipped")
			continue
		}
		answers[q.Question] = strings.Join(labels, ", ")
		said = append(said, strings.Join(labels, ", "))
	}
	if len(answers) == 0 {
		return nil, "", errors.New("there is nothing to send: no option was picked and not a word was written")
	}
	return answers, strings.Join(said, " · "), nil
}

func streamDismiss(ctx context.Context, s liveSession, toolUseID string) (string, error) {
	st, err := streamState(ctx, s)
	if err != nil {
		return "", err
	}
	p, ok := askPending(st, toolUseID)
	if !ok {
		return "", fmt.Errorf("session %s is asking something else now: the previous question was answered outside the panel", s.Name)
	}
	if err := refuse(ctx, s, p.RequestID, dismissedByPanel); err != nil {
		return "", err
	}
	return fmt.Sprintf("question dismissed in %s: the session is waiting for an ordinary message", s.Name), nil
}
