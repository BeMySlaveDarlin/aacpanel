package check

import (
	"strings"
	"testing"
)

type quoteSeen struct {
	Quote struct {
		Shown      bool   `json:"shown"`
		Composer   string `json:"composer"`
		StillShown bool   `json:"stillShown"`
	} `json:"quote"`
}

// pressedQuote checks what holds wherever the button stands: it comes up, and
// a press quotes and takes it away.
func pressedQuote(t *testing.T, got quoteSeen) {
	t.Helper()
	q := got.Quote
	if !q.Shown {
		t.Fatal("nothing came up for the selection — there is no way to quote at all")
	}
	if !strings.HasPrefix(q.Composer, "> ") {
		t.Errorf("after the press the composer holds %q — the quote is what the press is for", q.Composer)
	}
	if q.StillShown {
		t.Error("the button stayed after the press — it has nothing left to offer, and it covers the feed")
	}
}

// TestQuoteTipStandsBesideTheSelection drives the chat screen in an engine with
// a mouse: selects a piece of an answer and presses what comes up.
func TestQuoteTipStandsBesideTheSelection(t *testing.T) {
	var got quoteSeen
	runFixtureOn(t, "feed.html", phoneScreen, deskPointer, &got)
	pressedQuote(t, got)
}

// TestQuoteTipOnATouchScreenWaitsAtTheFootOfTheFeed is the same press on a
// touch screen, where the button waits at the foot of the feed rather than
// beside the selection.
func TestQuoteTipOnATouchScreenWaitsAtTheFootOfTheFeed(t *testing.T) {
	var got quoteSeen
	runFixtureOn(t, "feed.html", phoneScreen, phonePointer, &got)
	pressedQuote(t, got)
}
