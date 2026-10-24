package action

import (
	"context"
	"errors"
	"strings"
)

// AskQuestion classifies a completed assistant message in its session's
// contour. It never resumes or sends input to the session itself.
const AskQuestion = "question"

type QuestionAsker interface {
	Question(ctx context.Context, target, conversation, text string) (bool, error)
}

func validateQuestion(r Request) error {
	if !safeUUID(r.Conversation) || strings.TrimSpace(r.Text) == "" || len([]rune(r.Text)) > TextMax {
		return badRequest("question classification needs a conversation and a message of at most %d characters", TextMax)
	}
	if len(r.History) != 0 || r.Part != "" {
		return badRequest("question classification carries no history or settings screen")
	}
	return nil
}

func (c *Client) Question(ctx context.Context, target, conversation, text string) (bool, error) {
	resp, err := c.Do(ctx, Request{Ask: AskQuestion, Target: target, Conversation: conversation, Text: text})
	if err != nil {
		return false, err
	}
	if !resp.OK {
		return false, errors.New(resp.Error)
	}
	if resp.Question == nil {
		return false, errors.New("the executor answered without a question verdict")
	}
	return *resp.Question, nil
}
