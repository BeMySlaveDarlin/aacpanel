package check

import (
	"strings"
	"testing"
)

// On the phone every live session says where it lives on its row — the feed or
// the console — right before its percentage, and the percentage stands just
// before the button of what can be done to it, whatever the length of the name.
// A session with Remote Control up says so before where it lives, and only it.
// A tap anywhere on the row opens the conversation, but on the row's button of
// what can be done to it, which opens that. The state stands whole, and when
// and what else on a line of its own under it, not cut to fit one line.
func TestAStreamSessionIsMarkedOnItsCard(t *testing.T) {
	var got []struct {
		Name       string `json:"name"`
		Mark       string `json:"mark"`
		State      string `json:"state"`
		StateCut   bool   `json:"stateCut"`
		Since      string `json:"since"`
		SinceCut   bool   `json:"sinceCut"`
		SinceBelow bool   `json:"sinceBelow"`
		TapPct     string `json:"tapPct"`
		TapTag     string `json:"tapTag"`
		TapMore    string `json:"tapMore"`
		RC         string `json:"rc"`
		RCShown    bool   `json:"rcShown"`
		Shown      bool   `json:"shown"`
		Gap        int    `json:"gap"`
		PctInside  bool   `json:"pctInside"`
		PctRight   int    `json:"pctRight"`
		Overflow   int    `json:"overflow"`
	}
	runFixture(t, "streamcard.html", &got)
	if len(got) != 3 {
		t.Fatalf("%d cards, wanted 3: %+v", len(got), got)
	}
	for _, c := range got {
		stream := !strings.HasSuffix(c.Name, "aacpanel")
		want := map[bool]string{true: "stream", false: "tmux"}[stream]
		if c.Mark != want || !c.Shown {
			t.Errorf("%s: the row does not say %q where it can be seen: %+v", c.Name, want, c)
		}
		if c.Gap < 0 || c.Gap > 16 {
			t.Errorf("%s: the mark stands %dpx from the percentage — it floats in the middle of the row", c.Name, c.Gap)
		}
		if !c.PctInside || c.PctRight > 16 {
			t.Errorf("%s: the percentage left its place before the actions button (%dpx short): %+v", c.Name, c.PctRight, c)
		}
		session := c.Name
		if !strings.HasPrefix(session, "acme") && session != "aacpanel" && session != "person" {
			t.Fatalf("an unknown block %q", session)
		}
		if c.TapPct != "open:"+session || c.TapTag != "open:"+session {
			t.Errorf("%s: a tap on the percentage opens %q and on where it lives %q — the whole row opens the conversation", session, c.TapPct, c.TapTag)
		}
		if c.TapMore != "more:"+session {
			t.Errorf("%s: a tap on the button of what can be done does %q", session, c.TapMore)
		}
		wantState, wantSince := "idle", "2 min ago"
		switch session {
		case "aacpanel":
			wantSince = "2 min ago · 1 background task"
		case "person":
			wantState, wantSince = "no requests yet", ""
		}
		if c.State != wantState || c.StateCut {
			t.Errorf("%s: the state reads %q (cut %v), expected %q whole", session, c.State, c.StateCut, wantState)
		}
		if c.Since != wantSince || (wantSince != "" && (c.SinceCut || !c.SinceBelow)) {
			t.Errorf("%s: under the state stands %q (cut %v, on a line of its own %v), expected %q", session, c.Since, c.SinceCut, c.SinceBelow, wantSince)
		}
		remote := strings.HasPrefix(c.Name, "acme")
		if remote && (c.RC != "RC" || !c.RCShown) {
			t.Errorf("%s: Remote Control is up and the row does not say so before where it lives: %+v", c.Name, c)
		}
		if !remote && c.RC != "" {
			t.Errorf("%s: the row says Remote Control is up, and it is not", c.Name)
		}
		if c.Overflow > 0 {
			t.Errorf("the page scrolls sideways by %dpx", c.Overflow)
		}
	}
}
