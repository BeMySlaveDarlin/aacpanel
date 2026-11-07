package action

import (
	"context"
	"fmt"

	"aacpanel/internal/heavy"
)

// AskHeadroom reads the host's memory and its actual agents' MemoryHigh.
// The service cannot infer either from its container's cgroup namespace.
const AskHeadroom = "headroom"

type Headroom struct {
	BootID  string        `json:"bootId"`
	High    uint64        `json:"high"`
	Measure heavy.Measure `json:"measure"`
}

type HeadroomAsker interface {
	Headroom(context.Context) (*Headroom, error)
}

func (c *Client) Headroom(ctx context.Context) (*Headroom, error) {
	resp, err := c.Do(ctx, Request{Ask: AskHeadroom})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	if resp.Headroom == nil {
		return nil, fmt.Errorf("the executor answered without the host's headroom")
	}
	return resp.Headroom, nil
}
