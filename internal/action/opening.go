package action

import (
	"context"
	"fmt"
)

const AskOpening = "session.opening"

// Opening names the conversation that opened a child, independently of the
// grouping on the screen. Letters says its live session can receive mail.
type Opening struct {
	Name         string `json:"name"`
	Conversation string `json:"conversation"`
	Letters      bool   `json:"letters"`
}

type OpeningAsker interface {
	Opening(context.Context, string, string) (*Opening, error)
}

func (c *Client) Opening(ctx context.Context, target, conversation string) (*Opening, error) {
	resp, err := c.Do(ctx, Request{Ask: AskOpening, Target: target, Conversation: conversation})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Opening, nil
}
