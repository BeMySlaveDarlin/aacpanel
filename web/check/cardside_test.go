package check

import (
	"testing"
)

// The controls keep the top right corner, on the line of the name, and the
// name wraps beside them rather than yielding its end. The state reads whole
// on its line; the step of the checklist wraps whole, clear of n/N; where the
// session lives and Remote Control stand in their places on the line of its
// settings, and how full it is at the right edge — on both widths of a phone.
func TestThePhoneCardKeepsItsActionsAboveItsPlace(t *testing.T) {
	type card struct {
		Name          string  `json:"name"`
		NameLines     int     `json:"nameLines"`
		NameCut       bool    `json:"nameCut"`
		NameClear     bool    `json:"nameClear"`
		ActionsRight  float64 `json:"actionsRight"`
		ActionsOnLine bool    `json:"actionsOnLine"`
		State         string  `json:"state"`
		StateLines    int     `json:"stateLines"`
		RC            bool    `json:"rc"`
		RCWords       int     `json:"rcWords"`
		Mark          string  `json:"mark"`
		Pct           string  `json:"pct"`
		PctRight      float64 `json:"pctRight"`
		Step          string  `json:"step"`
		StepCut       bool    `json:"stepCut"`
		StepClear     bool    `json:"stepClear"`
		AgentLines    int     `json:"agentLines"`
	}
	var views map[string]struct {
		Cards    map[string]card `json:"cards"`
		Overflow int             `json:"overflow"`
	}
	runFixture(t, "cardside.html", &views)
	const long = "blog-fingerprint-rotation-second-pass"
	want := map[string]struct {
		state, mark, pct, step string
		rc                     bool
	}{
		"aacpanel":       {"working", "stream", "78%", "the owner takes the codex screen and the sheet of a thread, both on the phone", true},
		"atlas":          {"commands running", "stream", "64%", "fetch the rest and check that Jellyfin sees it", true},
		"blog":           {"commands running", "tmux", "9%", "no checklist", false},
		long:             {"idle", "stream", "22%", "no checklist", false},
		"shop":           {"idle", "stream", "31%", "no checklist", false},
		"codex-5afc361b": {"idle", "daemon", "15%", "no checklist", false},
	}
	if len(views) != 2 {
		t.Fatalf("expected both phone widths, got %v", views)
	}
	for _, width := range []string{"390", "360"} {
		got := views[width]
		t.Run(width, func(t *testing.T) {
			if len(got.Cards) != len(want) {
				t.Fatalf("%d cards, wanted %d: %+v", len(got.Cards), len(want), got.Cards)
			}
			for name, w := range want {
				c, ok := got.Cards[name]
				if !ok {
					t.Errorf("no card of %s", name)
					continue
				}
				if c.Name != name || c.NameCut || (name != long && c.NameLines != 1) {
					t.Errorf("%s: the card opens with the name %q on %d lines (cut %v) — whole, on one line unless it is too long for it",
						name, c.Name, c.NameLines, c.NameCut)
				}
				if !c.NameClear || !c.ActionsOnLine || c.ActionsRight < -0.5 || c.ActionsRight > 0.5 {
					t.Errorf("%s: the controls do not keep the top right corner (clear of the name %v, on its line %v, %.1fpx off the edge)",
						name, c.NameClear, c.ActionsOnLine, c.ActionsRight)
				}
				if c.State != w.state || c.StateLines != 1 {
					t.Errorf("%s: the state reads %q on %d lines, expected %q on one", name, c.State, c.StateLines, w.state)
				}
				if c.RC != w.rc || c.RCWords != 1 {
					t.Errorf("%s: Remote Control is on %v, said %d times, expected %v in its one place", name, c.RC, c.RCWords, w.rc)
				}
				if c.Mark != w.mark || c.Pct != w.pct || c.PctRight < -0.5 || c.PctRight > 0.5 {
					t.Errorf("%s: it lives %q and is %q full, %.1fpx short of the edge — expected %q and %q at the edge",
						name, c.Mark, c.Pct, c.PctRight, w.mark, w.pct)
				}
				if c.Step != w.step || c.StepCut || !c.StepClear {
					t.Errorf("%s: the step reads %q (cut %v, clear of n/N %v), expected %q whole", name, c.Step, c.StepCut, c.StepClear, w.step)
				}
				if c.AgentLines != 1 {
					t.Errorf("%s: who runs the session wraps to %d lines", name, c.AgentLines)
				}
			}
			if got.Overflow > 0 {
				t.Errorf("the list pushes the phone %dpx sideways", got.Overflow)
			}
		})
	}
}
