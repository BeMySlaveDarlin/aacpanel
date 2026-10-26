package turnend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"aacpanel/internal/mcp"
)

// ToolName is the name on the panel's server: Claude calls the tool as
// mcp__aacpanel__turn_end, while Codex names it on the server directly.
const ToolName = "turn_end"

// Instructions is the tool's line in the server's word. It reaches the
// conversation even when the client defers the tool's description.
const Instructions = "End every main-conversation turn with turn_end last: owner if you need the person, saying exactly what; " +
	"self if something set will wake you (background work, agent, alarm, observer, awaited letter); done if reported and nothing waits. Subagents never call it."

// Tool keeps the declaration of the bound conversation in dir with the time
// now gives. It is allowed because asking permission to say whose move comes
// next would itself leave the turn waiting on the person.
func Tool(dir string, now func() time.Time) mcp.Tool {
	return mcp.Tool{
		Name: ToolName, Title: "End turn", Allowed: true,
		Description: "Declares whose move follows this turn. owner means work cannot continue without the person: say exactly what you need in what. " +
			"self means something already set will wake you; done means the work is reported and nothing waits. " +
			"Call last at the end of every turn of the main conversation, never from a subagent. End the turn after calling.",
		Instructions: Instructions,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"next": map[string]any{"type": "string", "enum": []string{"owner", "self", "done"}},
				"what": map[string]any{"type": "string", "maxLength": MaxWhat, "description": "Exactly what you need from the person; required for owner."},
			},
			"required": []string{"next"}, "additionalProperties": false,
		},
		Call: func(_ context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			var args struct {
				Next string `json:"next"`
				What string `json:"what"`
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&args); err != nil {
				return "The turn was not kept: " + err.Error(), true
			}
			if _, err := dec.Token(); err != io.EOF {
				return "The turn was not kept: expected one object.", true
			}
			b, err := bind()
			if err != nil {
				return "The turn was not kept: " + err.Error(), true
			}
			if _, err := Keep(dir, b, args.Next, args.What, now()); err != nil {
				return "The turn was not kept: " + err.Error(), true
			}
			return "Noted: " + args.Next + ". End the turn now.", false
		},
	}
}
