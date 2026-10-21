package check

import (
	"os"
	"testing"
)

// Scrolled down the list of a contour page, the limits fold and stay at the
// top of the page: every plate is one line, the word of its agent, the window
// and the share, with the bar along its bottom edge, and the cards pass under
// them rather than over. They fold only once the list has carried them out of
// sight, so the cards do not jump, and open again only at the very top, so
// the height they give back does not carry the list over the bound: at every
// stop they stay as they came, frames after. Folded, no word is cut on the
// narrowest phone, where the widest share leaves the window a pixel to spare.
func TestTheLimitsFoldOverTheListScrolledPastThem(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type plate struct {
		Reads  string `json:"reads"`
		Height int    `json:"height"`
		Lines  int    `json:"lines"`
		BarGap int    `json:"barGap"`
	}
	type stop struct {
		Asked     int      `json:"asked"`
		ScrollTop int      `json:"scrollTop"`
		Tucked    bool     `json:"tucked"`
		Top       int      `json:"top"`
		Bottom    int      `json:"bottom"`
		Card      int      `json:"card"`
		OnTop     bool     `json:"onTop"`
		Cut       []string `json:"cut"`
		Plates    []plate  `json:"plates"`
	}
	for _, screen := range []struct{ name, metrics string }{{"360px", phone360}} {
		var got struct {
			Height       int    `json:"height"`
			ScrollHeight int    `json:"scrollHeight"`
			Stops        []stop `json:"stops"`
		}
		runFixtureOn(t, "limitsfold.html", screen.metrics, phonePointer, &got)
		at := screen.name
		if len(got.Stops) != 5 {
			t.Fatalf("%s: the fixture measured %d stops: %+v", at, len(got.Stops), got.Stops)
		}
		open, seen, past, near, top := got.Stops[0], got.Stops[1], got.Stops[2], got.Stops[3], got.Stops[4]
		if got.ScrollHeight-got.Height < past.Asked+open.Bottom {
			t.Fatalf("%s: the list scrolls %d px, too short to carry the limits out of sight and on", at, got.ScrollHeight-got.Height)
		}
		for _, s := range []stop{open, top} {
			if s.Tucked || s.Top != 0 || len(s.Plates) != 3 {
				t.Errorf("%s: at the top of the list the limits read %+v, expected them open where the page starts", at, s)
				continue
			}
			for _, l := range s.Plates {
				if l.Lines != 2 || l.Height != open.Plates[0].Height {
					t.Errorf("%s: at the top the window %q is %d px in %d lines, expected the open plate of two lines and %d px",
						at, l.Reads, l.Height, l.Lines, open.Plates[0].Height)
				}
			}
		}
		if seen.Tucked {
			t.Errorf("%s: the limits fold while a pixel of them is still in sight (%+v): the cards under them jump", at, seen)
		}
		if !past.Tucked || past.Top != 0 || !past.OnTop || len(past.Cut) > 0 {
			t.Errorf("%s: past the limits they read %+v, expected them folded at the top of the page, over the cards, "+
				"no word cut", at, past)
		}
		if past.Card != open.Card-past.Asked {
			t.Errorf("%s: the first card stands at %d once the limits fold, expected %d where the scroll left it",
				at, past.Card, open.Card-past.Asked)
		}
		want := []string{"Claude 99.5% 5h", "Claude 88% 7d", "Codex 44% 7d"}
		for i, l := range past.Plates {
			if i >= len(want) || l.Reads != want[i] || l.Lines != 1 || l.BarGap != 0 || 2*l.Height > open.Plates[0].Height {
				t.Errorf("%s: folded, a window reads %+v, expected %q on one line under half the open plate, "+
					"the bar along its bottom edge", at, l, want[min(i, len(want)-1)])
			}
		}
		if !near.Tucked || near.Top != 0 {
			t.Errorf("%s: a pixel short of the top the limits read %+v, expected them still folded: they open at the "+
				"very top only", at, near)
		}
	}
}
