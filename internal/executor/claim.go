package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

func (e *Executor) claimLetter(ctx context.Context, req action.Request) (string, error) {
	text := letterEnvelope("", "panel claim", req.Text)
	if th, ok := e.codex.Thread(req.Conversation); ok {
		if th.Name != req.Target {
			return "", fmt.Errorf("the claim recipient's name changed")
		}
		_, err := th.Link.Send(ctx, th.ID, codex.Message{ID: req.MessageID, Text: "A letter of the panel about a claim. Your person did not type it.\n\n" + text})
		return "the panel sent claim mail to " + req.Target, err
	}
	s, err := liveSessionOf(req.Conversation)
	if err != nil {
		return "", err
	}
	if s.Name != req.Target {
		return "", fmt.Errorf("the claim recipient's name changed")
	}
	if onStream(s) {
		_, err = streamAsk(ctx, s, stream.Request{Op: stream.OpLetter, UUID: req.MessageID, Text: text})
	} else if s.Socket == "" {
		err = fmt.Errorf("session %s takes no letters", s.Name)
	} else {
		err = writeLine(ctx, s.Socket, map[string]any{"type": "user", "uuid": req.MessageID,
			"message": map[string]any{"role": "user", "content": text}})
	}
	return "the panel sent claim mail to " + req.Target, err
}
