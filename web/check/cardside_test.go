package check

import (
	"strings"
	"testing"
)

// A live card keeps its place beside the name, its percentage beside the
// last line of its words and its actions underneath. Long names wrap, the
// checklist uses all the width before the percentage, and Remote Control is
// a word of the state rather than a plate of its own.
func TestTheRightSideOfALiveCardIsAPlaceAndAFigure(t *testing.T) {
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
		RCPlates         int     `json:"rcPlates"`
		RCInMarks        bool    `json:"rcInMarks"`
		Mark             string  `json:"mark"`
		Tag              look    `json:"tag"`
		TagOff           float64 `json:"tagOff"`
		TagOverState     float64 `json:"tagOverState"`
		TagRight         float64 `json:"tagRight"`
		TagAddsHeight    float64 `json:"tagAddsHeight"`
		Pct              string  `json:"pct"`
		PctRight         float64 `json:"pctRight"`
		PctUnder         float64 `json:"pctUnder"`
		PctLevel         float64 `json:"pctLevel"`
		Last             string  `json:"last"`
		LeftGives        float64 `json:"leftGives"`
		Clear            bool    `json:"clear"`
		AddsHeight       float64 `json:"addsHeight"`
		Checklist        string  `json:"checklist"`
		ChecklistLines   int     `json:"checklistLines"`
		ChecklistCut     bool    `json:"checklistCut"`
		ChecklistClear   bool    `json:"checklistClear"`
		ChecklistWidth   float64 `json:"checklistWidth"`
		ChecklistPastTag float64 `json:"checklistPastTag"`
		ChecklistShort   float64 `json:"checklistShort"`
		SinceLines       int     `json:"sinceLines"`
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

		// The name: on every card, whole, on its lines beside the place.
		if c.Name != name {
			t.Errorf("%s: the card opens with the name %q — every live card carries the name of its session", name, c.Name)
		}
		if c.NameLines != w.nameLines || c.NameCut {
			t.Errorf("%s: the name stands on %d lines (cut %v), expected %d whole", name, c.NameLines, c.NameCut, w.nameLines)
		}
		if !c.NameClear || c.NameGives < 0 || c.NameGives > 10 {
			t.Errorf("%s: the name meets the place (clear %v, it stops %.1fpx short of the place)", name, c.NameClear, c.NameGives)
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

		// Where it lives stays level with the name.
		if c.Mark != w.mark {
			t.Errorf("%s: the row says it lives in %q, expected %q", name, c.Mark, w.mark)
		}
		if c.Tag.Height <= 0 || strings.HasPrefix(c.Tag.Border, "0px") {
			t.Errorf("%s: where the session lives is not a mark with an edge: %+v", name, c.Tag)
		}
		if c.TagRight > 0.5 || c.TagRight < -0.5 {
			t.Errorf("%s: the place stands %.1fpx short of the right edge", name, c.TagRight)
		}
		if c.TagOff > 1 || c.TagOverState < 0 {
			t.Errorf("%s: the place stands %.1fpx off the first line of the name and %.1fpx over the state — it belongs on the name's line",
				name, c.TagOff, c.TagOverState)
		}
		if c.TagAddsHeight > 0.5 {
			t.Errorf("%s: the place runs %.1fpx under the name — it makes the name's line taller", name, c.TagAddsHeight)
		}

		// How full it is: the bottom right corner, under the place.
		if c.Pct != w.pct {
			t.Errorf("%s: the figure reads %q, expected %q", name, c.Pct, w.pct)
		}
		if c.PctRight < -0.5 || c.PctRight > 0.5 {
			t.Errorf("%s: the percentage stands %.1fpx short of the right edge", name, c.PctRight)
		}
		if c.PctUnder < 0 || c.PctLevel > 1 || c.Last != w.last {
			t.Errorf("%s: the figure stands %.1fpx under the place and %.1fpx off the last line on the left (%q, expected %q)",
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
			// The checklist reaches the percentage, past where the place starts.
			if c.ChecklistPastTag <= 0 || c.ChecklistShort < -0.5 || c.ChecklistShort > 0.5 {
				t.Errorf("%s: the checklist is %.0fpx wide, runs %.1fpx past where the place starts and stops %.1fpx short of the figure — "+
					"it should reach the figure, well past the place", name, c.ChecklistWidth, c.ChecklistPastTag, c.ChecklistShort)
			}
			t.Logf("%s: the checklist is %.0fpx wide, %.0fpx past where the place starts", name, c.ChecklistWidth, c.ChecklistPastTag)
		}
		if c.SinceLines != 1 {
			t.Errorf("%s: who runs the session wraps to %d lines", name, c.SinceLines)
		}
	}
	if got.Overflow > 0 {
		t.Errorf("the list pushes the phone %dpx sideways", got.Overflow)
	}
}
