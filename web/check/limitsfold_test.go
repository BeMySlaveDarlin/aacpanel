package check

import (
	"strings"
	"testing"
)

// Scrolled down the list of a contour page, the limits fold, each plate
// saying the word of its agent, the share and the window. They fold only once
// the list has carried them out of sight, and open again only at the very top:
// at every stop they stay as they came, frames after.
func TestTheLimitsFoldOverTheListScrolledPastThem(t *testing.T) {
	type stop struct {
		Tucked bool     `json:"tucked"`
		Plates []string `json:"plates"`
	}
	var got struct {
		Stops []stop `json:"stops"`
	}
	runFixture(t, "limitsfold.html", &got)
	if len(got.Stops) != 5 {
		t.Fatalf("the fixture read %d stops: %+v", len(got.Stops), got.Stops)
	}
	open, seen, past, near, top := got.Stops[0], got.Stops[1], got.Stops[2], got.Stops[3], got.Stops[4]
	for _, s := range []stop{open, top} {
		if s.Tucked || len(s.Plates) != 3 {
			t.Errorf("at the top of the list the limits read %+v, expected them open", s)
		}
	}
	if seen.Tucked {
		t.Errorf("the limits fold while a pixel of them is still in sight (%+v): the cards under them jump", seen)
	}
	if !past.Tucked {
		t.Errorf("past the limits they read %+v, expected them folded", past)
	}
	if want := "Claude 99.5% 5h|Claude 88% 7d|Codex 44% 7d"; strings.Join(past.Plates, "|") != want {
		t.Errorf("folded, the windows read %q, expected %q", strings.Join(past.Plates, "|"), want)
	}
	if !near.Tucked {
		t.Errorf("a pixel short of the top the limits read %+v, expected them still folded: they open at the "+
			"very top only", near)
	}
}
