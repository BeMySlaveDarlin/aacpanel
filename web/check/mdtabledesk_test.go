package check

import "testing"

// At a desk a table wider than the feed and taller than the screen scrolls
// inside a plate that stops short of the screen: the bar that scrolls it
// sideways stands at the bottom of the plate, in sight wherever the table is
// read, and the head of the table stays over the rows scrolled under it.
func TestADeskTableIsScrolledSidewaysFromAnywhere(t *testing.T) {
	var got struct {
		View    float64 `json:"view"`
		BoxH    float64 `json:"boxH"`
		Wide    bool    `json:"wide"`
		Tall    bool    `json:"tall"`
		HeadTop float64 `json:"headTop"`
	}
	runWideFixture(t, "mdtabledesk.html", &got)
	if !got.Wide {
		t.Fatalf("the table fits the feed: the fixture tests nothing")
	}
	if got.BoxH > got.View*0.75 || !got.Tall {
		t.Errorf("the plate is %.0f tall on a screen of %.0f, scrolling inside it: %v — the bar to scroll it sideways is out of sight",
			got.BoxH, got.View, got.Tall)
	}
	if got.HeadTop < -1 || got.HeadTop > 1 {
		t.Errorf("scrolled down inside, the head of the table stands %.0f from the top of the plate", got.HeadTop)
	}
}
