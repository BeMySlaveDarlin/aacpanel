package check

import (
	"strings"
	"testing"
)

// The right side of a live card on a phone is a narrow column of three things
// and nothing more. Remote Control is not a mark of it but a word on the
// state's line after how long the session has lived, with no plate and no
// fill under it. Where the session lives and the button of what can be done
// to it stand side by side level with the state and look one kind: the same
// height, edge, corners, type and padding. How full the session is stands in
// the bottom right corner of the row, under the pair and level with the last
// line on the left. The column touches none of the lines on the left, the
// checklist cut at its end included, and a row of a name keeps the pair on the
// state's line rather than on the name's.
func TestTheRightSideOfALiveCardIsAPairAndAFigure(t *testing.T) {
	type look struct {
		Height float64 `json:"height"`
		Border string  `json:"border"`
		Radius string  `json:"radius"`
		Font   string  `json:"font"`
		Pad    string  `json:"pad"`
		Ink    string  `json:"ink"`
		Fill   string  `json:"fill"`
	}
	type card struct {
		Named      bool   `json:"named"`
		StateReads string `json:"stateReads"`
		StateLines int    `json:"stateLines"`
		RC         *struct {
			look
			InState    bool `json:"inState"`
			OnLine     bool `json:"onLine"`
			AfterLived bool `json:"afterLived"`
		} `json:"rc"`
		RCPlates       int     `json:"rcPlates"`
		RCInSide       bool    `json:"rcInSide"`
		Mark           string  `json:"mark"`
		Tag            look    `json:"tag"`
		More           look    `json:"more"`
		PairLevel      float64 `json:"pairLevel"`
		PairGap        float64 `json:"pairGap"`
		PairOff        float64 `json:"pairOff"`
		PairRight      float64 `json:"pairRight"`
		Pct            string  `json:"pct"`
		PctRight       float64 `json:"pctRight"`
		PctBottom      float64 `json:"pctBottom"`
		PctUnder       float64 `json:"pctUnder"`
		PctLevel       float64 `json:"pctLevel"`
		Last           string  `json:"last"`
		LeftGives      float64 `json:"leftGives"`
		Clear          bool    `json:"clear"`
		AddsHeight     float64 `json:"addsHeight"`
		Checklist      string  `json:"checklist"`
		ChecklistLines int     `json:"checklistLines"`
		ChecklistCut   bool    `json:"checklistCut"`
		ChecklistClear bool    `json:"checklistClear"`
		SinceLines     int     `json:"sinceLines"`
	}
	var got struct {
		Cards    map[string]card `json:"cards"`
		Overflow int             `json:"overflow"`
	}
	runFixture(t, "cardside.html", &got)
	want := map[string]struct {
		state, mark, pct, last string
		named, long            bool
	}{
		"aacpanel":       {"idle · 15 h 40 m · RC", "stream", "78%", "pjsince", false, true},
		"atlas":          {"idle · 4 d 20 h · RC", "stream", "64%", "pjwork", false, true},
		"blog":           {"idle · 3 h 0 m", "tmux", "9%", "pjwork", false, false},
		"shop":           {"idle · 1 h 30 m", "stream", "31%", "pjsince", true, false},
		"codex-5afc361b": {"idle · 42 min", "daemon", "15%", "pjsince", true, false},
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
		remote := strings.HasSuffix(w.state, " · RC")

		// Remote Control: a word on the state's line, after the time.
		if c.StateReads != w.state || c.StateLines != 1 {
			t.Errorf("%s: the state's line reads %q on %d lines, expected %q on one", name, c.StateReads, c.StateLines, w.state)
		}
		if remote {
			if c.RC == nil || !c.RC.InState || !c.RC.OnLine || !c.RC.AfterLived {
				t.Errorf("%s: Remote Control is not a word on the state's line after the time: %+v", name, c.RC)
			} else if c.RC.Fill != "rgba(0, 0, 0, 0) none" || !strings.HasPrefix(c.RC.Border, "0px") || c.RC.Pad != "0px 0px 0px 0px" {
				t.Errorf("%s: the word of Remote Control stands on a plate (fill %q, edge %q, padding %q) — it is a word like the time",
					name, c.RC.Fill, c.RC.Border, c.RC.Pad)
			}
		} else if c.RC != nil {
			t.Errorf("%s: the state's line says Remote Control is up, and it is not", name)
		}
		if c.RCPlates != 0 || c.RCInSide {
			t.Errorf("%s: the right column still carries a mark of Remote Control (%d plates, in the column %v)", name, c.RCPlates, c.RCInSide)
		}

		// Where it lives and the button: a pair of one kind, level with the state.
		if c.Mark != w.mark {
			t.Errorf("%s: the row says it lives in %q, expected %q", name, c.Mark, w.mark)
		}
		if c.Tag.Height <= 0 || strings.HasPrefix(c.Tag.Border, "0px") {
			t.Errorf("%s: where the session lives is not a mark with an edge: %+v", name, c.Tag)
		}
		if c.Tag != c.More {
			t.Errorf("%s: where the session lives and the button of what can be done are drawn apart:\n place  %+v\n button %+v", name, c.Tag, c.More)
		}
		if c.PairLevel > 0.5 || c.PairGap < 0 || c.PairGap > 8 || c.PairRight > 0.5 || c.PairRight < -0.5 {
			t.Errorf("%s: the place and the button are not side by side at the right edge (off each other %.1fpx, %.1fpx apart, %.1fpx short of the edge)",
				name, c.PairLevel, c.PairGap, c.PairRight)
		}
		if c.PairOff > 2 {
			t.Errorf("%s: the pair stands %.1fpx off the state's line (a name over the row %v)", name, c.PairOff, c.Named)
		}

		// How full it is: the bottom right corner, under the pair.
		if c.Pct != w.pct {
			t.Errorf("%s: the figure reads %q, expected %q", name, c.Pct, w.pct)
		}
		if c.PctRight < -0.5 || c.PctRight > 0.5 || c.PctBottom < -0.5 || c.PctBottom > 0.5 {
			t.Errorf("%s: how full the session is stands %.1fpx from the right and %.1fpx from the foot of the row — not in its corner",
				name, c.PctRight, c.PctBottom)
		}
		if c.PctUnder < 0 || c.PctLevel > 1 || c.Last != w.last {
			t.Errorf("%s: the figure stands %.1fpx under the pair and %.1fpx off the last line on the left (%q, expected %q)",
				name, c.PctUnder, c.PctLevel, c.Last, w.last)
		}

		// Nothing of the column lies on the lines on the left.
		if !c.Clear || c.LeftGives < 0 || c.LeftGives > 10 {
			t.Errorf("%s: the column meets the lines on the left (clear %v, the lines stop %.1fpx short of it)", name, c.Clear, c.LeftGives)
		}
		if c.AddsHeight > 1 {
			t.Errorf("%s: the column runs %.1fpx past the lines on the left — it makes the row taller", name, c.AddsHeight)
		}
		if w.long && (c.Checklist == "" || !c.ChecklistClear || c.ChecklistLines != 1 || !c.ChecklistCut) {
			t.Errorf("%s: the checklist %q is not one cut line clear of the column (clear %v, %d lines, cut %v)",
				name, c.Checklist, c.ChecklistClear, c.ChecklistLines, c.ChecklistCut)
		}
		if c.SinceLines != 1 {
			t.Errorf("%s: who runs the session wraps to %d lines", name, c.SinceLines)
		}
		if c.Named != w.named {
			t.Errorf("%s: the row carries its name %v, expected %v", name, c.Named, w.named)
		}
	}
	if got.Overflow > 0 {
		t.Errorf("the list pushes the phone %dpx sideways", got.Overflow)
	}
}
