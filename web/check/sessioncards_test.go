package check

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

type sessionCardSeen struct {
	Name      string   `json:"name"`
	Lines     []int    `json:"lines"`
	Line2     string   `json:"line2"`
	Line3     string   `json:"line3"`
	Line4     string   `json:"line4"`
	Line5     string   `json:"line5"`
	Name1     string   `json:"name1"`
	Controls  []string `json:"controls"`
	Runs      []string `json:"runs"`
	Alarm     string   `json:"alarm"`
	AlarmIcon bool     `json:"alarmIcon"`
	Block     bool     `json:"block"`
}

type sessionCardsRun struct {
	Desk        bool                        `json:"desk"`
	Six         map[string]*sessionCardSeen `json:"six"`
	BareArchive string                      `json:"bareArchive"`
	Heads       []struct {
		Name  string   `json:"name"`
		Group string   `json:"group"`
		Acts  []string `json:"acts"`
	} `json:"heads"`
	Lones map[string]*struct {
		Head string   `json:"head"`
		Name string   `json:"name"`
		Acts []string `json:"acts"`
	} `json:"lones"`
	Kid *struct {
		Own string `json:"own"`
	} `json:"kid"`
	Fold       string   `json:"fold"`
	Foot       []string `json:"foot"`
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
// waiting, paused, archived — on one skeleton: every line in every state, so
// the list does not change its shape when a session starts to wait; only a
// session with no checklist, the quiet one here, has neither the line of its
// step nor its bar. A live
// card carries its controls and an archived one its resume; what runs is
// counted on the state's line, and a card with nothing running counts nothing.
func checkSessionCards(t *testing.T, got sessionCardsRun) {
	t.Helper()
	order := []string{"working", "quiet", "unseen", "waiting", "paused", "archived"}
	for _, state := range order {
		c := got.Six[state]
		if c == nil {
			t.Errorf("%s: the card of the state is not on the page", state)
			continue
		}
		want := []int{1, 1, 1, 1, 1, 1}
		if state == "quiet" {
			want = []int{1, 1, 0, 0, 1, 1}
		}
		for i, n := range c.Lines {
			if i < len(want) && n != want[i] {
				t.Errorf("%s (%s): line %d of the skeleton appears %d times, want %d — lines %v", state, c.Name, i+1, n, want[i], c.Lines)
			}
		}
		if state == "archived" {
			if len(c.Controls) != 1 || c.Controls[0] != "resume conversation of notes-bot" {
				t.Errorf("the archived card has %v where the live one has its controls, want Resume", c.Controls)
			}
			continue
		}
		if len(c.Controls) < 4 {
			t.Errorf("%s (%s) has %d controls, want pause, restart, pin and close", state, c.Name, len(c.Controls))
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
	// The status time comes as milliseconds of the epoch, as the collector gives it.
	if c := got.Six["working"]; c != nil && !strings.Contains(c.Line4, "34m") {
		t.Errorf("the working card reads %q on its agent line, want how long it has worked, 34m", c.Line4)
	}
	if c := got.Six["unseen"]; c != nil && !strings.HasPrefix(c.Line2, "done · not seen") {
		t.Errorf("a session whose last turn ended after the person looked says %q, want done · not seen", c.Line2)
	}
	if c := got.Six["paused"]; c != nil && (!strings.HasPrefix(c.Line2, "paused until ") || strings.Contains(c.Line4, "→")) {
		t.Errorf("the paused card says %q / %q, want paused and when the pause ends", c.Line2, c.Line4)
	}
	if c := got.Six["quiet"]; c != nil && (c.Line3 != "" || !strings.Contains(c.Line5, "no cap")) {
		t.Errorf("the quiet card reads %q / %q, want no line of a checklist and no cap said", c.Line3, c.Line5)
	}
	// The share of the window past the cap is set apart; a session with no
	// cap has no such share.
	if c := got.Six["working"]; c != nil && !c.Block {
		t.Error("the working card at cap 80% sets apart no share of its context bar past the cap")
	}
	if c := got.Six["quiet"]; c != nil && c.Block {
		t.Error("a session with no cap sets apart a share of its context bar")
	}
	// A conversation that ended before the panel kept its settings knows no cap.
	if !strings.Contains(got.BareArchive, "—") || strings.Contains(got.BareArchive, "no cap") {
		t.Errorf("an archived card that kept no settings reads %q, want its cap unknown, not no cap", got.BareArchive)
	}
	if c := got.Six["archived"]; c != nil && (c.Line2 != "closed" || !strings.HasPrefix(c.Line3, "Write the README") ||
		!strings.Contains(c.Line5, "peak 41%")) {
		t.Errorf("the archived card reads %q / %q / %q, want closed, its last step and its peak", c.Line2, c.Line3, c.Line5)
	}

	// Every project stands under its heading — its name, its group and its
	// controls — one of a lone session as one of three, and on the phone one
	// with nothing live over its last conversation; a session no project
	// holds has none.
	wantHeads := []string{"aacpanel", "kkam-smart-home", "llm-ops", "nightly", "person", "tg-ai-shadow", "watchdog"}
	if !got.Desk {
		wantHeads = append(wantHeads, "notes-bot", "old-shelf")
		slices.Sort(wantHeads)
	}
	var heads []string
	for _, head := range got.Heads {
		heads = append(heads, head.Name)
	}
	slices.Sort(heads)
	if !slices.Equal(heads, wantHeads) {
		t.Errorf("the headings are %v, want one over every project: %v", heads, wantHeads)
	}
	for _, head := range got.Heads {
		group := "Combine"
		if head.Name == "aacpanel" {
			group = "Infra"
		}
		if head.Group != group {
			t.Errorf("the heading of %s names the group %q, want %q", head.Name, head.Group, group)
		}
		// The heading of a project whose every live session is paused, one
		// or two, continues them.
		pause := "pause sessions in project " + head.Name
		if head.Name == "nightly" || head.Name == "llm-ops" {
			pause = "continue sessions in project " + head.Name
		}
		want := []string{"new session in project " + head.Name, pause, "restart the sessions of " + head.Name}
		if strings.Join(head.Acts, "|") != strings.Join(want, "|") {
			t.Errorf("the heading of %s has %v, want %v", head.Name, head.Acts, want)
		}
	}
	// Under the heading the card of a lone session is the session alone: no
	// prefix of the project and no plus of its own, the plus is the
	// heading's. A session no project holds stands with no heading over it
	// and reads by its whole name.
	for name, want := range map[string]struct{ head, name string }{
		"person-ingest": {"person", "ingest"},
		"tg-ai-shadow":  {"tg-ai-shadow", "tg-ai-shadow"},
		"scratch-probe": {"", "scratch-probe"},
	} {
		c := got.Lones[name]
		if c == nil {
			t.Errorf("%s: the card is not on the page", name)
			continue
		}
		if c.Head != want.head || c.Name != want.name {
			t.Errorf("the card of %s stands under the heading %q and reads %q, want %q and %q", name, c.Head, c.Name, want.head, want.name)
		}
		for _, act := range c.Acts {
			if strings.HasPrefix(act, "new session") {
				t.Errorf("the card of %s has a plus of its own, %q: the plus of a project is in its heading", name, act)
			}
		}
	}
	if got.Kid == nil || got.Kid.Own != "onvif" {
		t.Errorf("a session another opened reads %+v, want its name without the part its parent says", got.Kid)
	}
	if got.Fold != "2 openedonvif" {
		t.Errorf("the fold over the sessions kkam-cameras opened reads %q, want how many and who of them waits", got.Fold)
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
	if len(got.Foot) != 2 || got.Foot[0] != "All projects and archive" || got.Foot[1] != "Contour settings" {
		t.Fatalf("the end of the list reads %v, want the two ways on", got.Foot)
	}
}

func TestTheSessionCardKeepsOneSkeletonOnTheDesk(t *testing.T) {
	var got sessionCardsRun
	runWideFixture(t, "sessioncards.html", &got)
	if !got.Desk {
		t.Fatal("the desk ran the page as a phone")
	}
	checkSessionCards(t, got)
}
