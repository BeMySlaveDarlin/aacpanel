package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// LetterName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__send_to_session.
const LetterName = "send_to_session"

// LetterInstructions is the tool's line in the server's word to every session
// that has it.
const LetterInstructions = "Use send_to_session for another live or sleeping session on this host " +
	"(«напиши в соседнюю сессию», «передай сессии»), including across accounts or when SendMessage cannot reach it."

// codexLetterInstructions is the same line to a thread of codex, which has
// no SendMessage to turn to it from.
const codexLetterInstructions = "Use send_to_session for another Claude or Codex session on this host, including a sleeping Claude, " +
	"across accounts too («напиши в соседнюю сессию», «передай сессии»)."

// LetterDescription is the tool's own word to the model.
const LetterDescription = "Lists or writes to Claude and Codex sessions on this host, across accounts. " +
	"Call without arguments to list names. A plain letter needs to and text (at most 50000 characters); use it when nothing is expected. " +
	"For an answer or work, add expects (reply or done), optional due (1–1440 minutes) and subject (at most 120 characters). " +
	"There is no default due unless the recipient's project sets one. Answer a claim with re and outcome (reply, done or cant); " +
	"to may be omitted. Only the recipient answers; the sender may withdraw with re and outcome withdraw. " +
	"A reply to a done claim is progress and leaves it open. A letter wakes a sleeping recipient and lifts its pause. " +
	"A busy Claude reads it after its turn, a busy Codex at the next step. Claude without crossSessionInbound may need permission. " +
	"Self, ambiguous names and recipients without a letter input are refused; delivery does not mean the work is done. " +
	"Continue independent work or end the turn for a reply; do not resend uncertain delivery blindly. " +
	"For a subagent use the client's agent messaging; for the person use a question or notify." + helperCycle

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
				"description": "The listed recipient name. May be omitted with re; a call without arguments lists sessions."},
			"text": map[string]any{"type": "string", "minLength": 1, "maxLength": action.TextMax,
				"description": "The letter, whole: the recipient reads it as it is."},
			"expects": map[string]any{"type": "string", "enum": []string{"reply", "done"}},
			"due":     map[string]any{"type": "integer", "minimum": 1, "maximum": 1440},
			"subject": map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
			"re":      map[string]any{"type": "string", "pattern": "^[a-f0-9]{32}$"},
			"outcome": map[string]any{"type": "string", "enum": []string{"reply", "done", "cant", "withdraw"}},
		},
		"additionalProperties": false,
	}
}

func letter(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		To   string `json:"to"`
		Text string `json:"text"`
		act.ClaimInput
	}
	if len(raw) > 0 {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&args); err != nil {
			return "Nothing was sent: the arguments are not the tool's (" + err.Error() + ").", true
		}
	}
	if err := args.ClaimInput.Validate(); err != nil {
		return "Nothing was sent: " + err.Error() + ".", true
	}
	b, bound := bind()
	if args.To == "" && args.Re == "" {
		if args.Text != "" || args.Expects != "" || args.Due != nil || args.Subject != "" || args.Outcome != "" {
			return "Nothing was sent: a letter needs to, the name of its recipient; a call without to and text " +
				"lists the sessions.", true
		}
		self := ""
		if bound == nil {
			self = b.SessionID
		}
		return listing(ctx, h, self)
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
	if args.Re != "" && req.Target == "" {
		req.Target = "claim"
	}
	if err := req.Validate(); err != nil {
		return "Nothing was sent: " + err.Error() + ".", true
	}
	if args.Expects != "" {
		params["expects"] = args.Expects
	}
	if args.Due != nil {
		params["due"] = *args.Due
	}
	if args.Subject != "" {
		params["subject"] = args.Subject
	}
	if args.Re != "" {
		params["re"] = args.Re
	}
	if args.Outcome != "" {
		params["outcome"] = args.Outcome
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
	if got.Claim != nil {
		outcome := ""
		if args.Re != "" {
			outcome = args.Result()
		}
		return got.Claim.ClaimReceipt(got.Said, outcome), false
	}
	return "Sent: " + got.Said + ".", false
}

// The panel adds sleeping conversations to the census; a process snapshot
// alone would make them disappear from the addresses an agent can choose.
func listing(ctx context.Context, h Host, self string) (string, bool) {
	var raw json.RawMessage
	if err := h.crewRead(ctx, "/api/host", &raw); err != nil {
		return "The sessions are not listed: " + err.Error(), true
	}
	s, err := readSnapshot(raw, "of the panel")
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
		if r.Sleeping {
			state := "sleeping"
			if r.PauseUntil != "" {
				state += " until " + r.PauseUntil
			}
			line += " (" + state + ")"
		}
		if named[r.Name] > 1 {
			line += " (two sessions answer to this name, so a letter to it is refused)"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "There is no other session on this machine.", false
	}
	return "The sessions of this machine besides this one — the name, the account, the directory. A letter wakes a sleeping recipient and lifts its pause:\n" +
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
