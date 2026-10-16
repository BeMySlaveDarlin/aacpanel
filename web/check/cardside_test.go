package check

import (
	"strings"
	"testing"
)

// Actions keep the top right corner as a long name yields its end. The place
// follows Remote Control on the state line, leaving the checklist its width
// and the percentage beside the last words, on both widths of a phone.
func TestThePhoneCardKeepsItsActionsAboveItsPlace(t *testing.T) {
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
		Name          string  `json:"name"`
		NameLines     int     `json:"nameLines"`
		NameCut       bool    `json:"nameCut"`
		NameGives     float64 `json:"nameGives"`
		NameClear     bool    `json:"nameClear"`
		NameEllipsis  bool    `json:"nameEllipsis"`
		ActionsRight  float64 `json:"actionsRight"`
		ActionsOnLine bool    `json:"actionsOnLine"`
		StateReads    string  `json:"stateReads"`
		StateLines    int     `json:"stateLines"`
		RC            *struct {
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
		TagInState       bool    `json:"tagInState"`
		TagAfterLast     bool    `json:"tagAfterLast"`
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
	var views map[string]struct {
		Cards    map[string]card `json:"cards"`
		Overflow int             `json:"overflow"`
	}
	runFixture(t, "cardside.html", &views)
	const long = "blog-fingerprint-rotation-second-pass"
	want := map[string]struct {
		state, mark, pct, last string
		nameLines              int
		// How the checklist stands: "cut" at its end, "whole", or "" for none.
		checklist string
	}{
		"aacpanel":       {"working · 15 h 40 m · RC", "stream", "78%", "pjsince", 1, "cut"},
		"atlas":          {"background · 4 d 20 h · RC", "stream", "64%", "pjwork", 1, "whole"},
		"blog":           {"background · 3 h 0 m", "tmux", "9%", "pjwork", 1, ""},
		long:             {"idle · 25 min", "stream", "22%", "pjsince", 1, ""},
		"shop":           {"idle · 1 h 30 m", "stream", "31%", "pjsince", 1, ""},
		"codex-5afc361b": {"idle · 42 min", "daemon", "15%", "pjsince", 1, ""},
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
				remote := strings.HasSuffix(w.state, " · RC")

				// The name yields only its end to the actions on its right.
				if c.Name != name {
					t.Errorf("%s: the card opens with the name %q — every live card carries the name of its session", name, c.Name)
				}
				if c.NameLines != w.nameLines || c.NameCut != (name == long) || !c.NameEllipsis {
					t.Errorf("%s: the name stands on %d lines (cut %v), expected %d with only the long name cut", name, c.NameLines, c.NameCut, w.nameLines)
				}
				if !c.NameClear || c.NameGives < 0 || c.NameGives > 10 {
					t.Errorf("%s: the name meets the actions (clear %v, it stops %.1fpx short of the actions)", name, c.NameClear, c.NameGives)
				}

				// Remote Control: a word on the state's line, after the time.
				if c.StateReads != w.state+" "+w.mark || c.StateLines != 1 {
					t.Errorf("%s: the state's line reads %q on %d lines, expected %q on one", name, c.StateReads, c.StateLines, w.state+" "+w.mark)
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

				// The place is the last chip on the state line, after RC when present.
				if c.Mark != w.mark || c.Tag.Height <= 0 || strings.HasPrefix(c.Tag.Border, "0px") {
					t.Errorf("%s: the place is %q, expected the %s chip: %+v", name, c.Mark, w.mark, c.Tag)
				}
				if !c.TagInState || !c.TagAfterLast || c.TagOff > 1 || c.TagAddsHeight > 0.5 {
					t.Errorf("%s: the chip is not level after the last state word (in state %v, after last %v, offset %.1fpx, adds %.1fpx)",
						name, c.TagInState, c.TagAfterLast, c.TagOff, c.TagAddsHeight)
				}
				if !c.ActionsOnLine || c.ActionsRight < -0.5 || c.ActionsRight > 0.5 {
					t.Errorf("%s: actions do not stand level at the name's right edge (on line %v, offset %.1fpx)", name, c.ActionsOnLine, c.ActionsRight)
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
		})
	}
}
