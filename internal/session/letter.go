package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// LetterName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__send_to_session.
const LetterName = "send_to_session"

// LetterInstructions is the tool's line in the server's word to every session
// that has it.
const LetterInstructions = "To write to another live Claude session of this machine, in any account " +
	"(“tell the session in the work account”, «напиши в соседнюю сессию», «передай сессии»), or when SendMessage cannot " +
	"reach a live one, use send_to_session: without to it lists the sessions, with to and text it sends a letter."

// LetterDescription is the tool's own word to the model.
const LetterDescription = "Sends a letter to another live Claude session of this machine, in this account or " +
	"another, or lists them. Without to it returns the live sessions besides this one: the name, the account " +
	"and the directory. With to and text it sends the text to the session of that name. The letter arrives " +
	"as a letter from this session: the recipient's claude frames it as a message from another session, with " +
	"this session's name and the address of its message socket, and tells the model it was not typed by its " +
	"person. The recipient weighs it as a request of an agent, not a command, and can answer with SendMessage " +
	"to that address. A letter is never delivered any other way: a recipient without a message socket, a name " +
	"two live sessions answer to, and this session itself are refused with the reason. A recipient that runs " +
	"without permission prompts and has no crossSessionInbound setting holds a letter for its person to let " +
	"through. A busy recipient reads the letter when its turn ends. Write one letter that stands on its own."

// letterWait bounds the wait for the panel's answer: a letter is written to a
// socket, which takes a moment, and a panel that takes longer is stuck.
const letterWait = 15 * time.Second

// Letter is the tool that sends a letter from the session calling it, on the
// host h. It is not allowed: a letter asks the person, as any tool does.
func Letter(h Host) mcp.Tool {
	return mcp.Tool{
		Name:         LetterName,
		Title:        "Write to another session",
		Description:  LetterDescription,
		InputSchema:  letterSchema(),
		Instructions: LetterInstructions,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return letter(ctx, h, bind, args)
		},
	}
}

func letterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to": map[string]any{"type": "string",
				"description": "The name of the recipient session, as the list names it. Left out, the call lists the sessions."},
			"text": map[string]any{"type": "string", "minLength": 1, "maxLength": action.TextMax,
				"description": "The letter, whole: the recipient reads it as it is."},
		},
		"additionalProperties": false,
	}
}

func letter(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nothing was sent: the arguments are not the tool's (" + err.Error() + ").", true
		}
	}
	b, bound := bind()
	if args.To == "" {
		if args.Text != "" {
			return "Nothing was sent: a letter needs to, the name of its recipient; a call without to and text " +
				"lists the sessions.", true
		}
		self := ""
		if bound == nil {
			self = b.SessionID
		}
		return listing(h, self)
	}
	if bound != nil {
		return "Nothing was sent: " + bound.Error(), true
	}
	if b.SessionID == "" {
		return "Nothing was sent: the conversation of this session is not known yet — claude has not written " +
			"the file of this session, and a letter has no sender without it. Call again in a moment.", true
	}
	req := action.Request{ID: "letter", Kind: action.SessionLetter, Target: args.To, Text: args.Text, From: b.SessionID}
	if err := req.Validate(); err != nil {
		return "Nothing was sent: " + err.Error() + ".", true
	}
	got, err := h.act(ctx, string(action.SessionLetter), args.To,
		map[string]any{"text": args.Text, "from": b.SessionID}, letterWait)
	switch {
	case err != nil:
		return "Nothing was sent: " + err.Error() + ".", true
	case got.Late:
		return "The panel did not answer within " + letterWait.String() + ": whether the letter went is not known. " +
			"Do not send it again blindly — the recipient may have it.", true
	case !got.Taken:
		return "Nothing was sent: " + got.Said, true
	}
	return "Sent: " + got.Said + ".", false
}

// listing is the live sessions of the machine besides self, as the
// collector's snapshot has them.
func listing(h Host, self string) (string, bool) {
	s, err := h.read()
	if errors.Is(err, os.ErrNotExist) {
		return "The sessions are not listed: the collector has written no snapshot of the machine at " + h.State +
			" — is the panel's collector running?", true
	}
	if err != nil {
		return "The sessions are not listed: " + err.Error(), true
	}
	named := map[string]int{}
	for _, r := range s.Sessions {
		named[r.Name]++
	}
	var lines []string
	for _, r := range s.Sessions {
		if r.Name == "" || (self != "" && r.SessionID == self) {
			continue
		}
		line := fmt.Sprintf("- %s — %s — %s", r.Name, orUnknown(r.Profile), orUnknown(r.CWD))
		if named[r.Name] > 1 {
			line += " (two live sessions answer to this name, so a letter to it is refused)"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "There is no other live session on this machine.", false
	}
	return "The live sessions of this machine besides this one — the name, the account, the directory:\n" +
		strings.Join(lines, "\n"), false
}

func orUnknown(s string) string {
	if s == "" {
		return "not known"
	}
	return s
}
