package check

import "testing"

// A permission asked on a phone by a tool whose input is a long text: the
// card carries the whole note before the whole request, and the request
// scrolls in a box of its own.
func TestAPermissionOnAPhoneKeepsItsWholeRequestInReach(t *testing.T) {
	var got struct {
		Request, Note, NoteFirst, Scrolls bool
	}
	runFixture(t, "permitphone.html", &got)
	if !got.Request || !got.Note {
		t.Errorf("the end of the request (%v) or of the note (%v) is not in the dock at all", got.Request, got.Note)
	}
	if !got.NoteFirst || !got.Scrolls {
		t.Errorf("the note does not stand before the command (%v) or the command does not scroll in its box (%v): the rest of it cannot be read", got.NoteFirst, got.Scrolls)
	}
}
