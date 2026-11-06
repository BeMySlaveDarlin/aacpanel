package check

import "testing"

// A permission asked on a phone by a tool whose input is a long text: the
// dock carries the whole request and the whole note, and what it folds of
// them is one press away.
func TestAPermissionOnAPhoneKeepsItsWholeRequestOnePressAway(t *testing.T) {
	var got struct {
		Request, Note, Fold, Unfolded bool
	}
	runFixture(t, "permitphone.html", &got)
	if !got.Request || !got.Note {
		t.Errorf("the end of the request (%v) or of the note (%v) is not in the dock at all", got.Request, got.Note)
	}
	if !got.Fold || !got.Unfolded {
		t.Errorf("nothing unfolds the request and the note (a fold %v, unfolded %v): the rest of them cannot be read", got.Fold, got.Unfolded)
	}
}
