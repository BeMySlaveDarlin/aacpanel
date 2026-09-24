package check

import (
	"strings"
	"testing"
)

// A message sent to a busy session on the stream waits in its queue with a way
// back: edit takes it back into the composer, take back drops it, and a message
// the session read before the press stays, with the line saying so.
func TestAQueuedMessageOnTheStreamCanBeTakenBack(t *testing.T) {
	var got struct {
		Tools         int    `json:"tools"`
		Composer      string `json:"composer"`
		RowsAfterEdit int    `json:"rowsAfterEdit"`
		Fail          string `json:"fail"`
		RowsAfterRead int    `json:"rowsAfterRead"`
		Sent          []struct {
			Kind   string         `json:"kind"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
	}
	runFixture(t, "takeback.html", &got)

	if got.Tools != 1 {
		t.Fatalf("%d queue controls under one queued message", got.Tools)
	}
	if got.Composer != "run the e2e suite too" || got.RowsAfterEdit != 0 {
		t.Errorf("edit left %q in the composer and %d queued rows", got.Composer, got.RowsAfterEdit)
	}
	if !strings.Contains(got.Fail, "already delivered") || got.RowsAfterRead != 1 {
		t.Errorf("a message already read: line %q, %d rows left", got.Fail, got.RowsAfterRead)
	}
	var ids []string
	for _, s := range got.Sent {
		id, _ := s.Params["messageId"].(string)
		if id == "" {
			t.Errorf("%s went without the id of its message: %+v", s.Kind, s.Params)
		}
		ids = append(ids, s.Kind+":"+id)
	}
	if len(got.Sent) != 4 || !strings.HasPrefix(ids[1], "session.unqueue:") ||
		strings.TrimPrefix(ids[1], "session.unqueue:") != strings.TrimPrefix(ids[0], "session.send:") {
		t.Errorf("the take-back does not name the message that was sent: %v", ids)
	}
}
