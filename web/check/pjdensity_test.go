package check

import (
	"os"
	"strings"
	"testing"
)

// The phone's list of a contour holds its blocks close: a block a few pixels
// from the next, a section heading near what it heads, the head and the last
// conversation of a block no taller than a line and its padding, a live row
// no taller than its three lines — the name, the state and who runs it — and
// its padding, while a button still takes a finger.
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
		IdleDot string `json:"idleDot"`
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
	if got.Past > 50 {
		t.Errorf("the last conversation of a block is %d px tall", got.Past)
	}
	if got.Live > 72 {
		t.Errorf("a live row of a block is %d px tall — more than its three lines and its padding", got.Live)
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
	if strings.HasPrefix(got.IdleDot, "rgba(0, 0, 0, 0)") {
		t.Errorf("the dot of an idle live session is hollow (%s) — it reads as off", got.IdleDot)
	}
	t.Logf("%+v", got)
}
