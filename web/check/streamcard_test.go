package check

import (
	"strings"
	"testing"
)

// On the phone every live session says where it lives on its row — the feed or
// the console — in a narrow column on the right: where it lives and the button
// of what can be done to it side by side, how full the session is under them,
// and Remote Control, for a session that has it up, as a word on the state's
// line rather than a mark of the column. The column is as wide as its pair and
// no wider, so the words on the left keep the rest of the row: who runs the
// session on which model and since when stands on one line. The column runs
// no further down than the lines on the left, so it never makes the row
// taller. The name of the session, when the row has one, stands over both
// across the whole width, so a long one does not wrap. A tap anywhere on the
// row opens the conversation, the marks and the word of Remote Control
// included, but on the row's button of what can be done to it — and a little
// past its dots — which opens that. The state stands whole, and under it, on a
// line of its own, who runs the session — the word of its agent first; what
// runs in its background stands on a third line.
func TestAStreamSessionIsMarkedOnItsCard(t *testing.T) {
	var got []struct {
		Name             string `json:"name"`
		Mark             string `json:"mark"`
		State            string `json:"state"`
		StateCut         bool   `json:"stateCut"`
		Since            string `json:"since"`
		Agent            string `json:"agent"`
		AgentKey         string `json:"agentKey"`
		SinceCut         bool   `json:"sinceCut"`
		SinceLines       int    `json:"sinceLines"`
		SinceBelow       bool   `json:"sinceBelow"`
		Work             string `json:"work"`
		NameStands       bool   `json:"nameStands"`
		NameLines        int    `json:"nameLines"`
		NameAcross       bool   `json:"nameAcross"`
		WorkBelow        bool   `json:"workBelow"`
		TapPct           string `json:"tapPct"`
		TapTag           string `json:"tapTag"`
		TapRC            string `json:"tapRC"`
		TapMore          string `json:"tapMore"`
		TapCorner        string `json:"tapCorner"`
		RC               string `json:"rc"`
		RCElsewhere      bool   `json:"rcElsewhere"`
		Shown            bool   `json:"shown"`
		Column           bool   `json:"column"`
		Narrow           int    `json:"narrow"`
		LeftGives        int    `json:"leftGives"`
		ColumnInside     bool   `json:"columnInside"`
		ColumnAddsHeight int    `json:"columnAddsHeight"`
		Overflow         int    `json:"overflow"`
	}
	runFixture(t, "streamcard.html", &got)
	if len(got) != 3 {
		t.Fatalf("%d cards, wanted 3: %+v", len(got), got)
	}
	for _, c := range got {
		session := c.Name
		if !strings.HasPrefix(session, "acme") && session != "aacpanel" && session != "person" {
			t.Fatalf("an unknown block %q", session)
		}
		stream := session != "aacpanel"
		remote := strings.HasPrefix(session, "acme")
		want := map[bool]string{true: "stream", false: "tmux"}[stream]
		if c.Mark != want || !c.Shown {
			t.Errorf("%s: the row does not say %q where it can be seen: %+v", session, want, c)
		}
		if !c.Column {
			t.Fatalf("%s: the row has no column of its marks on the right", session)
		}
		if remote && c.RC != "RC" {
			t.Errorf("%s: Remote Control is up and the state's line does not say so: %+v", session, c)
		}
		if !remote && c.RC != "" {
			t.Errorf("%s: the row says Remote Control is up, and it is not", session)
		}
		if c.RCElsewhere {
			t.Errorf("%s: Remote Control is said somewhere else on the row than the state's line", session)
		}
		if c.Narrow < 0 || c.Narrow > 1 {
			t.Errorf("%s: the column is %dpx wider than its widest line", session, c.Narrow)
		}
		if c.LeftGives < 0 || c.LeftGives > 10 {
			t.Errorf("%s: the words on the left stop %dpx short of the column — they give the column more than it takes", session, c.LeftGives)
		}
		if !c.ColumnInside {
			t.Errorf("%s: the column spills out of its row: %+v", session, c)
		}
		if c.ColumnAddsHeight > 1 {
			t.Errorf("%s: the column runs %dpx past the lines on the left — it makes the row taller", session, c.ColumnAddsHeight)
		}
		if c.TapPct != "open:"+session || c.TapTag != "open:"+session || (remote && c.TapRC != "open:"+session) {
			t.Errorf("%s: a tap on the percentage opens %q, on where it lives %q, on Remote Control %q — the whole row opens the conversation",
				session, c.TapPct, c.TapTag, c.TapRC)
		}
		if c.TapMore != "more:"+session || c.TapCorner != "more:"+session {
			t.Errorf("%s: a tap on the button of what can be done does %q, and just past its dots %q", session, c.TapMore, c.TapCorner)
		}
		wantState, wantSince, wantWork := "idle", "Claude · Opus 5.5 · 2 min ago", ""
		switch {
		case session == "aacpanel":
			wantWork = "1 background task"
		case session == "person":
			wantState, wantSince = "no requests yet", "Claude · Opus 5.5"
		case remote:
			wantSince = "Claude · Opus 5.5 · 18 min ago"
		}
		if c.State != wantState || c.StateCut {
			t.Errorf("%s: the state reads %q (cut %v), expected %q whole", session, c.State, c.StateCut, wantState)
		}
		if c.Since != wantSince || c.SinceCut || !c.SinceBelow || c.SinceLines != 1 {
			t.Errorf("%s: under the state stands %q (cut %v, on a line of its own %v, on %d lines), expected %q on one",
				session, c.Since, c.SinceCut, c.SinceBelow, c.SinceLines, wantSince)
		}
		if !c.NameStands {
			t.Errorf("%s: the name of the session reads as a caption — it is what tells two sessions of one project apart, "+
				"and stands bolder, larger and in another ink than the line under the state", session)
		}
		if !c.NameAcross || c.NameLines != 1 {
			t.Errorf("%s: the name stands across the row %v, on %d lines — it goes over both columns and fits one", session, c.NameAcross, c.NameLines)
		}
		if c.Work != wantWork || (wantWork != "" && !c.WorkBelow) {
			t.Errorf("%s: the line of what runs in the background reads %q (under who runs it %v), expected %q",
				session, c.Work, c.WorkBelow, wantWork)
		}
		if c.Agent != "Claude" || c.AgentKey != "claude" {
			t.Errorf("%s: the line under the state opens with %q painted as %q's, expected the word of its agent, Claude", session, c.Agent, c.AgentKey)
		}
		if c.Overflow > 0 {
			t.Errorf("the page scrolls sideways by %dpx", c.Overflow)
		}
	}
}
