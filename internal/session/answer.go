package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/mcp"
)

const AnswerName = "session_answer"

const AnswerInstructions = "Use session_answer for an AskUserQuestion of a child this conversation opened; reply to a prose question with send_to_session."

const AnswerDescription = "Answers an AskUserQuestion of a child opened by this conversation through session_open. " +
	"Give name, ask (the question's toolUseId) and one answer per question in order: picks are exact option labels; " +
	"text is an answer of your own instead of picks. The executor checks the opening record, the current question " +
	"and its options. No other session, permission or MCP form can be answered. A question already answered is refused; " +
	"do not repeat an uncertain delivery blindly. Use send_to_session for a question in prose."

func CodexAnswer(h Host) mcp.Tool {
	t := Answer(h)
	t.Instructions = strings.ReplaceAll(t.Instructions, "an AskUserQuestion", "a blocking question")
	t.Description = strings.ReplaceAll(t.Description, "an AskUserQuestion", "a blocking question")
	return t
}

// The same ownership gate as a child close confines the tool to work its
// caller opened; it grants no permission on the host or another session.
func Answer(h Host) mcp.Tool {
	return mcp.Tool{
		Name: AnswerName, Title: "Answer a child session", Description: AnswerDescription,
		Instructions: AnswerInstructions, Allowed: true,
		InputSchema: map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"name", "ask", "answers"},
			"properties": map[string]any{
				"name": map[string]any{"type": "string"}, "ask": map[string]any{"type": "string"},
				"answers": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{
					"type": "object", "additionalProperties": false, "properties": map[string]any{
						"picks": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"text":  map[string]any{"type": "string", "maxLength": action.AskTextMax},
					},
				}},
			},
		},
		Call: func(ctx context.Context, bind mcp.Bind, raw json.RawMessage) (string, bool) {
			var args struct {
				Name    string `json:"name"`
				Ask     string `json:"ask"`
				Answers []struct {
					Picks []string `json:"picks"`
					Text  string   `json:"text"`
				} `json:"answers"`
			}
			read := json.NewDecoder(bytes.NewReader(raw))
			read.DisallowUnknownFields()
			if err := read.Decode(&args); err != nil {
				return "Nothing was answered: " + err.Error(), true
			}
			b, err := bind()
			if err != nil {
				return "Nothing was answered: " + err.Error(), true
			}
			if b.SessionID == "" || args.Name == "" || args.Ask == "" {
				return "Nothing was answered: the caller, child name and ask id must be known.", true
			}
			var reply struct {
				State *chat.Work `json:"state"`
			}
			if err := h.crewRead(ctx, "/api/chat?session="+url.QueryEscape(args.Name)+"&limit=1", &reply); err != nil {
				return "Nothing was answered: " + err.Error(), true
			}
			if reply.State == nil || reply.State.Ask == nil || reply.State.Ask.ToolUseID != args.Ask ||
				reply.State.Ask.Server != "" || reply.State.Ask.Async {
				return "Nothing was answered: the child is not waiting on that AskUserQuestion.", true
			}
			questions := reply.State.Ask.Questions
			if len(args.Answers) != len(questions) {
				return "Nothing was answered: give one answer for every question in the round.", true
			}
			picks := make([][]int, len(questions))
			texts := make([]string, len(questions))
			for i, answer := range args.Answers {
				picks[i] = []int{}
				if !questions[i].Multi && len(answer.Picks) > 1 {
					return fmt.Sprintf("Nothing was answered: question %d takes one option.", i+1), true
				}
				for _, label := range answer.Picks {
					found := 0
					for n, option := range questions[i].Options {
						if option.Label == label {
							if found != 0 {
								return fmt.Sprintf("Nothing was answered: question %d has more than one option %q.", i+1, label), true
							}
							found = n + 1
						}
					}
					if found == 0 {
						return fmt.Sprintf("Nothing was answered: question %d has no option %q.", i+1, label), true
					}
					picks[i] = append(picks[i], found)
				}
				texts[i] = answer.Text
			}
			req := action.Request{ID: "answer", Kind: action.SessionAnswer, Target: args.Name, Caller: b.SessionID,
				Answer: &action.Answer{AskID: args.Ask, Picks: picks, Texts: texts}}
			if err := req.Validate(); err != nil {
				return "Nothing was answered: " + err.Error(), true
			}
			got, err := h.act(ctx, string(action.SessionAnswer), args.Name,
				map[string]any{"caller": b.SessionID, "ask": args.Ask, "picks": picks, "texts": texts}, letterWait)
			if err != nil {
				return "The answer was not confirmed: " + err.Error() + ". Do not repeat it blindly.", true
			}
			if got.Late {
				return "The panel did not confirm the answer in time. Do not repeat it blindly.", true
			}
			if !got.Taken {
				return "Nothing was answered: " + got.Said, true
			}
			return "Answered: " + got.Said, false
		},
	}
}
