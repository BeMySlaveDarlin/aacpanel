package check

import (
	"reflect"
	"strings"
	"testing"
)

type checklistSeen struct {
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Dropped int    `json:"dropped"`
	Current int    `json:"current"`
	Over    bool   `json:"over"`
	N       int    `json:"n"`
	Step    string `json:"step"`
	Note    string `json:"note"`
}

// The checklist is read the way the person reads it: where the session is
// counts the steps behind it, done or dropped, and the step it stands on is
// the first at work, or with none at work the first still to do, wherever the
// model put it in the list. A checklist with nothing left says it is done,
// and no checklist, or one without a step, is nothing to draw.
func TestTheChecklistSaysWhereTheSessionIs(t *testing.T) {
	st := func(text, status string) map[string]any { return map[string]any{"text": text, "status": status} }
	checklist := func(items ...map[string]any) map[string]any {
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = it
		}
		return map[string]any{"items": out, "at": "2026-09-28T10:25:00Z", "note": "waits on the test base"}
	}
	cases := [][]any{
		{checklist(st("a", "done"), st("b", "done"), st("c", "done"), st("d", "active"), st("e", "pending"), st("f", "pending"), st("g", "pending"))},
		{checklist(st("a", "done"), st("b", "dropped"), st("c", "pending"), st("d", "pending"))},
		{checklist(st("a", "pending"), st("b", "done"), st("c", "active"), st("d", "active"))},
		{checklist(st("a", "done"), st("b", "done"), st("c", "dropped"))},
		{nil},
		{map[string]any{"items": []any{}, "at": ""}},
	}
	raw := runModuleJS(t, "src/screens/chat/checklist.js", "checklistOf", cases)
	got := make([]*checklistSeen, len(raw))
	for i, r := range raw {
		if r == nil {
			continue
		}
		got[i] = &checklistSeen{}
		decodeInto(t, r, got[i])
	}
	want := []*checklistSeen{
		{Total: 7, Done: 3, Current: 3, N: 4, Step: "d", Note: "waits on the test base"},
		{Total: 4, Done: 1, Dropped: 1, Current: 2, N: 3, Step: "c", Note: "waits on the test base"},
		{Total: 4, Done: 1, Current: 2, N: 2, Step: "c", Note: "waits on the test base"},
		{Total: 3, Done: 2, Dropped: 1, Current: -1, Over: true, N: 3, Note: "waits on the test base"},
		nil,
		nil,
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("checklist %d reads %+v, expected %+v", i, got[i], want[i])
		}
	}

	short := runModuleJS(t, "src/screens/chat/checklist.js", "checklistShort", cases)
	for i, w := range []string{"4/7 · d", "3/4 · c", "2/4 · c", "checklist done · 3/3", "", ""} {
		if short[i] != w {
			t.Errorf("the card says checklist %d as %q, expected %q", i, short[i], w)
		}
	}
}

type checklistLineSeen struct {
	Bar        bool     `json:"bar"`
	Line       bool     `json:"line"`
	First      bool     `json:"first"`
	Kids       []string `json:"kids"`
	Word       string   `json:"word"`
	Icon       bool     `json:"icon"`
	Num        string   `json:"num"`
	Label      string   `json:"label"`
	Gap        float64  `json:"gap"`
	Pad        float64  `json:"pad"`
	Step       string   `json:"step"`
	StepWidth  int      `json:"stepWidth"`
	Ticks      []string `json:"ticks"`
	TicksRight int      `json:"ticksRight"`
	LineAbove  bool     `json:"lineAbove"`
	Inside     bool     `json:"inside"`
	OneLine    bool     `json:"oneLine"`
	Now        bool     `json:"now"`
}

