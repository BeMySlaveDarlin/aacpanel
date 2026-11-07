package action

import (
	"context"
	"errors"

	"aacpanel/internal/heavy"
)

const AskHeavy = "heavy"

// HeavyAsker reads the queue without adding an action to the host's surface.
type HeavyAsker interface {
	Heavy(context.Context) (*heavy.View, error)
}

func (c *Client) Heavy(ctx context.Context) (*heavy.View, error) {
	resp, err := c.Do(ctx, Request{Ask: AskHeavy})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	if resp.Heavy == nil {
		return nil, errors.New("the executor answered without the queue of heavy runs")
	}
	return resp.Heavy, nil
}
