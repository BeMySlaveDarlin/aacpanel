package check

import "testing"

type badgeShot struct {
	Badges []struct {
		Kind string `json:"kind"`
	} `json:"badges"`
	InColumn int `json:"inColumn"`
}

// Every kind of work on the timeline has a badge there, not in the column of
// prose — a kind of call, thinking, the end of a turn. The kind of a published
// page is one more kind among them, whatever a card of a page looks like.
func TestEveryKindOfWorkHasABadgeOnTheTimeline(t *testing.T) {
	var got badgeShot
	runFixture(t, "toolbadges.html", &got)
	if len(got.Badges) < 12 {
		t.Fatalf("marks drawn: %+v", got.Badges)
	}
	if got.InColumn != 0 {
		t.Errorf("%d badges stand in the column of prose: the work is on the timeline", got.InColumn)
	}
	kinds := map[string]bool{}
	for _, b := range got.Badges {
		kinds[b.Kind] = true
	}
	for _, want := range []string{"artifact", "hook", "think", "turn"} {
		if !kinds[want] {
			t.Errorf("no %s mark among %v", want, kinds)
		}
	}
}

func TestThePhoneRailWaitsForTheCurrentRun(t *testing.T) {
	type run struct {
		Tail, Anchored, Now, Agents bool
	}
	var got struct{ Running, Ended, WaitingAgents, Replied run }
	runFixture(t, "toolbadges.html", &got)
	if g := got.Running; g.Tail || !g.Anchored || !g.Now {
		t.Errorf("running turn: %+v — its tail belongs to the now bar, and anchored work stays", g)
	}
	if g := got.Ended; !g.Tail || g.Now {
		t.Errorf("ended turn: %+v — its tail belongs on the rail", g)
	}
	if g := got.WaitingAgents; !g.Tail || !g.Agents || g.Now {
		t.Errorf("turn waiting on agents: %+v — the run is over and the bar counts the agents", g)
	}
	if g := got.Replied; !g.Tail || !g.Anchored || !g.Now {
		t.Errorf("running turn after its reply: %+v — work anchored to a reply stays on the rail", g)
	}
}
