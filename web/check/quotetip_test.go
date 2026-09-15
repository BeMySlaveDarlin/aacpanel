package check

import (
	"os"
	"strings"
	"testing"
)

// TestQuoteTipStandsBesideTheSelection drives the chat screen in an engine:
// selects a piece of an answer and presses what comes up.
//
// The quote used to be offered by a strip above the composer. A strip is held
// on screen for something that happens rarely, and it appears exactly where the
// thumb is about to press — the feed moves under the finger at that moment. The
// button beside the selection is where the eye already is and moves nothing.
func TestQuoteTipStandsBesideTheSelection(t *testing.T) {
	var got struct {
		Quote struct {
			Shown        bool    `json:"shown"`
			GapAbove     float64 `json:"gapAbove"`
			OffCentre    float64 `json:"offCentre"`
			InWindow     bool    `json:"inWindow"`
			OverComposer bool    `json:"overComposer"`
			Composer     string  `json:"composer"`
			StillShown   bool    `json:"stillShown"`
		} `json:"quote"`
	}
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: where the button lands is the stylesheet's business — run make front first")
	}
	runFixture(t, "feed.html", &got)
	q := got.Quote

	if !q.Shown {
		t.Fatal("nothing came up beside the selection — there is no way to quote at all")
	}
	if q.GapAbove < 0 || q.GapAbove > 24 {
		t.Errorf("the button stands %.0f px above the selection — it is meant to touch the text it quotes, "+
			"not to float somewhere over the feed", q.GapAbove)
	}
	if q.OffCentre > 24 {
		t.Errorf("the button is %.0f px off the centre of the selection — it reads as belonging to another piece "+
			"of the conversation", q.OffCentre)
	}
	if !q.InWindow {
		t.Error("the button landed outside the window — a selection near an edge would make it unpressable")
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
