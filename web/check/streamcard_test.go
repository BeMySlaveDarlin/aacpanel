package check

import (
	"strings"
	"testing"
)

type streamCardBell struct {
	Title  string `json:"title"`
	Icon   bool   `json:"icon"`
	Inside bool   `json:"inside"`
}

type streamCardRun struct {
	Cards []struct {
		Name          string `json:"name"`
		Mark          string `json:"mark"`
		State         string `json:"state"`
		StateCut      bool   `json:"stateCut"`
		Since         string `json:"since"`
		Agent         string `json:"agent"`
		AgentKey      string `json:"agentKey"`
		SinceCut      bool   `json:"sinceCut"`
		SinceLines    int    `json:"sinceLines"`
		SinceBelow    bool   `json:"sinceBelow"`
		Work          string `json:"work"`
		NameStands    bool   `json:"nameStands"`
		NameLines     int    `json:"nameLines"`
		NameBeside    bool   `json:"nameBeside"`
		WorkBelow     bool   `json:"workBelow"`
		TapPct        string `json:"tapPct"`
		TapTag        string `json:"tapTag"`
		TapRC         string `json:"tapRC"`
		RC            string `json:"rc"`
		RCElsewhere   bool   `json:"rcElsewhere"`
		Shown         bool   `json:"shown"`
		LeftGives     int    `json:"leftGives"`
		MarksInside   bool   `json:"marksInside"`
		PctAddsHeight int    `json:"pctAddsHeight"`
		Overflow      int    `json:"overflow"`
	} `json:"cards"`
	Called struct {
		Before  map[string]streamCardBell `json:"before"`
		Visited map[string]streamCardBell `json:"visited"`
		Again   map[string]streamCardBell `json:"again"`
	} `json:"called"`
}

// The phone's card keeps the name, place, state, model and background work
// readable below its name and actions. Tapping the place, percentage or Remote
// Control opens the conversation just as tapping the words does.
func TestAStreamSessionIsMarkedOnItsCard(t *testing.T) {
	var run streamCardRun
	runFixture(t, "streamcard.html", &run)
	got := run.Cards
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
		if remote && c.RC != "RC" {
			t.Errorf("%s: Remote Control is up and the state's line does not say so: %+v", session, c)
		}
		if !remote && c.RC != "" {
			t.Errorf("%s: the row says Remote Control is up, and it is not", session)
		}
		if c.RCElsewhere {
			t.Errorf("%s: Remote Control is said somewhere else on the row than the state's line", session)
		}
		if c.LeftGives < 0 || c.LeftGives > 10 {
			t.Errorf("%s: the words on the left stop %dpx short of the figure — they give the right side more than it takes", session, c.LeftGives)
		}
		if !c.MarksInside {
			t.Errorf("%s: the marks spill out of their row: %+v", session, c)
		}
		if c.PctAddsHeight > 1 {
			t.Errorf("%s: the figure runs %dpx past the lines on the left — it makes the row taller", session, c.PctAddsHeight)
		}
		if c.TapPct != "open:"+session || c.TapTag != "open:"+session || (remote && c.TapRC != "open:"+session) {
			t.Errorf("%s: a tap on the percentage opens %q, on where it lives %q, on Remote Control %q — the whole row opens the conversation",
				session, c.TapPct, c.TapTag, c.TapRC)
		}
		wantState, wantSince, wantWork := "idle", "Claude · Opus 5.5 · 2 min ago", ""
		switch {
		case session == "aacpanel":
			wantState, wantWork = "background", "3 background tasks"
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
		if !c.NameBeside || c.NameLines != 1 {
			t.Errorf("%s: the name stands beside the actions and level with them %v, on %d lines — it opens the row and fits one", session, c.NameBeside, c.NameLines)
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

// A session that called the person carries a bell on its card until the
// person opens it: the card of a session that never called has none, the
// device that opened the feed takes the bell off without waiting for the host
// to say so, and a call made after the visit brings it back.
func TestACallMarksTheCardUntilTheSessionIsOpened(t *testing.T) {
	var run streamCardRun
	runFixture(t, "streamcard.html", &run)
	got := run.Called
	bell, ok := got.Before["aacpanel"]
	if len(got.Before) != 1 || !ok {
		t.Fatalf("the bells before the visit stand on %v, wanted the one session that called", got.Before)
	}
	if !bell.Icon || !bell.Inside || !strings.HasPrefix(bell.Title, "1 waiting for you: call: Called you · ") {
		t.Errorf("the bell of the session that called is %+v: a mark inside its row that says when it called", bell)
	}
	if len(got.Visited) != 0 {
		t.Errorf("the feed of the session was opened and closed, and the list still rings: %v", got.Visited)
	}
	if _, ok := got.Again["aacpanel"]; !ok || len(got.Again) != 1 {
		t.Errorf("a call after the visit marks %v, wanted the session that called again", got.Again)
	}
}
