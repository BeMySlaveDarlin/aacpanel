package action

import (
	"context"
	"fmt"
	"time"
)

const AskSleepingAlarms = "alarms.sleeping"

type SleepingAlarm struct {
	ID           string    `json:"id"`
	Session      string    `json:"session"`
	Conversation string    `json:"conversation"`
	Next         time.Time `json:"next"`
}

type SleepingAlarms struct {
	Alarms []SleepingAlarm `json:"alarms"`
}

type SleepingAlarmsAsker interface {
	SleepingAlarms(context.Context) (*SleepingAlarms, error)
}

func (c *Client) SleepingAlarms(ctx context.Context) (*SleepingAlarms, error) {
	resp, err := c.Do(ctx, Request{Ask: AskSleepingAlarms})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	if resp.SleepingAlarms == nil {
		return nil, fmt.Errorf("the executor answered without its sleeping alarms")
	}
	return resp.SleepingAlarms, nil
}
