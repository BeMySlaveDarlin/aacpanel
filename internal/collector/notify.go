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
const notifyInstructions = "Use notify to call the person for a blocker, urgent failure or an asked-for ping " +
	`("call me"; "позови меня", "пингани", "дай знать на телефон"); not for progress or a question dialog.`

const notifyDescription = "Sends text as a phone push; a tap opens this session. The line also becomes a feed " +
	"card and marks the session until the person opens it. Give one self-contained line of up to 300 characters: " +
	"what is blocked or broken and what the person needs to do. One call per session per minute; a device " +
	"already showing the session gets no push. No secrets or private file contents: the line leaves the host. " +
	"Call when only the person can unblock work, something needs their attention now, or they asked for a ping " +
	"when a job finishes. Put the actual question or instructions in the conversation or a brief first. " +
	"For an answer, use the client's question tool; question and permission dialogs already call the person, " +
	"so do not add notify. Use secret_ask for credentials, brief_publish for a long read. Do not call for " +
	"routine progress or instead of work you can finish. This tool waits for no reply: continue independent " +
	"work, or end the turn if blocked. A refusal means nobody was called; report it in the conversation."

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
