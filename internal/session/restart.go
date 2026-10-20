package session

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// RestartName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__session_restart.
const RestartName = "session_restart"

// RestartInstructions is the tool's line in the server's word to every
// session that has it.
const RestartInstructions = "Use session_restart for an asked-for restart («перезапустись», «перезапусти сессию») " +
	"or a change needing a fresh start; continue only if asked to keep the conversation."

// RestartDescription is the tool's own word to the model.
const RestartDescription = "Restarts only this session through the panel with its project's directory, account " +
	"and launch settings. Use when asked, or when settings, hooks, CLAUDE.md, an MCP server or the context cap " +
	"require a new start. With no arguments it starts an empty context with the restart message. Save the work " +
	"on disk first as the project requires: its finalize skill if it has one, otherwise notes of the work and " +
	"next step. continue: true is only for an explicit request to keep the conversation, never to spare notes; " +
	"it requires intent, a message to yourself of at most 500 characters with the work and next step. intent " +
	"is accepted only with continue and arrives first in the resumed conversation. Active agents, workflows, " +
	"background commands or observers block the restart; wait for them to finish, or tell the person what is " +
	"running if they asked for the restart. anyway: true ends that work too, only on the person's word. Alarms " +
	"do not block a restart but end with this session. Use session_close to finish without a replacement; this " +
	"tool never restarts a service, container or another session. Once accepted, end the turn and do nothing " +
	"more: a stream session closes at that point."

// restartWait is how long the panel's answer is waited for. A refusal comes
// at once. A restart the panel took is answered only once the session is
// closed, and the session closes after this call returns: on the stream it is
// closed at the end of the turn the call is part of, and the turn cannot end
// while the call waits. So no answer by then is a restart under way.
var restartWait = 3 * time.Second

// Restart is the tool that restarts the session calling it, on the host h.
// It is not allowed: a restart asks the person, as any tool does.
func Restart(h Host) mcp.Tool {
	return mcp.Tool{
		Name:         RestartName,
		Title:        "Restart this session",
		Description:  RestartDescription,
		InputSchema:  restartSchema(),
		Instructions: RestartInstructions,
		// The restart past the context cap is done by the session alone,
		// with nobody at the screen to answer a prompt; the tool restarts
		// only the session that calls it and waits out its work.
		Allowed: true,
		Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return restart(ctx, h, bind, args)
		},
	}
}

func restartSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"continue": map[string]any{"type": "boolean",
				"description": "Go on with this conversation in the new session instead of starting with an empty context. Only when the person asks to keep the conversation."},
			"intent": map[string]any{"type": "string", "minLength": 1, "maxLength": action.IntentMax,
				"description": "With continue, and only with it: the message this session writes to itself — what it was doing and the step it takes next. It comes as the first message of the resumed conversation, in place of the map's message after a restart; nobody else writes it, and without it the session would come back and wait."},
			"anyway": map[string]any{"type": "boolean",
				"description": "Restart even with agents, workflows, background commands or observers at work; they end. Only on the person's word."},
		},
		"additionalProperties": false,
	}
}

func restart(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Continue bool   `json:"continue"`
		Intent   string `json:"intent"`
		Anyway   bool   `json:"anyway"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nothing was restarted: the arguments are not the tool's (" + err.Error() + ").", true
		}
	}
	switch {
	case args.Continue && strings.TrimSpace(args.Intent) == "":
		return "Nothing was restarted: continue needs intent, the message this session writes to itself — what " +
			"it was doing and the step it takes next. It comes first in the resumed conversation; without it the " +
			"session would come back and wait.", true
	case !args.Continue && args.Intent != "":
		return "Nothing was restarted: intent goes only with continue. A restart without it starts an empty " +
			"conversation, which opens with the map's message after a restart.", true
	}
	b, err := bind()
	if err != nil {
		return "Nothing was restarted: " + err.Error(), true
	}
	if b.SessionID == "" {
		return "Nothing was restarted: the conversation of this session is not known yet — claude has not " +
			"written the file of this session. Call again in a moment.", true
	}
	if args.Continue {
		going := action.Request{ID: "restart", Kind: action.SessionRestart, Target: "restart",
			Resume: b.SessionID, Text: args.Intent}
		if err := going.Validate(); err != nil {
			return "Nothing was restarted: " + err.Error() + ".", true
		}
	}
	if !args.Anyway {
		if w := h.atWork(b.SessionID, freshWait); w.any() {
			return waitLine(w, "restart", "restarted"), true
		}
	}
	params := map[string]any{"conversation": b.SessionID}
	if args.Continue {
		params["resume"], params["intent"] = true, args.Intent
	}
	got, err := h.act(ctx, string(action.SessionRestart), "", params, restartWait)
	switch {
	case err != nil:
		return "Nothing was restarted: " + err.Error() + ".", true
	case !got.Taken:
		return "The panel did not take the restart: " + got.Said, true
	case got.Late:
		return "The panel is restarting this session. It closes at the end of this turn: end the turn now " +
			"and do nothing more in it.", false
	}
	return "The panel restarted this session: " + got.Said + ". End the turn now and do nothing more in it.", false
}

// waitLine is what a restart or a close that waits says to the session asking
// for it: act names the one as a noun and a verb alike, done is what nothing
// was.
func waitLine(w work, act, done string) string {
	one := w.Agents+w.Tasks+w.Workflows+w.Observers == 1
	are, them := "are", "them"
	if one {
		are, them = "is", "it"
	}
	return "WAIT: nothing was " + done + ". " + w.words() + " of this session " + are + " at work, and a " + act +
		" ends " + them + " with the session. A " + act + " you started on your own: end the turn, and " + act +
		" once the work is done. A " + act + " the person asked for: tell them what is at work, and call again " +
		"with anyway only when they say so."
}
