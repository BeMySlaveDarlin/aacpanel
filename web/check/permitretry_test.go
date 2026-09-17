package check

import (
	"strings"
	"testing"
)

// A permission read on a phone and answered a minute later. The executor
// compares the keypress against the dialog on the screen now, and refuses one
// aimed at a dialog that has moved on — telling the person to look again. The
// card has to do the looking: a refusal that leaves the old question on the
// screen is a loop, and the person presses the same dead button twice.
func TestAPermissionCardLooksAgainAfterARefusal(t *testing.T) {
	var got struct {
		Asked []string `json:"asked"`
		Sent  []struct {
			Kind   string         `json:"kind"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
		Read      string `json:"read"`
		AfterFail string `json:"afterFail"`
		Done      bool   `json:"done"`
	}
	runFixture(t, "permitretry.html", &got)

	if len(got.Sent) != 2 {
		t.Fatalf("the two presses sent %d requests: %+v", len(got.Sent), got.Sent)
	}
	// The end of the dialog travels with the keypress: a dialog taller than the
	// console screen is recognised by the end the two readings share.
	if tail, _ := got.Sent[0].Params["tail"].(string); tail == "" {
		t.Errorf("the keypress carried no tail of the dialog: %+v — a cut dialog then cannot be "+
			"answered from a phone at all", got.Sent[0].Params)
	}
	if !strings.Contains(got.Read, "LMS-13562") {
		t.Fatalf("the card did not show the dialog it was given: %q", got.Read)
	}
	if !strings.Contains(got.AfterFail, "rm -rf") {
		t.Errorf("after the refusal the card still shows the old question: %q — the person is told to look "+
			"again at a screen that does not change", got.AfterFail)
	}
	if got.Sent[1].Params["dialog"] != "bbbb2222" {
		t.Errorf("the second press carried %v — it went out for the question that is no longer asked",
			got.Sent[1].Params["dialog"])
	}
	if !got.Done {
		t.Error("the second press did not answer the dialog")
	}
}
