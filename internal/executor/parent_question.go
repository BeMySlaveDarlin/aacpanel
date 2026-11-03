package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

// Parent letters are panel events, not words attributed to either the
// child or its person. The opening is checked again at delivery so a stale
// service snapshot cannot route a question to another conversation.
func (e *Executor) parentQuestionLetter(ctx context.Context, req action.Request) (string, error) {
	child := ""
	if th, ok := e.codex.Thread(req.ParentQuestion); ok {
		child = th.Name
	} else if s, err := liveSessionOf(req.ParentQuestion); err == nil {
		child = s.Name
	}
	if child == "" {
		return "", fmt.Errorf("the child asking its parent is no longer live")
	}
	opening, err := e.Opening(ctx, child, req.ParentQuestion)
	if err != nil {
		return "", err
	}
	if opening == nil || !opening.Letters || opening.Conversation != req.Conversation || opening.Name != req.Target {
		return "", fmt.Errorf("the child's opening has no live parent that can take this letter")
	}
	const sender = "panel question"
	if th, ok := e.codex.Thread(opening.Conversation); ok {
		_, err := th.Link.Send(ctx, th.ID, codex.Message{Text: "A letter of the panel about a question from your child. " +
			"Your person did not type it.\n\n" + letterEnvelope("", sender, req.Text)})
		return "the panel sent its child's question to " + opening.Name, err
	}
	s, err := liveSessionOf(opening.Conversation)
	if err != nil {
		return "", err
	}
	if onStream(s) {
		_, err = streamAsk(ctx, s, stream.Request{Op: stream.OpLetter, Text: letterEnvelope("", sender, req.Text)})
	} else {
		err = writeLine(ctx, s.Socket, map[string]any{"type": "user", "message": map[string]any{
			"role": "user", "content": letterEnvelope("", sender, req.Text)}})
	}
	return "the panel sent its child's question to " + opening.Name, err
}
