package check

import (
	"strings"
	"testing"
)

// The answers a person marks and the message a session receives are one thing,
// and the panel is what joins them: the text is built from the saved draft.
// A save that fails silently parts the two — the document goes on taking
// answers, the message carries the ones that got through, and the line saying
// how many is a line nobody reads twice.
func TestABriefThatCannotSaveNeitherSendsNorKeepsQuiet(t *testing.T) {
	var got struct {
		Barred struct {
			Say      string `json:"say"`
			Disabled bool   `json:"disabled"`
			Sent     int    `json:"sent"`
		} `json:"barred"`
		Sent []struct {
			Kind   string         `json:"kind"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
		Kept int `json:"kept"`
	}
	runFixture(t, "briefsave.html", &got)

	if !got.Barred.Disabled {
		t.Error("the answers were not saved and sending was offered all the same")
	}
	if got.Barred.Sent != 0 {
		t.Errorf("%d messages went into the session from a document the panel never kept", got.Barred.Sent)
	}
	if !strings.Contains(got.Barred.Say, "not reaching the panel") {
		t.Errorf("the dock says %q while the answers are going nowhere", got.Barred.Say)
	}

	// The send happens before the timer behind the last answer runs out: what
	// is typed and not yet saved goes first, and the message carries it.
	if len(got.Sent) != 1 {
		t.Fatalf("the send left %d messages: %+v", len(got.Sent), got.Sent)
	}
	text, _ := got.Sent[0].Params["text"].(string)
	if !strings.Contains(text, "Answered 2 of 2") {
		t.Errorf("the message says %q — an answer still behind the save timer never reached the session", text)
	}
	if got.Kept != 2 {
		t.Errorf("the panel kept %d answers of the two that were marked", got.Kept)
	}
}
