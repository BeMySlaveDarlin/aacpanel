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
const Instructions = "Use request for human/agent step chains. Commands go in human command fields; checks go in agent steps. End the turn for a human step; summaries wake you for your step, human failure and completion."

const Description = "Creates a chain of 1 to 10 steps, starting with the person. Each step has who (you or agent), Markdown text (500 characters), and optionally command (2000 characters, only for you). The panel runs commands in the session directory; output stays in the act and arrives in summaries. Do not repeat steps in prose. Give title (120 characters) and blocking. Report your current agent step with id, step (from 1), outcome (done or failed), and optional note (2000 characters). A failed human step pauses the chain: use id with reopen (its step number), or withdraw: true. Every reply says whether to end the turn. A blocking question belongs to the client's question tool, a credential to secret_ask. Do not notify again: request already pushes. Do not repeat an uncertain delivery blindly."

// Tool binds a request to its caller and sends it through the local panel.
func Tool(panel string) mcp.Tool {
	return mcp.Tool{
		Name: "request", Title: "Ask the person to act", Allowed: true,
		Instructions: Instructions, Description: Description,
		InputSchema: inputSchema(),
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
			payload := map[string]any{"conversation": caller.SessionID, "kind": "do", "title": input.Title, "body": map[string]any{"steps": input.Steps}, "blocking": input.Blocking}
			if input.ID != "" {
				payload = map[string]any{"conversation": caller.SessionID, "id": input.ID}
				if input.Withdraw {
					payload["withdraw"] = true
				} else if input.Reopen != 0 {
					payload["reopen"] = input.Reopen
				} else {
					payload["step"], payload["outcome"], payload["note"] = input.Step, input.Outcome, input.Note
				}
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return "Nothing was requested: " + err.Error(), true
			}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(panel, "/")+"/api/acts", bytes.NewReader(body))
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
				ID      string `json:"id"`
				Message string `json:"message"`
			}
			if json.Unmarshal(data, &result) != nil || !ValidID(result.ID) {
				return "The panel's answer carries no request id. Do not repeat the request blindly.", true
			}
			if result.Message != "" {
				return result.Message, false
			}
			if input.Withdraw {
				return "Withdrawn request " + result.ID + ". End the turn if no independent work remains.", false
			}
			return "Requested as " + result.ID + ". Step 1 is the person's. End the turn: you are woken for your own step, when a step of the person fails, and when the chain ends.", false
		},
	}
}

func inputSchema() map[string]any {
	form := func(required, forbidden []string) map[string]any {
		absent := make([]any, 0, len(forbidden))
		for _, key := range forbidden {
			absent = append(absent, map[string]any{"required": []string{key}})
		}
		return map[string]any{"required": required, "not": map[string]any{"anyOf": absent}}
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"title":    map[string]any{"type": "string", "minLength": 1, "maxLength": TitleMax},
			"blocking": map[string]any{"type": "boolean"},
			"steps": map[string]any{"type": "array", "minItems": 1, "maxItems": StepsMax, "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"who", "text"},
				"properties": map[string]any{"who": map[string]any{"type": "string", "enum": []string{"you", "agent"}}, "text": map[string]any{"type": "string", "minLength": 1, "maxLength": TextMax}, "command": map[string]any{"type": "string", "minLength": 1, "maxLength": CommandMax}},
			}},
			"id":       map[string]any{"type": "string", "pattern": idPattern.String()},
			"withdraw": map[string]any{"type": "boolean", "const": true},
			"reopen":   map[string]any{"type": "integer", "minimum": 1, "maximum": StepsMax},
			"step":     map[string]any{"type": "integer", "minimum": 1, "maximum": StepsMax},
			"outcome":  map[string]any{"type": "string", "enum": []string{"done", "failed"}},
			"note":     map[string]any{"type": "string", "maxLength": NoteMax},
		},
		"oneOf": []any{
			form([]string{"title", "steps", "blocking"}, []string{"id", "withdraw", "reopen", "step", "outcome", "note"}),
			form([]string{"id", "step", "outcome"}, []string{"title", "steps", "blocking", "withdraw", "reopen"}),
			map[string]any{"oneOf": []any{
				form([]string{"id", "withdraw"}, []string{"title", "steps", "blocking", "step", "outcome", "note", "reopen"}),
				form([]string{"id", "reopen"}, []string{"title", "steps", "blocking", "step", "outcome", "note", "withdraw"}),
			}},
		},
	}
}
