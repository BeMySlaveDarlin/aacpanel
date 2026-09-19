package check

import "testing"

// Three controls stand in the header of a conversation: the files of the
// project, the pair that switches the view, and the window on the host. The
// pair carries a frame of its own, and the two beside it used to carry none —
// a lone glyph on a header reads as a mark, not as something to press.
func TestTheLoneHeaderButtonsLookLikeButtons(t *testing.T) {
	var got struct {
		Files  *headButton `json:"files"`
		Pair   *headButton `json:"pair"`
		Window *headButton `json:"window"`
	}
	runFixture(t, "headbuttons.html", &got)

	if got.Files == nil || got.Pair == nil || got.Window == nil {
		t.Fatalf("the header did not draw all three controls: %+v", got)
	}
	for _, c := range []struct {
		name string
		it   *headButton
	}{{"the files of the project", got.Files}, {"the window on the host", got.Window}} {
		if !c.it.HasBackground {
			t.Errorf("%s has no background of its own (%s): beside a framed pair it reads as a mark on "+
				"the header rather than as a button", c.name, c.it.Background)
		}
		if !c.it.HasBorder {
			t.Errorf("%s has no edge of its own: the frame is what tells a control from a glyph", c.name)
		}
		if diff := c.it.Height - got.Pair.Height; diff > 1 || diff < -1 {
			t.Errorf("%s is %v tall against %v of the pair beside it — the row does not stand level",
				c.name, c.it.Height, got.Pair.Height)
		}
		if diff := c.it.Top - got.Pair.Top; diff > 1 || diff < -1 {
			t.Errorf("%s sits %v from the top against %v of the pair — the row does not line up",
				c.name, c.it.Top, got.Pair.Top)
		}
	}
}

type headButton struct {
	Height        float64 `json:"height"`
	Width         float64 `json:"width"`
	Top           float64 `json:"top"`
	Background    string  `json:"background"`
	HasBackground bool    `json:"hasBackground"`
	Border        string  `json:"border"`
	HasBorder     bool    `json:"hasBorder"`
}
