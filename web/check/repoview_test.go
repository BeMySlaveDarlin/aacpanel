package check

import (
	"strings"
	"testing"
)

type repoShot struct {
	GutterInsideScroller bool     `json:"gutterInsideScroller"`
	GutterWidth          int      `json:"gutterWidth"`
	SrcScrolls           string   `json:"srcScrolls"`
	PageMoved            bool     `json:"pageMoved"`
	ContentVisibility    string   `json:"contentVisibility"`
	SpanClasses          []string `json:"spanClasses"`
	AddLines             int      `json:"addLines"`
	DelLines             int      `json:"delLines"`
	Tags                 []string `json:"tags"`
	Dots                 []string `json:"dots"`
	GutterTouchAction    string   `json:"gutterTouchAction"`
}

// The gutter with the numbers is the strip a finger swipes the page by and the
// handle a line is picked up with. Inside the sideways scroll of the code it is
// neither: it slides away with the diff, and a phone is left with nowhere to
// start a swipe from on a screen full of code.
func TestTheGutterStaysOutOfTheSidewaysScroll(t *testing.T) {
	var got repoShot
	runFixture(t, "repoview.html", &got)

	if got.GutterInsideScroller {
		t.Error("the gutter sits inside the block that scrolls sideways — it slides away with the code and takes the swipe with it")
	}
	if got.GutterWidth <= 0 {
		t.Error("the gutter has no width at all — there is nothing to tap and nothing to swipe by")
	}
	if got.GutterTouchAction != "pan-y" {
		t.Errorf("the gutter takes touch as %q: a horizontal drag there belongs to the page, not to the code", got.GutterTouchAction)
	}
}

// A line longer than the screen scrolls inside itself. What moves sideways in
// this panel is the contents of a block, never the screen.
func TestALongLineScrollsInsideItselfAndNotThePage(t *testing.T) {
	var got repoShot
	runFixture(t, "repoview.html", &got)

	if got.SrcScrolls != "auto" && got.SrcScrolls != "scroll" {
		t.Errorf("the code of a line scrolls as %q — a long line then drags the page sideways", got.SrcScrolls)
	}
	if got.PageMoved {
		t.Error("scrolling a line moved the page with it")
	}
}

// The browser skips drawing what is off screen by itself. The height of a line
// of code is known, so nothing has to be measured or windowed by hand — and a
// search of the page still finds what is further down the file, which a
// hand-written window would have thrown out of the document.
func TestTheRunOfLinesIsLeftToTheBrowserToSkip(t *testing.T) {
	var got repoShot
	runFixture(t, "repoview.html", &got)

	if got.ContentVisibility != "auto" {
		t.Errorf("the lines are drawn with content-visibility %q — either every line of every file is drawn at once, or a window was written by hand", got.ContentVisibility)
	}
}

// The spans the service sends are what colours the code. A class it numbers and
// the panel does not name paints code in the colour of nothing.
func TestTheSpansOfTheServiceArePaintedByTheirClasses(t *testing.T) {
	var got repoShot
	runFixture(t, "repoview.html", &got)

	if len(got.SpanClasses) == 0 {
		t.Fatal("no coloured spans on the screen — the lines came back as plain text")
	}
	for _, cls := range got.SpanClasses {
		if !strings.HasPrefix(cls, "cd") {
			t.Errorf("a span carries the class %q, which belongs to nothing in the panel", cls)
		}
	}
	var keyword, fn bool
	for _, cls := range got.SpanClasses {
		switch cls {
		case "cdkw":
			keyword = true
		case "cdfn":
			fn = true
		}
	}
	if !keyword {
		t.Error("no keyword was painted, though the fixture sends one")
	}
	if !fn {
		t.Error("the class for a function name paints nothing — it is numbered by the service and named by no stylesheet")
	}
}

// What is in a commit and what is not are told apart on every hunk and every
// file. A review answers for the two differently, and a screen that shows one
// mark for both asks a person to remember which is which.
func TestTheTwoLayersAreMarkedApart(t *testing.T) {
	var got repoShot
	runFixture(t, "repoview.html", &got)

	if got.AddLines == 0 || got.DelLines == 0 {
		t.Fatalf("the diff drew %d additions and %d deletions", got.AddLines, got.DelLines)
	}
	if len(got.Tags) == 0 {
		t.Fatal("a hunk came up without saying which layer it belongs to")
	}
	var says bool
	for _, tag := range got.Tags {
		if strings.Contains(strings.ToLower(tag), "commit") {
			says = true
		}
	}
	if !says {
		t.Errorf("the marks on the hunks read %v — none of them says anything about a commit", got.Tags)
	}
	// The dot of a file not yet committed is not the colour of a deletion: a
	// file marked with it reads as deleted rather than as unfinished.
	for _, dot := range got.Dots {
		if dot != "wt" && dot != "done" {
			t.Errorf("a file carries the mark %q, which the panel does not define", dot)
		}
	}
}
