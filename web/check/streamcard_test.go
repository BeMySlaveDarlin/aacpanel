package check

import (
	"strings"
	"testing"
)

// On the phone every live session says where it lives on its row — the feed or
// the console — right before its percentage, and the percentage stands just
// before the button of what can be done to it, whatever the length of the name.
func TestAStreamSessionIsMarkedOnItsCard(t *testing.T) {
	var got []struct {
		Name      string `json:"name"`
		Mark      string `json:"mark"`
		Shown     bool   `json:"shown"`
		Gap       int    `json:"gap"`
		PctInside bool   `json:"pctInside"`
		PctRight  int    `json:"pctRight"`
		Overflow  int    `json:"overflow"`
	}
	runFixture(t, "streamcard.html", &got)
	if len(got) != 3 {
		t.Fatalf("%d cards, wanted 3: %+v", len(got), got)
	}
	for _, c := range got {
		stream := !strings.HasSuffix(c.Name, "aacpanel")
		want := map[bool]string{true: "feed", false: "console"}[stream]
		if c.Mark != want || !c.Shown {
			t.Errorf("%s: the row does not say %q where it can be seen: %+v", c.Name, want, c)
		}
		if c.Gap < 0 || c.Gap > 16 {
			t.Errorf("%s: the mark stands %dpx from the percentage — it floats in the middle of the row", c.Name, c.Gap)
		}
		if !c.PctInside || c.PctRight > 16 {
			t.Errorf("%s: the percentage left its place before the actions button (%dpx short): %+v", c.Name, c.PctRight, c)
		}
		if c.Overflow > 0 {
			t.Errorf("the page scrolls sideways by %dpx", c.Overflow)
		}
	}
}
