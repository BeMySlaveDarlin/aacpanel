package check

import "testing"

// Four controls stand in the header of a conversation on a phone, in two rows
// flush right: the files of the project and the pair that switches the view,
// then the window on the host and Remote Control. The pair carries a frame of
// its own, and a lone glyph beside it reads as a mark, not as something to
// press — so each button alone wears the frame and the height of the pair.
func TestTheLoneHeaderButtonsLookLikeButtons(t *testing.T) {
	var got struct {
		Files  *headButton `json:"files"`
		Pair   *headButton `json:"pair"`
		Window *headButton `json:"window"`
		Remote *headButton `json:"remote"`
	}
	runFixture(t, "headbuttons.html", &got)

	if got.Files == nil || got.Pair == nil || got.Window == nil || got.Remote == nil {
		t.Fatalf("the header did not draw all four controls: %+v", got)
	}
	if got.Window.Top-got.Pair.Top < got.Pair.Height {
		t.Errorf("the window stands at %v, the pair at %v: the tools of the host belong in a row of their own",
			got.Window.Top, got.Pair.Top)
	}
	if diff := got.Remote.Right - got.Pair.Right; diff > 1 || diff < -1 {
		t.Errorf("the rows end at %v and %v: they stand flush right", got.Pair.Right, got.Remote.Right)
	}
	for _, c := range []struct {
		name string
		it   *headButton
		row  *headButton
	}{
		{"the files of the project", got.Files, got.Pair},
		{"the window on the host", got.Window, got.Remote},
		{"remote control", got.Remote, got.Window},
	} {
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
		if diff := c.it.Top - c.row.Top; diff > 1 || diff < -1 {
			t.Errorf("%s sits %v from the top against %v of its row — the row does not line up",
				c.name, c.it.Top, c.row.Top)
		}
	}
}

type headButton struct {
	Height        float64 `json:"height"`
	Width         float64 `json:"width"`
	Top           float64 `json:"top"`
	Right         float64 `json:"right"`
	Background    string  `json:"background"`
	HasBackground bool    `json:"hasBackground"`
	Border        string  `json:"border"`
	HasBorder     bool    `json:"hasBorder"`
}
