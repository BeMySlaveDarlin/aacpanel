package action

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"aacpanel/internal/checklist"
)

const (
	AskChecklist    = "checklist.current"
	AskChildren     = "session.children"
	ChecklistLogMax = 10000
)

type ChecklistInput struct {
	Checklist ChecklistState `json:"checklist"`
	Log       string         `json:"log"`
}

type ChecklistState struct {
	Items []checklist.Item `json:"items"`
	Note  string           `json:"note,omitempty"`
}

type ChecklistVerdict struct {
	Current    bool    `json:"current"`
	Confidence float64 `json:"confidence"`
}

type ChildSession struct {
	Key          string `json:"key"`
	Conversation string `json:"conversation,omitempty"`
}

type ChecklistAsker interface {
	ChecklistCurrent(context.Context, string, string, ChecklistInput) (ChecklistVerdict, error)
}

type ChildrenAsker interface {
	Children(context.Context, string, string) ([]ChildSession, error)
}

func validateChecklist(r Request) error {
	if !safeUUID(r.Conversation) || r.Checklist == nil {
		return badRequest("a checklist review needs its conversation and input")
	}
	if r.Text != "" || len(r.History) > 0 || r.Part != "" || len(r.AlreadyAsked) > 0 {
		return badRequest("a checklist review carries only its checklist and log")
	}
	input := r.Checklist
	if strings.TrimSpace(input.Log) == "" || len([]rune(input.Log)) > ChecklistLogMax {
		return badRequest("a checklist log needs at most %d characters", ChecklistLogMax)
	}
	if len(input.Checklist.Items) == 0 || len(input.Checklist.Items) > checklist.MaxItems || len([]rune(input.Checklist.Note)) > checklist.MaxNote {
		return badRequest("a checklist review needs a bounded checklist")
	}
	for _, item := range input.Checklist.Items {
		if strings.TrimSpace(item.Text) == "" || len([]rune(item.Text)) > checklist.MaxText || !checklistStatus(item.Status) {
			return badRequest("a checklist review has an invalid step")
		}
	}
	return nil
}

func checklistStatus(status string) bool {
	return status == checklist.Pending || status == checklist.Active || status == checklist.Done || status == checklist.Dropped
}

// Missing fields cannot earn a negative verdict: false and zero are valid
// answers themselves and must be distinguished from an unreadable reply.
func ParseChecklistVerdict(raw []byte) (ChecklistVerdict, error) {
	var got struct {
		Current    *bool    `json:"current"`
		Confidence *float64 `json:"confidence"`
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&got) != nil || got.Current == nil || got.Confidence == nil || *got.Confidence < 0 || *got.Confidence > 1 {
		return ChecklistVerdict{}, errors.New("the checklist classifier returned an unreadable verdict")
	}
	if _, err := d.Token(); err != io.EOF {
		return ChecklistVerdict{}, errors.New("the checklist classifier returned more than one verdict")
	}
	return ChecklistVerdict{Current: *got.Current, Confidence: *got.Confidence}, nil
}

func (c *Client) ChecklistCurrent(ctx context.Context, target, conversation string, input ChecklistInput) (ChecklistVerdict, error) {
	resp, err := c.Do(ctx, Request{Ask: AskChecklist, Target: target, Conversation: conversation, Checklist: &input})
	if err != nil {
		return ChecklistVerdict{}, err
	}
	if !resp.OK {
		return ChecklistVerdict{}, errors.New(resp.Error)
	}
	if resp.Checklist == nil {
		return ChecklistVerdict{}, errors.New("the executor answered without a checklist verdict")
	}
	return *resp.Checklist, nil
}

func (c *Client) Children(ctx context.Context, target, conversation string) ([]ChildSession, error) {
	resp, err := c.Do(ctx, Request{Ask: AskChildren, Target: target, Conversation: conversation})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	return resp.Children, nil
}
