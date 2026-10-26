package check

import (
	"os"
	"testing"
)

// On a phone the quiet words after a session's state — its age, an alarm,
// the automatic restart, where it lives — never break the state in the middle
// of a word: a short state stays on one line and the words that do not fit
// beside it go to the next.
func TestTheQuietWordsOfAStateLeaveItsWordsWhole(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Text     string
		Lines    int
		Marks    int
		LongText string
		LongOff  float64
	}
	runFixture(t, "sessionstatefit.html", &got)
	if got.Text != "working" || got.Marks < 4 {
		t.Fatalf("the row says %q with %d quiet words, expected working with its age, alarm, restart and place", got.Text, got.Marks)
	}
	if got.Lines != 1 {
		t.Errorf("%q is laid out on %d lines — the words beside it break it mid-word", got.Text, got.Lines)
	}
	// A state longer than the line stays beside its dot and wraps there,
	// rather than leaving the dot alone on a line of its own.
	if got.LongText == "" || got.LongOff > 6 {
		t.Errorf("the long state %q stands %.0f px off the line of its dot", got.LongText, got.LongOff)
	}
}
