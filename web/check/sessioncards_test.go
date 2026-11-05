package check

import (
	"regexp"
	"strings"
	"testing"
)

type sessionCardSeen struct {
	Name        string `json:"name"`
	Height      int    `json:"height"`
	Lines       []int  `json:"lines"`
	LineHeights []int  `json:"lineHeights"`
	Line2       string `json:"line2"`
	Line3       string `json:"line3"`
	Line4       string `json:"line4"`
	Line5       string `json:"line5"`
	Name1       string `json:"name1"`
	Controls    []struct {
		Label string `json:"label"`
		Shown bool   `json:"shown"`
	} `json:"controls"`
	Cut       []string `json:"cut"`
	Runs      []string `json:"runs"`
	Alarm     string   `json:"alarm"`
	AlarmIcon bool     `json:"alarmIcon"`
	RunsGap   *int     `json:"runsGap"`
	Overflow  int      `json:"overflow"`
}

type sessionCardsRun struct {
	Desk        bool                        `json:"desk"`
	Six         map[string]*sessionCardSeen `json:"six"`
	CutAnywhere []string                    `json:"cutAnywhere"`
	Heads       []struct {
		Name string   `json:"name"`
		Acts []string `json:"acts"`
	} `json:"heads"`
	Solo *struct {
		Pre  string   `json:"pre"`
		Own  string   `json:"own"`
		Acts []string `json:"acts"`
	} `json:"solo"`
	Kid *struct {
		Own string `json:"own"`
	} `json:"kid"`
	Fold string `json:"fold"`
	Foot []struct {
		Text   string `json:"text"`
		Border string `json:"border"`
		Bg     string `json:"bg"`
		Height int    `json:"height"`
	} `json:"foot"`
	Overflow   int `json:"overflow"`
	Width      int `json:"width"`
	RestartAll struct {
		Asked     []string `json:"asked"`
		Restarted []string `json:"restarted"`
		Cancelled struct {
			Sheet bool `json:"sheet"`
			Sent  int  `json:"sent"`
		} `json:"cancelled"`
	} `json:"restartAll"`
	Continued []struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	} `json:"continued"`
}

