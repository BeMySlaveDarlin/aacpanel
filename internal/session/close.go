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
const CloseInstructions = "When the person asks this session to close itself " +
	"(«закройся», «выключись», «закрой сессию») or it was opened for one job — by session_open, say — and " +
	"the job is done, close it with session_close."

// codexCloseInstructions is the same line to a thread of codex, which opens
// no sessions with session_open.
const codexCloseInstructions = "When the person asks this session to close itself " +
	"(«закройся», «выключись», «закрой сессию») or it was opened for one job and the job is done, close it " +
	"with session_close."

// closeRules are what the tool says to a claude session and to a thread of
// codex alike: whose word a close waits for, and what of the session's work
// holds it.
const closeRules = " It closes only this session: never a service, a container or another session. Only when " +
	"the person asks for it, or when the session was opened for one job and the job is done; put what the work " +
	"left on disk first, the way the project keeps it, and say your last words to the person before the call. " +
	"A close ends everything the session runs — its agents, workflows and background commands — so while any of " +
	"them is at work nothing is closed and the call says what is at work; a wake-up the session set runs nothing " +
	"and does not count. A close you decided on: end the turn and close once that work is done. A close the " +
	"person asked for: tell them what is at work. anyway closes all the same and ends that work: only on the " +
	"person's word."

// CloseDescription is the tool's own word to the model.
const CloseDescription = "Closes this session through the panel, as Close on its screen does, and starts nothing " +
	"in its place." + closeRules + " The session is closed right after the call, a session on the stream at the " +
	"end of the current turn: once the panel has taken the close, end the turn and do nothing more."

// codexCloseDescription is the tool's word to a thread of codex, which the
// panel closes as its Close does: the turn the thread runs — the one that
// makes the call — is interrupted, and the panel leaves the thread.
const codexCloseDescription = "Closes this thread of codex through the panel, as Close on its screen does: the " +
	"turn the thread runs — the one making this call — is interrupted and the panel leaves the thread; codex " +
	"resume brings it back." + closeRules

// closeWait is how long the panel's answer is waited for, for the reason the
// restart waits as long: a close the panel took on the stream is answered
// only once the session is gone, after the turn this call is part of.
var closeWait = 3 * time.Second

// Close is the tool that closes the session calling it, on the host h. It is
// allowed: a session opened for one job closes itself once the job is done,
// with nobody at the screen to answer a prompt, and the tool closes only the
// session that calls it and waits out its work.
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
			"anyway": map[string]any{"type": "boolean",
				"description": "Close even with agents, workflows or background commands at work; they end. Only on the person's word."},
		},
		"additionalProperties": false,
	}
}

// closeSelf asks the panel to close the session its server serves. The close
// names the conversation the server found — a claude's, or the thread a call
// of codex proved — and never a session of the model's choosing: the panel
// finds the session that runs it, and the executor closes that session only
// while it still runs it.
func closeSelf(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Anyway bool `json:"anyway"`
	}
	// A name the model adds is refused rather than passed over: a model that
	// takes the tool for a way to close another session would otherwise close
	// its own.
	if len(raw) > 0 {
		read := json.NewDecoder(bytes.NewReader(raw))
		read.DisallowUnknownFields()
		if err := read.Decode(&args); err != nil {
			return "Nothing was closed: the arguments are not the tool's (" + err.Error() + "). It closes this " +
				"session and takes no other.", true
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
