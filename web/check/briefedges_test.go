package check

import (
	"math"
	"testing"
)

// A monitor wide enough for the window of a brief, and the page of the shelf,
// to be wider than the reading column. On a narrower one the column fills
// either of them, and a strip held to the column looks the same as one that
// runs the width of its window.
const wideDeskScreen = `{"width":1920,"height":1080,"deviceScaleFactor":1,"mobile":false}`

type edgeSpan struct {
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
}

type edgeStrips struct {
	HeadTop    float64 `json:"headTop"`
	DockBottom float64 `json:"dockBottom"`
}

type briefEdges struct {
	Fill        edgeSpan              `json:"fill"`
	Head        edgeSpan              `json:"head"`
	Dock        edgeSpan              `json:"dock"`
	Page        edgeSpan              `json:"page"`
	Column      edgeSpan              `json:"column"`
	Arrow       float64               `json:"arrow"`
	Say         float64               `json:"say"`
	Last        float64               `json:"last"`
	HeadRadius  float64               `json:"headRadius"`
	DockRadius  float64               `json:"dockRadius"`
	FrameRadius float64               `json:"frameRadius"`
	Scrolls     float64               `json:"scrolls"`
	At          map[string]edgeStrips `json:"at"`
}

type briefEdgesShot struct {
	Window int        `json:"window"`
	Layer  briefEdges `json:"layer"`
	Shelf  briefEdges `json:"shelf"`
}

// The head and the dock of a brief ride along with the text, and at a desk the
// window they ride in is wider than the column the text is held to. Held to
// the column themselves, they stand inside the window with a gap at each end,
// and their square corners show against the window's round ones. So they run
// the width of whatever the document fills — the window over a conversation
// and the page of the shelf alike — while what they carry stays over the
// column: the arrow over the start of the text, the buttons ending where the
// text ends.
func TestTheStripsOfABriefRunTheWidthOfItsWindowAtADesk(t *testing.T) {
	var got briefEdgesShot
	runFixtureOn(t, "briefedges.html", wideDeskScreen, deskPointer, &got)

	near := func(a, b, by float64) bool { return math.Abs(a-b) <= by }
	for _, place := range []struct {
		name  string
		edges briefEdges
	}{{"the window over a conversation", got.Layer}, {"the page of the shelf", got.Shelf}} {
		e := place.edges
		if e.Fill.Right-e.Fill.Left < e.Page.Right-e.Page.Left+8 {
			t.Fatalf("in %s the document fills %.0f–%.0f and its page %.0f–%.0f: on a %d px screen the window is no wider than the page, and the test measures nothing",
				place.name, e.Fill.Left, e.Fill.Right, e.Page.Left, e.Page.Right, got.Window)
		}
		for _, strip := range []struct {
			name string
			span edgeSpan
		}{{"head", e.Head}, {"dock", e.Dock}} {
			if !near(strip.span.Left, e.Fill.Left, 1) || !near(strip.span.Right, e.Fill.Right, 1) {
				t.Errorf("in %s the %s runs %.1f–%.1f in a document that fills %.1f–%.1f: it stops short of the window with a gap at each end",
					place.name, strip.name, strip.span.Left, strip.span.Right, e.Fill.Left, e.Fill.Right)
			}
		}
		if !near(e.Arrow, e.Column.Left, 2) {
			t.Errorf("in %s the arrow stands at %.1f and the text starts at %.1f: the way out is not over the column", place.name, e.Arrow, e.Column.Left)
		}
		if !near(e.Say, e.Column.Left, 2) {
			t.Errorf("in %s the line of the dock starts at %.1f and the text at %.1f", place.name, e.Say, e.Column.Left)
		}
		if !near(e.Last, e.Column.Right, 2) {
			t.Errorf("in %s the last button ends at %.1f and the text at %.1f: the acts are not where the eye ends its line", place.name, e.Last, e.Column.Right)
		}
	}
}

// In the window a conversation opens a brief in, the head is the window's top
// edge and the dock its bottom one, all the way down the document: the head
// stays on the top while the text runs under it, the dock on the bottom, and
// at the end of the text the dock does not rise off the bottom to show a band
// of the window under it. Each takes the window's corners, so no square corner
// of a strip stands in a round corner of the window.
func TestTheDockOfABriefIsTheBottomEdgeOfItsWindowAtADesk(t *testing.T) {
	var got briefEdgesShot
	runFixtureOn(t, "briefedges.html", wideDeskScreen, deskPointer, &got)

	w := got.Layer
	if w.Scrolls < 200 {
		t.Fatalf("the document scrolls %.0f px in its window: the strips are never measured with the text running under them", w.Scrolls)
	}
	for _, at := range []string{"top", "middle", "end"} {
		s, ok := w.At[at]
		if !ok {
			t.Fatalf("the fixture did not measure the strips at the %s of the document", at)
		}
		if math.Abs(s.HeadTop) > 1 {
			t.Errorf("at the %s of the document the head stands %.1f px below the top of the window", at, s.HeadTop)
		}
		if math.Abs(s.DockBottom) > 1 {
			t.Errorf("at the %s of the document the dock stands %.1f px above the bottom of the window: a band of the window shows under the buttons", at, s.DockBottom)
		}
	}
	if w.FrameRadius == 0 {
		t.Fatal("the window has square corners: the fixture is not drawing the window a brief opens in")
	}
	if w.HeadRadius != w.FrameRadius {
		t.Errorf("the head's top corners are %.1f px round in a window whose corners are %.1f px", w.HeadRadius, w.FrameRadius)
	}
	if w.DockRadius != w.FrameRadius {
		t.Errorf("the dock's bottom corners are %.1f px round in a window whose corners are %.1f px: a step in each bottom corner", w.DockRadius, w.FrameRadius)
	}

	// The page of the shelf has no window of its own, and the document is drawn
	// the same there: the strips ride, and the dock closes it the way the head
	// opens it.
	s := got.Shelf
	if s.Scrolls < 200 {
		t.Fatalf("the document scrolls %.0f px on the shelf: the strips are never measured with the text running under them", s.Scrolls)
	}
	if top, mid := s.At["top"], s.At["middle"]; math.Abs(top.HeadTop-mid.HeadTop) > 1 || math.Abs(top.DockBottom-mid.DockBottom) > 1 {
		t.Errorf("on the shelf the strips move with the text: head %.1f → %.1f from the top, dock %.1f → %.1f from the bottom",
			top.HeadTop, mid.HeadTop, top.DockBottom, mid.DockBottom)
	}
	if s.DockRadius != s.HeadRadius {
		t.Errorf("on the shelf the dock's corners are %.1f px round and the head's %.1f px", s.DockRadius, s.HeadRadius)
	}
}
