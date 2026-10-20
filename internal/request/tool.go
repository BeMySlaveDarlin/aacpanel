package request

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/mcp"
)

// Instructions is the place of a request among the ways to reach the person.
const Instructions = "Use request when the person must do something outside the conversation: run a command elsewhere, sign in or handle a device. If blocking, end the turn and wait for their answer."

// Description explains what happens after the person is asked to act.
const Description = "Leaves a task for the person's hands in the panel and pushes it to their phone: " +
	"run a command on another machine, sign in, flip a device or approve something elsewhere. " +
	"Give a title of at most 120 characters, steps as Markdown of at most 2000 characters (commands in code blocks), " +
	"and blocking: whether this session waits on it. The person answers Done or Can't, with an optional note; " +
	"the answer comes back here as a message. If blocking, end the turn and wait for that message; otherwise " +
	"continue independent work. Returns an id. To withdraw your own open request, give only id and withdraw: true. " +
	"A blocking question belongs to the client's question tool; a mere call to notify; a credential to secret_ask. " +
	"Do not notify again for this task: request already pushes. An uncertain delivery is not a reason to repeat it blindly."

// Tool binds a request to its caller and sends it through the local panel.
func Tool(panel string) mcp.Tool {
	return mcp.Tool{
		Name: "request", Title: "Ask the person to act", Allowed: true,
		Instructions: Instructions, Description: Description,
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"title":    map[string]any{"type": "string", "minLength": 1, "maxLength": TitleMax},
				"steps":    map[string]any{"type": "string", "minLength": 1, "maxLength": StepsMax},
				"blocking": map[string]any{"type": "boolean"},
				"id":       map[string]any{"type": "string", "pattern": idPattern.String()},
				"withdraw": map[string]any{"type": "boolean", "const": true},
			},
			"oneOf": []any{map[string]any{"required": []string{"title", "steps", "blocking"}},
				map[string]any{"required": []string{"id", "withdraw"}}},
		},
		Call: func(ctx context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			var input Input
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return "Nothing was requested: " + err.Error(), true
			}
			if err := input.Validate(); err != nil {
				return "Nothing was requested: " + err.Error(), true
			}
			caller, err := bind()
			if err != nil {
				return "Nothing was requested: " + err.Error(), true
			}
			if caller.SessionID == "" {
				return "Nothing was requested: the conversation of this session is not known yet.", true
			}
			body, err := json.Marshal(struct {
				Conversation string `json:"conversation"`
				Input
			}{caller.SessionID, input})
			if err != nil {
				return "Nothing was requested: " + err.Error(), true
			}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(panel, "/")+"/api/requests", bytes.NewReader(body))
			if err != nil {
				return "Nothing was requested: " + err.Error(), true
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "The panel did not confirm the request: " + err.Error() + ". Do not repeat it blindly.", true
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
			if err != nil {
				return "The panel's answer was not read. Do not repeat the request blindly.", true
			}
			if resp.StatusCode != http.StatusOK {
				return "Nothing was requested: " + strings.TrimSpace(string(data)), true
			}
			var result struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(data, &result) != nil || !ValidID(result.ID) {
				return "The panel's answer carries no request id. Do not repeat the request blindly.", true
			}
			if input.Withdraw {
				return "Withdrawn request " + result.ID + ".", false
			}
			next := "Continue independent work; the person's answer will arrive as a message."
			if *input.Blocking {
				next = "End the turn and wait for the person's answer message."
			}
			return "Requested as " + result.ID + ". " + next, false
		},
	}
}
