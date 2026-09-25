package check

import (
	"strings"
	"testing"
)

// A session on the stream is marked on its card on the phone, right after the
// name, and the percentage stays at the edge; a console session carries no
// mark.
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
		if stream && (c.Mark != "stream" || !c.Shown) {
			t.Errorf("%s: the stream session has no visible mark on the phone: %+v", c.Name, c)
		}
		if !stream && c.Mark != "" {
			t.Errorf("%s: a console session is marked %q", c.Name, c.Mark)
		}
		if stream && (c.Gap < 0 || c.Gap > 16) {
			t.Errorf("%s: the mark stands %dpx from the name — it floats in the middle of the line", c.Name, c.Gap)
		}
		if !c.PctInside || c.PctRight > 1 {
			t.Errorf("%s: the percentage left the edge of the line (%dpx short): %+v", c.Name, c.PctRight, c)
		}
		if c.Overflow > 0 {
			t.Errorf("the page scrolls sideways by %dpx", c.Overflow)
		}
	}
}
