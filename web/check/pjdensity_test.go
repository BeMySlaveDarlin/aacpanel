package check

import (
	"os"
	"testing"
)

// The phone's list of a contour holds its blocks close: a block a few pixels
// from the next, a section heading near what it heads, the head and the rows
// of a block no taller than a line and its padding — while a button still
// takes a finger.
func TestThePhoneListHasNoSpansBetweenItsBlocks(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Between       int   `json:"between"`
		BeforeSection int   `json:"beforeSection"`
		UnderSection  int   `json:"underSection"`
		Head          int   `json:"head"`
		Rows          []int `json:"rows"`
		Button        int   `json:"button"`
	}
	runFixture(t, "pjdensity.html", &got)
	if got.Between > 8 {
		t.Errorf("two blocks stand %d px apart — a span, not a gap", got.Between)
	}
	if got.BeforeSection > 16 || got.UnderSection > 6 {
		t.Errorf("a section heading stands %d px under the block above and %d px over its own", got.BeforeSection, got.UnderSection)
	}
	if got.Head > 38 {
		t.Errorf("the head of a block is %d px tall", got.Head)
	}
	for _, h := range got.Rows {
		if h > 50 {
			t.Errorf("a row of a block is %d px tall: %v", h, got.Rows)
		}
	}
	if got.Button < 28 {
		t.Errorf("the resume button is %d px tall — too small for a finger", got.Button)
	}
	t.Logf("%+v", got)
}