// The six states a card has to hold — working, quiet, done and not seen,
// waiting, paused, archived — on one skeleton: every line in every state and
// the same height whatever the state, so the list does not move under the eye
// when a session starts to wait. The controls are there without a hover;
// nothing the card says is cut with an ellipsis; what runs is counted right
// after the state's word, and a card with nothing running counts nothing.
func checkSessionCards(t *testing.T, got sessionCardsRun) {
	t.Helper()
	order := []string{"working", "quiet", "unseen", "waiting", "paused", "archived"}
	height := 0
	for _, state := range order {
		c := got.Six[state]
		if c == nil {
			t.Errorf("%s: the card of the state is not on the page", state)
			continue
		}
		for i, n := range c.Lines {
			if n != 1 {
				t.Errorf("%s (%s): line %d of the skeleton appears %d times, want once — lines %v", state, c.Name, i+1, n, c.Lines)
			}
		}
		if height == 0 {
			height = c.Height
		}
		if d := c.Height - height; d > 1 || d < -1 {
			t.Errorf("%s (%s) is %d px tall and the working card %d px (lines %v) — the card changes its height with its state",
				state, c.Name, c.Height, height, c.LineHeights)
		}
		if len(c.Cut) > 0 {
			t.Errorf("%s (%s): cut with an ellipsis or past its box: %v", state, c.Name, c.Cut)
		}
		if c.Overflow > 0 {
			t.Errorf("%s (%s) runs %d px past its own edge", state, c.Name, c.Overflow)
		}
		if state == "archived" {
			if len(c.Controls) != 1 || c.Controls[0].Label != "resume conversation of notes-bot" || !c.Controls[0].Shown {
				t.Errorf("the archived card has %+v where the live one has its controls, want Resume in sight", c.Controls)
			}
			continue
		}
		if len(c.Controls) < 4 {
			t.Errorf("%s (%s) has %d controls, want pause, restart, pin and close", state, c.Name, len(c.Controls))
		}
		for _, ctl := range c.Controls {
			if !ctl.Shown {
				t.Errorf("%s (%s): %q is not in sight without a hover", state, c.Name, ctl.Label)
			}
		}
	}

	if w := got.Six["working"]; w != nil && (strings.Join(w.Runs, ",") != "1" || !strings.HasPrefix(w.Line2, "working")) {
		t.Errorf("the working card says %q and counts %v running, want working and the one subagent", w.Line2, w.Runs)
	}
	if w := got.Six["waiting"]; w != nil {
		if !strings.HasPrefix(w.Line2, "asks Layout") || strings.Join(w.Runs, ",") != "2,1" || !strings.Contains(w.Line2, "feed lags 1 min") {
			t.Errorf("the waiting card says %q and counts %v, want the subject of the question, two subagents and a "+
				"command running, and the collector's note", w.Line2, w.Runs)
		}
		if w.RunsGap == nil || *w.RunsGap > 16 {
			gap := -1
			if w.RunsGap != nil {
				gap = *w.RunsGap
			}
			t.Errorf("the counters stand %d px from the state's word — they go right after it, not to the right edge", gap)
		}
		if !strings.Contains(w.Line4, "2 queued") || !strings.Contains(w.Line5, "RC") || !strings.Contains(w.Line5, "worktree") {
			t.Errorf("the waiting card reads %q / %q, want its queue in words and its badges", w.Line4, w.Line5)
		}
	}
	for _, state := range []string{"quiet", "unseen", "paused", "archived"} {
		if c := got.Six[state]; c != nil && len(c.Runs) > 0 {
			t.Errorf("%s (%s) counts %v running with nothing running", state, c.Name, c.Runs)
		}
	}
	// A wake ahead is said on the state's line, a clock and its time; a card
	// with none says nothing of one.
	for state, c := range got.Six {
		if c == nil {
			continue
		}
		if state == "quiet" {
			if !regexp.MustCompile(`^\d\d:\d\d$`).MatchString(c.Alarm) || !c.AlarmIcon {
				t.Errorf("the quiet card with a wake ahead says %q (clock %v) on the state's line, want the clock and its time", c.Alarm, c.AlarmIcon)
			}
		} else if c.Alarm != "" {
			t.Errorf("%s (%s) says a wake %q with none ahead", state, c.Name, c.Alarm)
		}
	}
	if c := got.Six["unseen"]; c != nil && !strings.HasPrefix(c.Line2, "done · not seen") {
		t.Errorf("a session whose last turn ended after the person looked says %q, want done · not seen", c.Line2)
	}
	if c := got.Six["paused"]; c != nil && (c.Line2 != "paused" || !strings.Contains(c.Line4, "→")) {
		t.Errorf("the paused card says %q / %q, want paused and when the pause ends", c.Line2, c.Line4)
	}
	if c := got.Six["quiet"]; c != nil && (!strings.HasPrefix(c.Line3, "no checklist") || !strings.Contains(c.Line5, "no cap")) {
		t.Errorf("the quiet card reads %q / %q, want no checklist and no cap said", c.Line3, c.Line5)
	}
	if c := got.Six["archived"]; c != nil && (c.Line2 != "closed" || !strings.HasPrefix(c.Line3, "Write the README") ||
		!strings.Contains(c.Line5, "peak 41%")) {
		t.Errorf("the archived card reads %q / %q / %q, want closed, its last step and its peak", c.Line2, c.Line3, c.Line5)
	}
	if len(got.CutAnywhere) > 0 {
		t.Errorf("cards of the list cut what they say: %v", got.CutAnywhere)
	}

	if len(got.Heads) != 3 || got.Heads[0].Name != "aacpanel" || got.Heads[1].Name != "kkam-smart-home" || got.Heads[2].Name != "nightly" {
		t.Fatalf("the headings are %+v, want only the three projects of two sessions or more", got.Heads)
	}
	for _, head := range got.Heads {
		pause := "pause sessions in project " + head.Name
		if head.Name == "nightly" {
			pause = "continue sessions in project " + head.Name
		}
		want := []string{"new session in project " + head.Name, pause, "restart the sessions of " + head.Name}
		if strings.Join(head.Acts, "|") != strings.Join(want, "|") {
			t.Errorf("the heading of %s has %v, want %v", head.Name, head.Acts, want)
		}
	}
	if got.Solo == nil || got.Solo.Pre != "person-" || got.Solo.Own != "ingest" ||
		len(got.Solo.Acts) == 0 || got.Solo.Acts[0] != "new session in project person" {
		t.Errorf("the card of a project of one session reads %+v, want the project as a prefix and its plus first", got.Solo)
	}
	if got.Kid == nil || got.Kid.Own != "onvif" {
		t.Errorf("a session another opened reads %+v, want its name without the part its parent says", got.Kid)
	}
	if got.Fold != "2 openedonvif" {
		t.Errorf("the fold over the sessions kkam-cameras opened reads %q, want how many and who of them waits", got.Fold)
	}
	if got.Overflow > 0 {
		t.Errorf("the page scrolls %d px sideways", got.Overflow)
	}

	// The heading restarts its sessions one by one, each confirmed as a
	// restart of one is; a cancel of the first stops the rest.
	r := got.RestartAll
	if strings.Join(r.Restarted, ",") != "kkam-cameras,kkam-cameras-onvif,kkam-cameras-rtsp" || len(r.Asked) != 3 {
		t.Errorf("the restart of the heading asked %v and restarted %v, want each of the three sessions confirmed and restarted", r.Asked, r.Restarted)
	}
	if r.Cancelled.Sheet || r.Cancelled.Sent != 0 {
		t.Errorf("a cancel of the restart of the heading left a sheet %v and sent %d", r.Cancelled.Sheet, r.Cancelled.Sent)
	}
	// A project all paused is continued from its heading by the resume of
	// the project.
	if len(got.Continued) != 1 || got.Continued[0].Kind != "project.pause" || got.Continued[0].Params["project"] != float64(8) ||
		got.Continued[0].Params["resume"] != true || got.Continued[0].Params["group"] != false {
		t.Errorf("the continue of the heading sent %+v, want the resume of project 8", got.Continued)
	}
}

func TestTheSessionCardKeepsOneSkeletonOnThePhone(t *testing.T) {
	var got sessionCardsRun
	runFixture(t, "sessioncards.html", &got)
	if got.Desk {
		t.Fatal("the phone ran the page as a desk")
	}
	checkSessionCards(t, got)
	if len(got.Foot) != 2 || got.Foot[0].Text != "All projects and archive" || got.Foot[1].Text != "Contour settings" {
		t.Fatalf("the end of the list reads %+v, want the two ways on", got.Foot)
	}
	for _, link := range got.Foot {
		if link.Border != "none" || link.Bg != "rgba(0, 0, 0, 0)" || link.Height > 44 {
			t.Errorf("%q is drawn as a card (%+v) — a quiet line of text is wanted", link.Text, link)
		}
	}
}

func TestTheSessionCardKeepsOneSkeletonOnTheDesk(t *testing.T) {
	var got sessionCardsRun
	runWideFixture(t, "sessioncards.html", &got)
	if !got.Desk {
		t.Fatal("the desk ran the page as a phone")
	}
	checkSessionCards(t, got)
	if got.Width < 340 || got.Width > 360 {
		t.Errorf("a card of the column is %d px wide at the scale of 1.2, want the 351 px of the design", got.Width)
	}
}