// On a phone the checklist heads the card over the composer: a check mark and
// where the session is as a count, the step it is on and a tick for every
// step, right above what the session is doing this moment — no more room
// between the two lines than the bar under it holds as its padding. Its label
// says the checklist in words. A quiet session keeps the line alone over the
// composer, a checklist all behind says it is done, forty steps still leave
// room for the step, and a session with no checklist draws nothing of it. A
// tap on the line opens the checklist.
func TestTheChecklistHeadsTheCardOverTheComposer(t *testing.T) {
	var got struct {
		Busy     checklistLineSeen `json:"busy"`
		Idle     checklistLineSeen `json:"idle"`
		Over     checklistLineSeen `json:"over"`
		Long     checklistLineSeen `json:"long"`
		None     checklistLineSeen `json:"none"`
		BusyNone checklistLineSeen `json:"busynone"`
		Opened   string            `json:"opened"`
		Overflow int               `json:"overflow"`
		Error    string            `json:"error"`
	}
	runFixture(t, "checklistline.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	b := got.Busy
	if !b.Line || !b.First || !b.LineAbove || !b.Inside || !b.Now {
		t.Errorf("the checklist does not head the card of a session at work, inside it and over what it does: %+v", b)
	}
	if !b.Icon || b.Word != "" || b.Num != "4/7" || !strings.HasPrefix(b.Step, "the watcher waits") || !b.OneLine {
		t.Errorf("the line heads with mark %v, %q, %q and says %q (one line %v), expected the mark, 4/7 and the step at work",
			b.Icon, b.Word, b.Num, b.Step, b.OneLine)
	}
	if !strings.HasPrefix(b.Label, "checklist: step 4 of 7, the watcher waits") || !strings.HasSuffix(b.Label, "open the checklist") {
		t.Errorf("the line is labelled %q, expected the checklist in words", b.Label)
	}
	if b.Pad <= 0 || b.Gap < 0 || b.Gap > b.Pad+0.5 {
		t.Errorf("the now line stands %.1fpx under the checklist line, the bar's padding is %.1fpx", b.Gap, b.Pad)
	}
	wantTicks := []string{"t-done", "t-done", "t-done", "t-active now", "t-pending", "t-pending", "t-pending"}
	if !reflect.DeepEqual(b.Ticks, wantTicks) {
		t.Errorf("the ticks are %v, expected %v", b.Ticks, wantTicks)
	}
	i := got.Idle
	if !i.Line || i.Now || len(i.Kids) != 1 || i.Num != "4/7" {
		t.Errorf("a quiet session with a checklist does not keep the line alone over the composer: %+v", i)
	}
	o := got.Over
	if !o.Icon || o.Word != "" || o.Num != "7/7" || o.Step != "" || o.TicksRight > 40 || !strings.HasPrefix(o.Label, "checklist done, 7 of 7") {
		t.Errorf("a checklist all behind heads with mark %v, %q, %q, says %q, labelled %q, its ticks %dpx from the end — expected the mark, 7/7 and checklist done in words",
			o.Icon, o.Word, o.Num, o.Step, o.Label, o.TicksRight)
	}
	if n := len(o.Ticks); n != 7 || o.Ticks[6] != "t-dropped" {
		t.Errorf("the ticks of the checklist done are %v, the dropped step not marked", o.Ticks)
	}
	l := got.Long
	if len(l.Ticks) != 40 || l.Num != "21/40" || l.StepWidth < 80 || !l.OneLine {
		t.Errorf("forty steps: %d ticks, %q, the step %dpx wide (one line %v)", len(l.Ticks), l.Num, l.StepWidth, l.OneLine)
	}
	if got.None.Bar || got.None.Line {
		t.Errorf("a quiet session with no checklist draws a bar: %+v", got.None)
	}
	if got.BusyNone.Line || !got.BusyNone.Now || len(got.BusyNone.Kids) != 1 {
		t.Errorf("a session with no checklist draws a checklist in its card: %+v", got.BusyNone)
	}
	if got.Opened != "idle,busy" {
		t.Errorf("taps on the line opened %q", got.Opened)
	}
	if got.Overflow > 0 {
		t.Errorf("the checklist pushes the phone %dpx sideways", got.Overflow)
	}
}

// The line opens the whole checklist in a sheet: its note on top, every step
// with its mark — a done one ticked with the time it was done, the one at
// work lit with since when, the rest hollow and a dropped one struck out — in
// the order the model keeps them.
func TestTheLineOpensTheWholeChecklist(t *testing.T) {
	var got struct {
		LineFirst bool   `json:"lineFirst"`
		Line      string `json:"line"`
		Title     string `json:"title"`
		Sub       string `json:"sub"`
		Label     string `json:"label"`
		Note      string `json:"note"`
		NoteOnTop bool   `json:"noteOnTop"`
		Steps     []struct {
			Cls    string `json:"cls"`
			Text   string `json:"text"`
			At     string `json:"at"`
			Tick   bool   `json:"tick"`
			Struck string `json:"struck"`
			Tag    string `json:"tag"`
		} `json:"steps"`
		Times    map[string]string `json:"times"`
		Overflow int               `json:"overflow"`
		Error    string            `json:"error"`
	}
	runFixture(t, "checklistsheet.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !got.LineFirst || !strings.Contains(got.Line, "4/7") || !strings.Contains(got.Line, "mutate the lines") {
		t.Errorf("the card over the composer of the conversation does not open with the checklist: first %v, %q", got.LineFirst, got.Line)
	}
	if got.Title != "checklist" || got.Label != "checklist" || !strings.HasPrefix(got.Sub, "4 of 7 · 2 done · 1 dropped · updated ") {
		t.Errorf("the sheet is headed %q (%q), %q", got.Title, got.Label, got.Sub)
	}
	if got.Note != "waits on the test base" || !got.NoteOnTop {
		t.Errorf("the note %q is not over the steps (%v)", got.Note, got.NoteOnTop)
	}
	type want struct{ cls, text, at, tag string }
	wants := []want{
		{"ckitem s-done", "read the code", got.Times["done1"], "span"},
		{"ckitem s-dropped", "a second collector", "", "s"},
		{"ckitem s-done", "write the tests", got.Times["done2"], "span"},
		{"ckitem s-active now", "mutate the lines of the feature", "since " + got.Times["active"], "span"},
		{"ckitem s-pending", "the full check", "", "span"},
		{"ckitem s-pending", "shots of the live screen", "", "span"},
		{"ckitem s-pending", "hand it back", "", "span"},
	}
	if len(got.Steps) != len(wants) {
		t.Fatalf("the sheet lists %d steps, expected %d: %+v", len(got.Steps), len(wants), got.Steps)
	}
	for n, w := range wants {
		s := got.Steps[n]
		if s.Cls != w.cls || s.Text != w.text || s.At != w.at || s.Tag != w.tag {
			t.Errorf("step %d is %+v, expected %+v", n, s, w)
		}
		if tick := strings.Contains(w.cls, "done") || strings.Contains(w.cls, "dropped"); s.Tick != tick {
			t.Errorf("step %d (%s) carries a mark %v", n, w.cls, s.Tick)
		}
		if struck := s.Struck == "line-through"; struck != (w.tag == "s") {
			t.Errorf("step %d (%s) is struck out: %q", n, w.cls, s.Struck)
		}
	}
	if got.Overflow > 0 {
		t.Errorf("the sheet pushes the phone %dpx sideways", got.Overflow)
	}
}

// At a desk the checklist is a block at the top of the timeline column, not a
// line over the composer, and it stays at the top of the feed while the feed
// scrolls under it, over the entries it covers. Folded, it is one line with
// the step; the fold is kept by the browser, so the conversation drawn again
// comes up folded, and unfolding is kept the same way.
func TestTheDeskKeepsTheChecklistAtTheTopOfTheTimeline(t *testing.T) {
	var got struct {
		First      bool   `json:"first"`
		InComposer bool   `json:"inComposer"`
		NowBar     bool   `json:"nowBar"`
		Head       string `json:"head"`
		Steps      int    `json:"steps"`
		Note       string `json:"note"`
		Scrolls    int    `json:"scrolls"`
		AtTop      int    `json:"atTop"`
		Over       int    `json:"over"`
		LineGone   int    `json:"lineGone"`
		Held       int    `json:"held"`
		CoveredHit string `json:"coveredHit"`
		Folded     struct {
			Head     string `json:"head"`
			Step     string `json:"step"`
			Steps    int    `json:"steps"`
			Expanded string `json:"expanded"`
			Shorter  bool   `json:"shorter"`
			OneLine  bool   `json:"oneLine"`
			Kept     string `json:"kept"`
		} `json:"folded"`
		Again struct {
			Folded bool `json:"folded"`
			Steps  int  `json:"steps"`
		} `json:"again"`
		Unfolded struct {
			Steps int    `json:"steps"`
			Kept  string `json:"kept"`
		} `json:"unfolded"`
		Overflow int    `json:"overflow"`
		Error    string `json:"error"`
	}
	runWideFixture(t, "checklistdesk.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !got.First || got.Steps != 7 || !strings.HasPrefix(got.Head, "Checklist4 of 7") || got.Note != "waits on the test base" {
		t.Errorf("the timeline column does not open with the whole checklist: first %v, %q, %d steps, note %q", got.First, got.Head, got.Steps, got.Note)
	}
	if got.InComposer || !got.NowBar {
		t.Errorf("at a desk the checklist stands over the composer too (%v), or the card of what is going on is gone (%v)", got.InComposer, !got.NowBar)
	}
	if got.Scrolls < 1000 {
		t.Fatalf("the feed scrolls %dpx: too little to see the checklist held", got.Scrolls)
	}
	if got.AtTop < -1 || got.AtTop > 2 || got.Over > 0 {
		t.Errorf("at the top of the feed the checklist stands %dpx under the top of the column, %dpx off its line", got.AtTop, got.Over)
	}
	if got.LineGone < 500 || got.Held < -1 || got.Held > 2 {
		t.Errorf("scrolled down, the column is %dpx above the feed and the checklist %dpx under its top: it is not held", got.LineGone, got.Held)
	}
	if got.CoveredHit != "" && got.CoveredHit != "checklist" {
		t.Errorf("an entry under the checklist shows through it: a point there is %q", got.CoveredHit)
	}
	f := got.Folded
	if f.Steps != 0 || f.Expanded != "false" || !f.Shorter || !f.OneLine || f.Kept != "folded" ||
		!strings.HasPrefix(f.Head, "Checklist4 of 7") || f.Step != "mutate the lines of the feature" {
		t.Errorf("folded, the checklist is %+v", f)
	}
	if !got.Again.Folded || got.Again.Steps != 0 {
		t.Errorf("drawn again, the checklist forgot it was folded: %+v", got.Again)
	}
	if got.Unfolded.Steps != 7 || got.Unfolded.Kept != "open" {
		t.Errorf("unfolded, the checklist is %+v", got.Unfolded)
	}
	if got.Overflow > 0 {
		t.Errorf("the desk scrolls %dpx sideways", got.Overflow)
	}
}

// A card of the sessions list says where a session is in its checklist on the
// line of its state, after the state and in the room it leaves: the state
// stays whole on one line, the checklist is cut before it is, and a session
// with no checklist says nothing of one.
func TestTheCardSaysWhereTheSessionIsInItsChecklist(t *testing.T) {
	type card struct {
		State           string `json:"state"`
		StateCut        bool   `json:"stateCut"`
		StateLines      int    `json:"stateLines"`
		Checklist       string `json:"checklist"`
		InState         bool   `json:"inState"`
		SameLine        bool   `json:"sameLine"`
		ChecklistWidth  int    `json:"checklistWidth"`
		ChecklistInside bool   `json:"checklistInside"`
	}
	var got struct {
		Busy     card `json:"busy"`
		Done     card `json:"done"`
		Asks     card `json:"asks"`
		Plain    card `json:"plain"`
		Overflow int  `json:"overflow"`
	}
	runFixture(t, "checklistcard.html", &got)
	for name, c := range map[string]struct {
		card
		checklist string
		state     string
	}{
		"busy": {got.Busy, "4/7 · the watcher waits for the background and checks a fresh snapshot again", "working"},
		"done": {got.Done, "checklist done · 7/7", "idle"},
		"asks": {got.Asks, "2/3 · later 1", "asks you · Which way"},
	} {
		if c.Checklist != c.checklist || !c.InState || !c.SameLine || c.ChecklistWidth < 40 || !c.ChecklistInside {
			t.Errorf("%s: the card says %q (in the state %v, on its line %v, %dpx, inside %v), expected %q",
				name, c.Checklist, c.InState, c.SameLine, c.ChecklistWidth, c.ChecklistInside, c.checklist)
		}
		if c.State != c.state || c.StateCut || c.StateLines != 1 {
			t.Errorf("%s: the state reads %q on %d lines (cut %v), expected %q whole on one", name, c.State, c.StateLines, c.StateCut, c.state)
		}
	}
	if got.Plain.Checklist != "" || got.Plain.InState || got.Plain.State != "idle" {
		t.Errorf("a session with no checklist says %+v", got.Plain)
	}
	if got.Overflow > 0 {
		t.Errorf("the list pushes the phone %dpx sideways", got.Overflow)
	}
}
