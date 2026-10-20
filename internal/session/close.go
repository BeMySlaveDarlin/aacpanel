package session

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// CloseName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__session_close.
const CloseName = "session_close"

// CloseInstructions is the tool's line in the server's word to every session
// that has it.
const CloseInstructions = "Use session_close when asked («закройся», «выключись», «закрой сессию») or a " +
	"one-job Claude session finishes; name closes a child from session_open, after its report."

const codexCloseInstructions = "Use session_close when asked to close this thread or a child from session_open; " +
	"a Codex helper stays open for follow-ups until its parent or person asks it to close."

// closeRules are what the tool says to a claude session and to a thread of
// codex alike: whose word a close waits for, and what of the session's work
// holds it.
const closeRules = " Without name it closes only this session; with name, only a child this conversation opened " +
	"through session_open. Close when asked or when a one-job Claude session has finished. " +
	"A Codex helper stays open for follow-up turns until its parent or person explicitly asks it to close. " +
	"Save the work and give the report first. Agents, workflows, background commands and observers hold the " +
	"close: wait for them or report what is running. anyway: true ends that work too, only on the person's word. " +
	"Alarms do not hold the close but end with the session. Do not close just to await a reply; end the turn. " +
	"Use session_open to start or resume another conversation. After a child close, this session can continue."

// CloseDescription is the tool's own word to the model.
const CloseDescription = "Closes this session through the panel, as Close on its screen does, and starts nothing " +
	"in its place." + closeRules + " The session is closed right after the call, a session on the stream at the " +
	"end of the current turn: once the panel has taken a self-close, end the turn and do nothing more." + helperCycle

// codexCloseDescription is the tool's word to a thread of codex, which the
// panel closes as its Close does: the turn the thread runs — the one that
// makes the call — is interrupted, and the panel leaves the thread.
const codexCloseDescription = "Closes this thread of codex through the panel, as Close on its screen does: the " +
	"turn the thread runs — the one making this call — is interrupted and the panel leaves the thread; codex " +
	"resume brings it back." + closeRules + " After an accepted self-close, do nothing more." + helperCycle

// closeWait is how long the panel's answer is waited for, for the reason the
// restart waits as long: a close the panel took on the stream is answered
// only once the session is gone, after the turn this call is part of.
var closeWait = 3 * time.Second

// Close is the tool that closes the session calling it, on the host h. It is
// allowed: a session opened for one job closes itself once the job is done,
// with nobody at the screen to answer a prompt. A named child is checked by
// its opening record on the host; the close waits out the target's work.
func Close(h Host) mcp.Tool {
	return mcp.Tool{
		Name:         CloseName,
		Title:        "Close this session",
		Description:  CloseDescription,
		InputSchema:  closeSchema(),
		Instructions: CloseInstructions,
		Allowed:      true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return closeSelf(ctx, h, bind, args)
		},
	}
}

// CodexClose is the same tool for a thread of codex, bound to the thread the
// call proves.
func CodexClose(h Host) mcp.Tool {
	t := Close(h)
	t.Description, t.Instructions = codexCloseDescription, codexCloseInstructions
	return t
}

func closeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "A child opened by this conversation; omitted closes this session."},
			"anyway": map[string]any{"type": "boolean",
				"description": "Close even with agents, workflows, background commands or observers at work; they end. Only on the person's word."},
		},
		"additionalProperties": false,
	}
}

// closeSelf names the caller's conversation for a self-close, or sends that
// identity as Caller for a named child's close. The host checks the opening
// record before closing a child, independently of the collector snapshot.
func closeSelf(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Anyway bool   `json:"anyway"`
		Name   string `json:"name"`
	}
	if len(raw) > 0 {
		read := json.NewDecoder(bytes.NewReader(raw))
		read.DisallowUnknownFields()
		if err := read.Decode(&args); err != nil {
			return "Nothing was closed: the arguments are not the tool's (" + err.Error() + "). It closes this " +
				"session or a child it opened.", true
		}
	}
	b, err := bind()
	if err != nil {
		return "Nothing was closed: " + err.Error(), true
	}
	if b.SessionID == "" {
		return "Nothing was closed: the conversation of this session is not known yet — claude has not " +
			"written the file of this session. Call again in a moment.", true
	}
	if args.Name != "" {
		if err := action.ValidParent(args.Name); err != nil {
			return "Nothing was closed: " + err.Error(), true
		}
		if !args.Anyway {
			if snap, err := h.read(); err == nil {
				for _, row := range snap.Sessions {
					if row.Name == args.Name && row.SessionID != "" {
						if w := h.atWork(row.SessionID, freshWait); w.any() {
							return waitLine(w, "close", "closed"), true
						}
					}
				}
			}
		}
		got, err := h.act(ctx, string(action.SessionClose), args.Name, map[string]any{"caller": b.SessionID}, closeWait)
		if err != nil {
			return "Nothing was closed: " + err.Error(), true
		}
		if !got.Taken {
			return "The panel did not take the child close: " + got.Said, true
		}
		if got.Late {
			return "The panel is closing child " + args.Name + "; this session can continue.", false
		}
		return "The panel closed child " + args.Name + ": " + got.Said, false
	}
	if !args.Anyway {
		if w := h.atWork(b.SessionID, freshWait); w.any() {
			return waitLine(w, "close", "closed"), true
		}
	}
	got, err := h.act(ctx, string(action.SessionClose), "", map[string]any{"conversation": b.SessionID}, closeWait)
	switch {
	case err != nil:
		return "Nothing was closed: " + err.Error() + ".", true
	case !got.Taken:
		return "The panel did not take the close: " + got.Said, true
	case got.Late:
		return "The panel is closing this session. It closes at the end of this turn: end the turn now " +
			"and do nothing more in it.", false
	}
	return "The panel closed this session: " + got.Said + ". End the turn now and do nothing more in it.", false
}
