package check

import (
	"os"
	"testing"
)

// A conversation opened from the phone's list and closed again brings the
// list back where it was left, not to its top: the card pressed stands where
// it stood, the limits folded over the list as they were. On a pager of
// contours the list scrolls in its page, with a single contour in the body;
// both keep their place.
func TestThePhoneListKeepsItsPlaceAcrossAConversation(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type place struct {
		Scroller  string `json:"scroller"`
		ScrollTop int    `json:"scrollTop"`
		Card      int    `json:"card"`
		Tucked    bool   `json:"tucked"`
	}
	type trip struct {
		Label    string `json:"label"`
		Left     place  `json:"left"`
		Opened   bool   `json:"opened"`
		Returned bool   `json:"returned"`
		Settled  bool   `json:"settled"`
		Back     place  `json:"back"`
	}
	var got map[string]trip
	runFixture(t, "listscroll.html", &got)
	for _, name := range []string{"pager", "body"} {
		r, ok := got[name]
		if !ok {
			t.Fatalf("no trip on the %s: %+v", name, got)
		}
		if !r.Opened || !r.Returned || !r.Settled {
			t.Fatalf("%s: the conversation opened %v, the list came back %v, the history let go of the conversation %v",
				name, r.Opened, r.Returned, r.Settled)
		}
		if r.Left.ScrollTop < 300 || !r.Left.Tucked {
			t.Fatalf("%s: the list was not scrolled past its limits before the conversation: %+v", name, r.Left)
		}
		if r.Back.Scroller != r.Left.Scroller {
			t.Errorf("%s: the list scrolled in %q and came back in %q", name, r.Left.Scroller, r.Back.Scroller)
		}
		if r.Back.ScrollTop != r.Left.ScrollTop || r.Back.Card != r.Left.Card || r.Back.Tucked != r.Left.Tucked {
			t.Errorf("%s: %s stood %d px down the list scrolled to %d, and came back to %+v", name, r.Label, r.Left.Card, r.Left.ScrollTop, r.Back)
		}
	}
}
