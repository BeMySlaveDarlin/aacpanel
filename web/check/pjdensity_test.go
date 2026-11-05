package check

import (
	"os"
	"strings"
	"testing"
)

// The phone's list of a contour holds its blocks close: a block a few pixels
// from the next, a section heading near what it heads, the head of a block no
// taller than a line and its padding, the cards of their five lines, the last
// conversation as tall as a live card, while a button still takes a finger.
func TestThePhoneListHasNoSpansBetweenItsBlocks(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Between       int `json:"between"`
		BeforeSection int `json:"beforeSection"`
		UnderSection  int `json:"underSection"`
		Head          int `json:"head"`
		Live          int `json:"live"`
		Past          int `json:"past"`
		Button        int `json:"button"`
		IdleBlock     struct {
			Bg     string `json:"bg"`
			Border string `json:"border"`
		} `json:"idleBlock"`
		PastBlock struct {
			Bg     string `json:"bg"`
			Border string `json:"border"`
		} `json:"pastBlock"`
	}
	runFixture(t, "pjdensity.html", &got)
	if got.Between > 8 {
		t.Errorf("two blocks stand %d px apart — a span, not a gap", got.Between)
	}
	if got.BeforeSection > 16 || got.UnderSection > 6 {
		t.Errorf("a section heading stands %d px under the block above and %d px over its own", got.BeforeSection, got.UnderSection)
	}
	if got.Head > 44 {
		t.Errorf("the head of a block is %d px tall", got.Head)
	}
	// A card is five lines whatever it stands for: the last conversation of a
	// project is as tall as a live session's card, and both stay close.
	if got.Live > 150 || got.Past < got.Live-1 || got.Past > got.Live+1 {
		t.Errorf("a live card is %d px and the last conversation %d px — one skeleton, five lines close together", got.Live, got.Past)
	}
	if got.Button < 28 {
		t.Errorf("the resume button is %d px tall — too small for a finger", got.Button)
	}
	flat := func(bg string) bool { return strings.HasPrefix(bg, "rgba(0, 0, 0, 0) none") }
	if flat(got.IdleBlock.Bg) || got.IdleBlock.Border == "dashed" {
		t.Errorf("a quiet block with a live session is drawn like a closed one: %+v", got.IdleBlock)
	}
	if !flat(got.PastBlock.Bg) || got.PastBlock.Border != "dashed" {
		t.Errorf("a block with only its past is not drawn flat on a dashed edge: %+v", got.PastBlock)
	}
	t.Logf("%+v", got)
}
