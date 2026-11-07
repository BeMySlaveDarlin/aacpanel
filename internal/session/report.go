package session

import (
	"context"
	"fmt"
	"time"

	"aacpanel/internal/action"
)

// A report may wait for the panel's admission and fresh census before its
// recipient can take a letter. A timeout is uncertain, never a blind retry.
func (h Host) Report(ctx context.Context, report action.HelperReport, text string) error {
	req := action.Request{ID: "report", Kind: action.SessionLetter, Target: "parent", Report: &report, Conversation: report.Parent, Text: text}
	if err := req.Validate(); err != nil {
		return err
	}
	answer, err := h.act(ctx, string(action.SessionLetter), "", map[string]any{"report": report, "text": text}, 10*time.Minute)
	if err != nil {
		return err
	}
	if answer.Late {
		return fmt.Errorf("the panel did not confirm the report before the caller stopped waiting; delivery may finish later")
	}
	if !answer.Taken {
		return fmt.Errorf("%s", answer.Said)
	}
	if !answer.Logged {
		return fmt.Errorf("the report delivery was not journaled")
	}
	return nil
}
