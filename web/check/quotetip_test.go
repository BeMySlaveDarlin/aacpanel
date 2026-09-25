package check

import (
	"os"
	"strings"
	"testing"
)

type quoteSeen struct {
	Quote struct {
		Shown            bool    `json:"shown"`
		GapAbove         float64 `json:"gapAbove"`
		OffCentre        float64 `json:"offCentre"`
		InWindow         bool    `json:"inWindow"`
		OverComposer     bool    `json:"overComposer"`
		FootGap          float64 `json:"footGap"`
		OffFeedCentre    float64 `json:"offFeedCentre"`
		ClearOfSelection bool    `json:"clearOfSelection"`
		Composer         string  `json:"composer"`
		StillShown       bool    `json:"stillShown"`
		Where            any     `json:"where"`
	} `json:"quote"`
}

// pressedQuote checks what holds wherever the button stands: it comes up, it
// can be pressed, it leaves the composer alone, and a press quotes.
func pressedQuote(t *testing.T, got quoteSeen) {
	t.Helper()
	q := got.Quote
	if !q.Shown {
		t.Fatal("nothing came up for the selection — there is no way to quote at all")
	}
	if !q.InWindow {
		t.Error("the button landed outside the window — it cannot be pressed there")
	}
	if q.OverComposer {
		t.Error("the button covers the composer — that is the strip above it again, only harder to dismiss")
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
//
// The quote used to be offered by a strip above the composer. A strip is held
// on screen for something that happens rarely, and it appears exactly where the
// thumb is about to press — the feed moves under the finger at that moment. The
// button beside the selection is where the eye already is and moves nothing.
func TestQuoteTipStandsBesideTheSelection(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: where the button lands is the stylesheet's business — run make front first")
	}
	var got quoteSeen
	runFixtureOn(t, "feed.html", phoneScreen, deskPointer, &got)
	pressedQuote(t, got)
	q := got.Quote
	if q.GapAbove < 0 || q.GapAbove > 24 {
		t.Errorf("the button stands %.0f px above the selection — it is meant to touch the text it quotes, "+
			"not to float somewhere over the feed", q.GapAbove)
	}
	if q.OffCentre > 24 {
		t.Errorf("the button is %.0f px off the centre of the selection — it reads as belonging to another piece "+
			"of the conversation", q.OffCentre)
	}
}

// TestQuoteTipOnATouchScreenWaitsAtTheFootOfTheFeed is the same press on a
// touch screen. There the system puts its own bar — copy, select all, share —
// right over the selection, and the handles under it: a button beside the
// selection came up under that bar, where it could not be pressed.
func TestQuoteTipOnATouchScreenWaitsAtTheFootOfTheFeed(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: where the button lands is the stylesheet's business — run make front first")
	}
	var got quoteSeen
	runFixtureOn(t, "feed.html", phoneScreen, phonePointer, &got)
	pressedQuote(t, got)
	q := got.Quote
	if !q.ClearOfSelection {
		t.Errorf("the button stands over the selection or under it — where the system's bar and handles are: %v", q.Where)
	}
	if q.FootGap < 0 || q.FootGap > 24 {
		t.Errorf("the button stands %.0f px above the foot of the feed — it is meant to wait at the foot", q.FootGap)
	}
	if q.OffFeedCentre > 2 {
		t.Errorf("the button is %.0f px off the middle of the feed — at the right it meets the jump to the end", q.OffFeedCentre)
	}
}
