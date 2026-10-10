package check

import (
	"strings"
	"testing"
)

// A live card on a phone opens with the name of its session, the only session
// of its project included, and two marks stand level with the name at the
// right edge: where the session lives and the button of what can be done to
// it, side by side and of one kind — the same height, edge, corners, type and
// padding. A name too long for its line wraps under itself, whole, and never
// runs under the pair. Remote Control is not a mark but a word on the state's
// line after how long the session has lived, with no plate and no fill under
// it. How full the session is stands in the bottom right corner of the row,
// level with the last line on the left, and that figure is all the lines under
// the name give up on the right: the checklist, on one line and cut at its end
// when it is longer, runs past where the pair starts. Nothing lies on anything else, and neither
// the pair nor the figure makes the row taller.
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
		Name       string  `json:"name"`
		NameLines  int     `json:"nameLines"`
		NameCut    bool    `json:"nameCut"`
		NameGives  float64 `json:"nameGives"`
		NameClear  bool    `json:"nameClear"`
		StateReads string  `json:"stateReads"`
		StateLines int     `json:"stateLines"`
		RC         *struct {
			look
			InState    bool `json:"inState"`
			OnLine     bool `json:"onLine"`
			AfterLived bool `json:"afterLived"`
		} `json:"rc"`
		RCPlates          int     `json:"rcPlates"`
		RCInMarks         bool    `json:"rcInMarks"`
		Mark              string  `json:"mark"`
		Tag               look    `json:"tag"`
		More              look    `json:"more"`
		PairLevel         float64 `json:"pairLevel"`
		PairGap           float64 `json:"pairGap"`
		PairOff           float64 `json:"pairOff"`
		PairOverState     float64 `json:"pairOverState"`
		PairRight         float64 `json:"pairRight"`
		PairAddsHeight    float64 `json:"pairAddsHeight"`
		Pct               string  `json:"pct"`
		PctRight          float64 `json:"pctRight"`
		PctBottom         float64 `json:"pctBottom"`
		PctUnder          float64 `json:"pctUnder"`
		PctLevel          float64 `json:"pctLevel"`
		Last              string  `json:"last"`
		LeftGives         float64 `json:"leftGives"`
		Clear             bool    `json:"clear"`
		AddsHeight        float64 `json:"addsHeight"`
		Checklist         string  `json:"checklist"`
		ChecklistLines    int     `json:"checklistLines"`
		ChecklistCut      bool    `json:"checklistCut"`
		ChecklistClear    bool    `json:"checklistClear"`
		ChecklistWidth    float64 `json:"checklistWidth"`
		ChecklistPastPair float64 `json:"checklistPastPair"`
		ChecklistShort    float64 `json:"checklistShort"`
		SinceLines        int     `json:"sinceLines"`
	}
	var got struct {
		Cards    map[string]card `json:"cards"`
		Overflow int             `json:"overflow"`
	}
	runFixture(t, "cardside.html", &got)
	const long = "blog-fingerprint-rotation-second-pass"
	want := map[string]struct {
		state, mark, pct, last string
		nameLines              int
		// How the checklist stands: "cut" at its end, "whole", or "" for none.
		checklist string
	}{
		"aacpanel":       {"idle · 15 h 40 m · RC", "stream", "78%", "pjsince", 1, "cut"},
		"atlas":          {"idle · 4 d 20 h · RC", "stream", "64%", "pjwork", 1, "whole"},
		"blog":           {"idle · 3 h 0 m", "tmux", "9%", "pjwork", 1, ""},
		long:             {"idle · 25 min", "stream", "22%", "pjsince", 2, ""},
		"shop":           {"idle · 1 h 30 m", "stream", "31%", "pjsince", 1, ""},
		"codex-5afc361b": {"idle · 42 min", "daemon", "15%", "pjsince", 1, ""},
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

		// The name: on every card, whole, on its lines beside the pair.
		if c.Name != name {
			t.Errorf("%s: the card opens with the name %q — every live card carries the name of its session", name, c.Name)
		}
		if c.NameLines != w.nameLines || c.NameCut {
			t.Errorf("%s: the name stands on %d lines (cut %v), expected %d whole", name, c.NameLines, c.NameCut, w.nameLines)
		}
		if !c.NameClear || c.NameGives < 0 || c.NameGives > 10 {
			t.Errorf("%s: the name meets the pair (clear %v, it stops %.1fpx short of the place)", name, c.NameClear, c.NameGives)
		}

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
		if c.RCPlates != 0 || c.RCInMarks {
			t.Errorf("%s: the marks on the right still carry Remote Control (%d plates, in a mark %v)", name, c.RCPlates, c.RCInMarks)
		}

		// Where it lives and the button: a pair of one kind, level with the name.
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
		if c.PairOff > 1 || c.PairOverState < 0 {
			t.Errorf("%s: the pair stands %.1fpx off the first line of the name and %.1fpx over the state — it belongs on the name's line",
				name, c.PairOff, c.PairOverState)
		}
		if c.PairAddsHeight > 0.5 {
			t.Errorf("%s: the pair runs %.1fpx under the name — it makes the name's line taller", name, c.PairAddsHeight)
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

		// Nothing on the right lies on the lines on the left.
		if !c.Clear || c.LeftGives < 0 || c.LeftGives > 10 {
			t.Errorf("%s: the marks meet the lines on the left (clear %v, the lines stop %.1fpx short of the figure)", name, c.Clear, c.LeftGives)
		}
		if c.AddsHeight > 1 {
			t.Errorf("%s: the figure runs %.1fpx past the lines on the left — it makes the row taller", name, c.AddsHeight)
		}
		if w.checklist != "" {
			if c.Checklist == "" || !c.ChecklistClear || c.ChecklistLines != 1 || c.ChecklistCut != (w.checklist == "cut") {
				t.Errorf("%s: the checklist %q is not one line clear of the marks, %s (clear %v, %d lines, cut %v)",
					name, c.Checklist, w.checklist, c.ChecklistClear, c.ChecklistLines, c.ChecklistCut)
			}
			// The pair is a good 100px wide and the figure a third of it: a
			// checklist that stops where the pair starts is still sharing its
			// line with the pair.
			if c.ChecklistPastPair < 40 || c.ChecklistShort < -0.5 || c.ChecklistShort > 0.5 {
				t.Errorf("%s: the checklist is %.0fpx wide, runs %.1fpx past where the pair starts and stops %.1fpx short of the figure — "+
					"it should reach the figure, well past the pair", name, c.ChecklistWidth, c.ChecklistPastPair, c.ChecklistShort)
			}
			t.Logf("%s: the checklist is %.0fpx wide, %.0fpx past where the pair starts", name, c.ChecklistWidth, c.ChecklistPastPair)
		}
		if c.SinceLines != 1 {
			t.Errorf("%s: who runs the session wraps to %d lines", name, c.SinceLines)
		}
	}
	if got.Overflow > 0 {
		t.Errorf("the list pushes the phone %dpx sideways", got.Overflow)
	}
}
