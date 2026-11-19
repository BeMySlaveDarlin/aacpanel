// Package ask leaves questions in the panel while the session works on.
package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"aacpanel/internal/act"
	"aacpanel/internal/mcp"
	"aacpanel/internal/question"
)

const Instructions = "Use ask when you can keep working while the person answers. Use AskUserQuestion when you cannot proceed without the answer. The panel delivers all answers in one letter with the ask id and wakes the session once."

func Tool(panel string) mcp.Tool {
	return mcp.Tool{
		Name: "ask", Title: "Ask while working", Allowed: true,
		Instructions: Instructions,
		Description:  "Asks 1 to 4 questions without blocking the session. Each has question (200 characters), header (12), options (0 to 4, label 40 and description 120), and multiSelect. No options means a free answer. Keep working: all answers arrive together in one panel letter with the ask id. Use AskUserQuestion when work cannot proceed without the answer.",
		InputSchema:  inputSchema(),
		Call: func(ctx context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			var in struct {
				Questions []struct {
					Question    string            `json:"question"`
					Header      string            `json:"header"`
					Options     []question.Option `json:"options"`
					MultiSelect bool              `json:"multiSelect"`
				} `json:"questions"`
			}
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err := d.Decode(&in); err != nil {
				return "Nothing was asked: " + err.Error(), true
			}
			if len(in.Questions) < 1 || len(in.Questions) > 4 {
				return "Nothing was asked: give 1 to 4 questions.", true
			}
			questions := []question.Question{}
			for _, q := range in.Questions {
				if len(q.Options) > 4 {
					return "Nothing was asked: give at most 4 options per question.", true
				}
				questions = append(questions, question.Question{Question: q.Question, Header: q.Header, Options: q.Options, Multi: q.MultiSelect})
			}
			questions = question.Clean(questions)
			if len(questions) == 0 {
				return "Nothing was asked: the questions are empty.", true
			}
			caller, err := bind()
			if err != nil {
				return "Nothing was asked: " + err.Error(), true
			}
			if caller.SessionID == "" {
				return "Nothing was asked: the conversation is not known yet.", true
			}
			blocking := false
			what := question.Line(questions[0].Question, 140)
			id, err := act.Create(ctx, panel, caller.SessionID, act.Input{Kind: "ask", Title: question.Line(what, 120), Blocking: &blocking,
				Body: act.Body{Source: "tool", What: what, Questions: questions}})
			if err != nil {
				return "The panel did not confirm the ask: " + err.Error() + ". Do not repeat it blindly.", true
			}
			return "Asked as " + id + ".", false
		},
	}
}

func inputSchema() map[string]any {
	text := func(limit int) map[string]any { return map[string]any{"type": "string", "maxLength": limit} }
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"questions"}, "properties": map[string]any{
		"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"question", "header", "options", "multiSelect"}, "properties": map[string]any{
				"question": text(200), "header": text(12), "multiSelect": map[string]any{"type": "boolean"},
				"options": map[string]any{"type": "array", "minItems": 0, "maxItems": 4, "items": map[string]any{
					"type": "object", "additionalProperties": false, "required": []string{"label", "description"}, "properties": map[string]any{"label": text(40), "description": text(120)},
				}},
			},
		}},
	}}
}

func CodexTool(panel string) mcp.Tool {
	tool := Tool(panel)
	tool.Instructions = strings.ReplaceAll(tool.Instructions, "AskUserQuestion", "the client's question tool")
	tool.Description = strings.ReplaceAll(tool.Description, "AskUserQuestion", "the client's question tool")
	return tool
}
