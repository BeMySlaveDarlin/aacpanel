package check

import "testing"

// The contour pages of the phone are turned by their own sideways scroll, and
// a scroll is caught only over the box that scrolls. When every contour is
// short the lists end high up, and the room between the last row and the
// bottom menu has to belong to the pages: a swipe begun there is the same
// swipe as one begun on the list. The pages reaching the menu must not cost
// the long contour its scroll down to the last row, nor a row its tap.
func TestASwipeUnderAShortListTurnsTheContourPage(t *testing.T) {
	type point struct {
		Y        float64 `json:"y"`
		Tap      string  `json:"tap"`
		Under    string  `json:"under"`
		Sideways string  `json:"sideways"`
	}
	type turn struct {
		Box  string `json:"box"`
		Page string `json:"page"`
	}
	var got struct {
		Error         string  `json:"error"`
		Start         string  `json:"start"`
		LastRowBottom float64 `json:"lastRowBottom"`
		NavTop        float64 `json:"navTop"`
		Row           point   `json:"row"`
		Empty         []point `json:"empty"`
		Forward       turn    `json:"forward"`
		Back          turn    `json:"back"`
		Down          string  `json:"down"`
		TallBefore    float64 `json:"tallBefore"`
		PagesMoved    float64 `json:"pagesMoved"`
		TallEnd       struct {
			Bottom float64 `json:"bottom"`
			NavTop float64 `json:"navTop"`
			Tap    string  `json:"tap"`
		} `json:"tallEnd"`
	}
	runFixture(t, "pagerswipe.html", &got)

	if got.Error != "" {
		t.Fatalf("the fixture failed: %s", got.Error)
	}
	if got.Start != "Acme" {
		t.Fatalf("the phone opened on %q, not on the contour it was left on", got.Start)
	}
	if room := got.NavTop - got.LastRowBottom; room < 200 {
		t.Fatalf("only %.0f px between the last row and the menu: there is no empty room to swipe in", room)
	}

	if got.Row.Tap != "contour settings" {
		t.Errorf("a tap on the last row at y=%.0f reaches %q, not the row", got.Row.Y, got.Row.Tap)
	}
	if got.Row.Sideways != "pfpages" {
		t.Errorf("a swipe on the list at y=%.0f moves %q, not the pages", got.Row.Y, got.Row.Sideways)
	}

	if len(got.Empty) != 2 {
		t.Fatalf("%d points probed in the empty room, expected two", len(got.Empty))
	}
	for _, p := range got.Empty {
		if p.Tap != "" {
			t.Fatalf("the point at y=%.0f lies on %q, not in the empty room under the list", p.Y, p.Tap)
		}
		if p.Sideways != "pfpages" {
			t.Errorf("a swipe begun at y=%.0f in the empty room under the list lands on %q, "+
				"which does not scroll sideways, and the contour page stays where it is", p.Y, p.Under)
		}
	}
	if got.Forward.Page != "globex" {
		t.Errorf("a swipe from the middle of the empty room left the phone on %q, expected the next contour", got.Forward.Page)
	}
	if got.Back.Page != "Acme" {
		t.Errorf("a swipe from just above the menu left the phone on %q, expected the contour before", got.Back.Page)
	}

	if got.Down != "pfpage" {
		t.Errorf("a pull up on the long contour moves %q, not its page", got.Down)
	}
	if got.TallBefore <= got.TallEnd.NavTop {
		t.Fatalf("the long contour ends at %.0f, above the menu at %.0f: there is nothing to scroll",
			got.TallBefore, got.TallEnd.NavTop)
	}
	if got.PagesMoved != 0 {
		t.Errorf("scrolling the long contour down moved the pages sideways by %v", got.PagesMoved)
	}
	if got.TallEnd.Bottom > got.TallEnd.NavTop {
		t.Errorf("scrolled to its end, the long contour's last row still ends at %.0f, under the menu at %.0f",
			got.TallEnd.Bottom, got.TallEnd.NavTop)
	}
	if got.TallEnd.Tap != "contour settings" {
		t.Errorf("a tap on the long contour's last row reaches %q, not the row", got.TallEnd.Tap)
	}
}
