package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
)

// Ready acknowledges the panel's context cycle without ending the process
// while this tool call, or another call in the same turn, is still running.
func Ready(h Host) mcp.Tool {
	return mcp.Tool{
		Name: "session_ready", Title: "Ready for the panel's restart", Allowed: true,
		Instructions: "Use session_ready only after the panel asks this session to save for a restart; then end the turn and wait.",
		Description:  "Reports that this session has saved its state and stopped its background work after the panel asked it to prepare for a context restart. It restarts nothing. Optional intent is the first message of the fresh session, up to 500 characters, without a leading dash or slash. After acceptance, end the turn and wait. Without a request from the panel, use session_restart for a restart the person asked for.",
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"intent": map[string]any{
				"type": "string", "minLength": 1, "maxLength": action.IntentMax,
				"description": "The first message of the fresh session: where the saved state is and the next step.",
			}},
		},
		Call: func(ctx context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			var args struct {
				Intent string `json:"intent"`
			}
			if len(raw) > 0 {
				d := json.NewDecoder(bytes.NewReader(raw))
				d.DisallowUnknownFields()
				if err := d.Decode(&args); err != nil {
					return "Readiness was not accepted: the arguments are not the tool's (" + err.Error() + ").", true
				}
				if d.Decode(new(json.RawMessage)) != io.EOF {
					return "Readiness was not accepted: the arguments contain more than one value.", true
				}
			}
			b, err := bind()
			if err != nil {
				return "Readiness was not accepted: " + err.Error(), true
			}
			r := action.Ready{Conversation: b.SessionID, Intent: args.Intent}
			if err := r.Validate(); err != nil {
				return "Readiness was not accepted: " + err.Error(), true
			}
			got, err := h.act(ctx, string(action.SessionReady), "", map[string]any{
				"conversation": r.Conversation, "intent": r.Intent,
			}, letterWait)
			switch {
			case err != nil:
				return "Readiness was not accepted: " + err.Error(), true
			case got.Late:
				return "The panel has not confirmed readiness. Call session_ready again to confirm it.", true
			case !got.Taken:
				return "Readiness was not accepted: " + got.Said, true
			default:
				return "Readiness accepted. End the turn and wait for the panel to restart this session.", false
			}
		},
	}
}
