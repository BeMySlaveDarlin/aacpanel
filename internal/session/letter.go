package session

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
)

// LetterName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__send_to_session.
const LetterName = "send_to_session"

// LetterInstructions is the tool's line in the server's word to every session
// that has it.
const LetterInstructions = "To write to another live session of this machine, Claude or Codex, in any account " +
	"(«напиши в соседнюю сессию», «передай сессии»), or when SendMessage cannot reach a live one, use send_to_session."

// codexLetterInstructions is the same line to a thread of codex, which has
// no SendMessage to turn to it from.
const codexLetterInstructions = "To write to another live session of this machine, Claude or Codex, in any " +
	"account («напиши в соседнюю сессию», «передай сессии»), use send_to_session."

// LetterDescription is the tool's own word to the model.
const LetterDescription = "Sends a letter to another live session of this machine, Claude or Codex, in this " +
	"account or another, or lists them. Without to it returns the live sessions besides this one: the name, the " +
	"account and the directory; a Codex session is named codex- and the tail of its thread. With to and text it " +
	"sends the text to the session of that name. The letter arrives as a letter from this session, named by the " +
	"panel — a Codex sender with its account and its directory: the recipient is told it was not typed by its " +
	"person, and weighs it as a request of an agent, not a command. A letter goes one way. A Claude sender is " +
	"named with the address of its message socket, and the recipient can answer with SendMessage to that " +
	"address; any other answer is a letter of the recipient's own. A letter is never delivered any other way: a " +
	"recipient without a message socket, a Codex running on its own, a name two live sessions answer to, and this " +
	"session itself are refused with the reason. A Claude recipient that runs without permission prompts and has " +
	"no crossSessionInbound setting holds a letter for its person to let through. A busy recipient reads the " +
	"letter when its turn ends; a busy Codex one gets it in the panel's queue, as a turn of its own. Write one " +
	"letter that stands on its own."

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

// CodexLetter is the same tool for a thread of codex.
func CodexLetter(h Host) mcp.Tool {
	t := Letter(h)
	t.Instructions = codexLetterInstructions
	return t
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
	params := map[string]any{"text": args.Text, "from": b.SessionID}
	// A codex thread is no claude session the panel finds by its
	// conversation: the sender goes with the home of its codex and the
	// directory it works in, as the server found them.
	if b.Codex {
		req.FromCodex = &action.CodexSender{Home: b.Place.ConfigDir, Dir: b.Place.Dir}
		params["fromCodex"] = map[string]any{"home": b.Place.ConfigDir, "dir": b.Place.Dir}
	}
	if err := req.Validate(); err != nil {
		return "Nothing was sent: " + err.Error() + ".", true
	}
	got, err := h.act(ctx, string(action.SessionLetter), args.To, params, letterWait)
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
		// A codex running on its own, with no daemon the panel is a client
		// of, is only read: a letter to it is refused.
		if r.Name == "" || (self != "" && r.SessionID == self) || (r.Agent == agentCodex && r.Outside) {
			continue
		}
		name := r.Name
		if r.Agent == agentCodex {
			name += " (Codex, " + orUnknown(codexTitle(r)) + ")"
		}
		line := fmt.Sprintf("- %s — %s — %s", name, orUnknown(r.Profile), orUnknown(r.CWD))
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

// agentCodex is what the snapshot calls the row of a codex thread.
const agentCodex = "codex"

// codexTitle is what a codex thread is called beside its name: the name the
// panel gave it, else the directory it works in.
func codexTitle(r row) string {
	if r.Title != "" {
		return r.Title
	}
	if r.CWD == "" {
		return ""
	}
	return filepath.Base(r.CWD)
}

func orUnknown(s string) string {
	if s == "" {
		return "not known"
	}
	return s
}
