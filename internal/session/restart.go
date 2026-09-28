package session

import (
	"context"
	"encoding/json"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// RestartName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__session_restart.
const RestartName = "session_restart"

// RestartInstructions is the tool's line in the server's word to every
// session that has it.
const RestartInstructions = "When the person asks to restart this session — “restart yourself”, " +
	"«перезапустись», «перезапусти сессию» — or a change to settings, a hook, CLAUDE.md or an MCP server " +
	"takes a new start to pick up, restart it with session_restart; continue keeps the conversation."

// RestartDescription is the tool's own word to the model.
const RestartDescription = "Restarts this session through the panel, as its project from the map: the same " +
	"directory and account, the same launch parameters — the console or the feed, the model — and the map's " +
	"message after a restart as its first. It restarts only this session: never a service, a container or " +
	"another session. Without continue the new session starts with an empty context, so put what is worth " +
	"keeping on disk first — notes, where the work stands, a commit; with continue it goes on with this " +
	"conversation. The session is closed right after the call, a session in the feed at the end of the " +
	"current turn: once the panel has taken the restart, end the turn and do nothing more. A restart ends everything the session runs — its agents, workflows " +
	"and background commands — so while any of them is at work nothing is restarted and the call says what " +
	"is at work; a wake-up the session set runs nothing and does not count. A restart you started on your " +
	"own: end the turn and restart once that work is done. A restart the person asked for: tell them what " +
	"is at work. anyway restarts all the same and ends that work: only on the person's word."

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
				"description": "Go on with this conversation in the new session instead of starting with an empty context."},
			"anyway": map[string]any{"type": "boolean",
				"description": "Restart even with agents, workflows or background commands at work; they end. Only on the person's word."},
		},
		"additionalProperties": false,
	}
}

func restart(ctx context.Context, h Host, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Continue bool `json:"continue"`
		Anyway   bool `json:"anyway"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Nothing was restarted: the arguments are not the tool's (" + err.Error() + ").", true
		}
	}
	b, err := bind()
	if err != nil {
		return "Nothing was restarted: " + err.Error(), true
	}
	if b.SessionID == "" {
		return "Nothing was restarted: the conversation of this session is not known yet — claude has not " +
			"written the file of this session. Call again in a moment.", true
	}
	if !args.Anyway {
		if w := h.atWork(b.SessionID, freshWait); w.any() {
			return waitLine(w), true
		}
	}
	params := map[string]any{"conversation": b.SessionID}
	if args.Continue {
		params["resume"] = true
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

// waitLine is what a restart that waits says to the session asking for it.
func waitLine(w work) string {
	one := w.Agents+w.Tasks+w.Workflows == 1
	are, them := "are", "them"
	if one {
		are, them = "is", "it"
	}
	return "WAIT: nothing was restarted. " + w.words() + " of this session " + are + " at work, and a restart ends " +
		them + " with the session. A restart you started on your own: end the turn, and restart once the work is " +
		"done. A restart the person asked for: tell them what is at work, and call again with anyway only when " +
		"they say so."
}
