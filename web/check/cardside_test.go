package check

import (
	"testing"
)

// A live card of the phone says its session in the words of its settings: the
// name, the state, where it lives, Remote Control in its one place, how full
// it is and the step of its checklist, whole in its tip.
func TestThePhoneCardSaysItsStateAndItsPlace(t *testing.T) {
	type card struct {
		Name    string `json:"name"`
		State   string `json:"state"`
		RC      bool   `json:"rc"`
		RCWords int    `json:"rcWords"`
		Mark    string `json:"mark"`
		Pct     string `json:"pct"`
		Step    string `json:"step"`
		StepTip string `json:"stepTip"`
	}
	var got struct {
		Cards map[string]card `json:"cards"`
	}
	runFixture(t, "cardside.html", &got)
	const long = "blog-fingerprint-rotation-second-pass"
	want := map[string]struct {
		state, mark, pct, step string
		rc                     bool
	}{
		"aacpanel":       {"working", "stream", "78%", "the owner takes the codex screen and the sheet of a thread, both on the phone", true},
		"atlas":          {"commands running", "stream", "64%", "fetch the rest and check that Jellyfin sees it", true},
		"blog":           {"commands running", "tmux", "9%", "", false},
		long:             {"idle", "stream", "22%", "", false},
		"shop":           {"idle", "stream", "31%", "", false},
		"codex-5afc361b": {"idle", "daemon", "15%", "", false},
	}
	if len(got.Cards) != len(want) {
		t.Fatalf("%d cards, wanted %d: %+v", len(got.Cards), len(want), got.Cards)
	}
	for name, w := range want {
		c, ok := got.Cards[name]
		if !ok {
			t.Errorf("no card of %s", name)
			continue
		}
		if c.Name != name {
			t.Errorf("%s: the card opens with the name %q", name, c.Name)
		}
		if c.State != w.state {
			t.Errorf("%s: the state reads %q, expected %q", name, c.State, w.state)
		}
		if c.RC != w.rc || c.RCWords != 1 {
			t.Errorf("%s: Remote Control is on %v, said %d times, expected %v in its one place", name, c.RC, c.RCWords, w.rc)
		}
		if c.Mark != w.mark || c.Pct != w.pct {
			t.Errorf("%s: it lives %q and is %q full, expected %q and %q", name, c.Mark, c.Pct, w.mark, w.pct)
		}
		if c.Step != w.step || c.StepTip != w.step {
			t.Errorf("%s: the step reads %q (tip %q), expected %q, whole in its tip", name, c.Step, c.StepTip, w.step)
		}
	}
}
