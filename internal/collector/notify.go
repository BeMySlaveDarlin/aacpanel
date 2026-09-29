package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/mcp"
)

// NotifyName is the call tool's name on the panel's server: the model calls
// it mcp__aacpanel__notify.
const NotifyName = "notify"

// notifyInstructions is the tool's line in the server's word to every
// session, which says when to call; notifyDescription is read once the tool is
// looked up and says when not to.
const notifyInstructions = "When the work is blocked on what only the person can decide and they are away, " +
	`or they asked to be called ("call me", "ping me"; in Russian "позови меня", "пингани", "дай знать на телефон"), ` +
	"call them with notify: one line to their phone, not a question."

const notifyDescription = "Calls the person to this session: the line arrives on their phone as a push, and a " +
	"tap opens this session in the panel. Not a question and not a permission request: nothing appears in the " +
	"conversation, nothing waits for an answer, and the turn goes on. Call when the work is blocked on a decision only " +
	"they can make, when something broke that they would want to know about now, or when they asked to be called " +
	"when a job finished. Not instead of finishing work that is merely hard, not for progress, and not in place " +
	"of a question when an answer is what you need and they are there. The line stands on its own, read on a " +
	"phone with no conversation around it: say what is stuck and why it needs them, not \"need you\". It goes " +
	"to a device outside this machine: no keys, no tokens, no contents of a private file. One call a minute " +
	"from a session, 300 characters to a line; the phone already showing this session gets nothing, and a call " +
	"nobody came for goes stale in a few minutes. A refusal says why nobody was called; say it in the " +
	"conversation instead."

// maxCall is the length of a line the collector keeps. The rest is cut here
// rather than there: the collector reads a call in one short read, and a line
// of pages would not parse.
const maxCall = 300

// callWait bounds one exchange with the collector: a call is one short line.
const callWait = 2 * time.Second

// Notify is the tool that calls the person through the collector on socket.
// It is allowed: a call asks nothing, and one that waited for a yes would
// wait for the person it is calling.
func Notify(socket string) mcp.Tool {
	return mcp.Tool{
		Name:         NotifyName,
		Title:        "Call the person",
		Description:  notifyDescription,
		InputSchema:  notifySchema(),
		Instructions: notifyInstructions,
		Allowed:      true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return call(ctx, socket, bind, args)
		},
	}
}

func notifySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "What to tell them, in one line of up to 300 characters.",
			},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	}
}

// call hands the line to the collector under the conversation of the claude
// the server serves, which is what the push opens.
func call(ctx context.Context, socket string, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Text string `json:"text"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nobody was called: the arguments are not a line (" + err.Error() + ").", true
		}
	}
	text := strings.Join(strings.Fields(args.Text), " ")
	if text == "" {
		return "Nobody was called: the line is empty, and a call carries something the person can act on.", true
	}
	cut := ""
	if n := utf8.RuneCountInString(text); n > maxCall {
		text = string([]rune(text)[:maxCall])
		cut = fmt.Sprintf(" The line was %d characters, and the push carries the first %d.", n, maxCall)
	}
	b, err := session(bind)
	if err != nil {
		return "Nobody was called: " + err.Error() + ".", true
	}
	body, err := json.Marshal(map[string]string{"sessionId": b.SessionID, "text": text})
	if err != nil {
		return "Nobody was called: " + err.Error() + ".", true
	}
	got, err := ask(ctx, socket, callWait, body)
	if err != nil {
		return "Nobody was called: " + err.Error() + ". Say it in the conversation instead.", true
	}
	if !got.OK {
		return "Nobody was called: " + refusal(got, "the collector refused the call") + ".", true
	}
	return "The person has been called: the push carries this line and opens this session." + cut, false
}
