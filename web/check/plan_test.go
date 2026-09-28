package check

import (
	"reflect"
	"strings"
	"testing"
)

type planSeen struct {
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Dropped int    `json:"dropped"`
	Current int    `json:"current"`
	Over    bool   `json:"over"`
	N       int    `json:"n"`
	Step    string `json:"step"`
	Note    string `json:"note"`
}

// The plan is read the way the person reads it: where the session is counts
// the steps behind it, done or dropped, and the step it stands on is the first
// at work, or with none at work the first still to do, wherever the model put
// it in the list. A plan with nothing left says it is done, and no plan, or
// one without a step, is nothing to draw.
func TestThePlanSaysWhereTheSessionIs(t *testing.T) {
	st := func(text, status string) map[string]any { return map[string]any{"text": text, "status": status} }
	plan := func(items ...map[string]any) map[string]any {
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = it
		}
		return map[string]any{"items": out, "at": "2026-09-28T10:25:00Z", "note": "waits on the test base"}
	}
	cases := [][]any{
		{plan(st("a", "done"), st("b", "done"), st("c", "done"), st("d", "active"), st("e", "pending"), st("f", "pending"), st("g", "pending"))},
		{plan(st("a", "done"), st("b", "dropped"), st("c", "pending"), st("d", "pending"))},
		{plan(st("a", "pending"), st("b", "done"), st("c", "active"), st("d", "active"))},
		{plan(st("a", "done"), st("b", "done"), st("c", "dropped"))},
		{nil},
		{map[string]any{"items": []any{}, "at": ""}},
	}
	raw := runModuleJS(t, "src/screens/chat/plan.js", "planOf", cases)
	got := make([]*planSeen, len(raw))
	for i, r := range raw {
		if r == nil {
			continue
		}
		got[i] = &planSeen{}
		decodeInto(t, r, got[i])
	}
	want := []*planSeen{
		{Total: 7, Done: 3, Current: 3, N: 4, Step: "d", Note: "waits on the test base"},
		{Total: 4, Done: 1, Dropped: 1, Current: 2, N: 3, Step: "c", Note: "waits on the test base"},
		{Total: 4, Done: 1, Current: 2, N: 2, Step: "c", Note: "waits on the test base"},
		{Total: 3, Done: 2, Dropped: 1, Current: -1, Over: true, N: 3, Note: "waits on the test base"},
		nil,
		nil,
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("plan %d reads %+v, expected %+v", i, got[i], want[i])
		}
	}

	short := runModuleJS(t, "src/screens/chat/plan.js", "planShort", cases)
	for i, w := range []string{"4/7 · d", "3/4 · c", "2/4 · c", "plan done · 3/3", "", ""} {
		if short[i] != w {
			t.Errorf("the card says plan %d as %q, expected %q", i, short[i], w)
		}
	}
}

type planLineSeen struct {
	Bar        bool     `json:"bar"`
	Line       bool     `json:"line"`
	First      bool     `json:"first"`
	Kids       []string `json:"kids"`
	Word       string   `json:"word"`
	Num        string   `json:"num"`
	Step       string   `json:"step"`
	StepWidth  int      `json:"stepWidth"`
	Ticks      []string `json:"ticks"`
	TicksRight int      `json:"ticksRight"`
	LineAbove  bool     `json:"lineAbove"`
	Inside     bool     `json:"inside"`
	OneLine    bool     `json:"oneLine"`
	Now        bool     `json:"now"`
}

// On a phone the plan heads the card over the composer: the word, where the
// session is, the step it is on and a tick for every step, above what the
// session is doing this moment. A quiet session keeps the line alone over the
// composer, a plan all behind says it is done, forty steps still leave room
// for the step, and a session with no plan draws nothing of it. A tap on the
// line opens the plan.
func TestThePlanHeadsTheCardOverTheComposer(t *testing.T) {
	var got struct {
		Busy     planLineSeen `json:"busy"`
		Idle     planLineSeen `json:"idle"`
		Over     planLineSeen `json:"over"`
		Long     planLineSeen `json:"long"`
		None     planLineSeen `json:"none"`
		BusyNone planLineSeen `json:"busynone"`
		Opened   string       `json:"opened"`
		Overflow int          `json:"overflow"`
		Error    string       `json:"error"`
	}
	runFixture(t, "planline.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	b := got.Busy
	if !b.Line || !b.First || !b.LineAbove || !b.Inside || !b.Now {
		t.Errorf("the plan does not head the card of a session at work, inside it and over what it does: %+v", b)
	}
	if b.Word != "Plan" || b.Num != "4 of 7" || !strings.HasPrefix(b.Step, "the watcher waits") || !b.OneLine {
		t.Errorf("the line says %q %q %q (one line %v), expected Plan, 4 of 7 and the step at work", b.Word, b.Num, b.Step, b.OneLine)
	}
	wantTicks := []string{"t-done", "t-done", "t-done", "t-active now", "t-pending", "t-pending", "t-pending"}
	if !reflect.DeepEqual(b.Ticks, wantTicks) {
		t.Errorf("the ticks are %v, expected %v", b.Ticks, wantTicks)
	}
	i := got.Idle
	if !i.Line || i.Now || len(i.Kids) != 1 || i.Num != "4 of 7" {
		t.Errorf("a quiet session with a plan does not keep the line alone over the composer: %+v", i)
	}
	o := got.Over
	if o.Word != "Plan done" || o.Num != "7 of 7" || o.Step != "" || o.TicksRight > 40 {
		t.Errorf("a plan all behind says %q %q %q, its ticks %dpx from the end — expected Plan done, 7 of 7", o.Word, o.Num, o.Step, o.TicksRight)
	}
	if n := len(o.Ticks); n != 7 || o.Ticks[6] != "t-dropped" {
		t.Errorf("the ticks of the plan done are %v, the dropped step not marked", o.Ticks)
	}
	l := got.Long
	if len(l.Ticks) != 40 || l.Num != "21 of 40" || l.StepWidth < 80 || !l.OneLine {
		t.Errorf("forty steps: %d ticks, %q, the step %dpx wide (one line %v)", len(l.Ticks), l.Num, l.StepWidth, l.OneLine)
	}
	if got.None.Bar || got.None.Line {
		t.Errorf("a quiet session with no plan draws a bar: %+v", got.None)
	}
	if got.BusyNone.Line || !got.BusyNone.Now || len(got.BusyNone.Kids) != 1 {
		t.Errorf("a session with no plan draws a plan in its card: %+v", got.BusyNone)
	}
	if got.Opened != "idle,busy" {
		t.Errorf("taps on the line opened %q", got.Opened)
	}
	if got.Overflow > 0 {
		t.Errorf("the plan pushes the phone %dpx sideways", got.Overflow)
	}
}

// The line opens the whole plan in a sheet: its note on top, every step with
// its mark — a done one ticked with the time it was done, the one at work lit
// with since when, the rest hollow and a dropped one struck out — in the
// order the model keeps them.
func TestTheLineOpensTheWholePlan(t *testing.T) {
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
	runFixture(t, "plansheet.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !got.LineFirst || !strings.Contains(got.Line, "4 of 7") || !strings.Contains(got.Line, "mutate the lines") {
		t.Errorf("the card over the composer of the conversation does not open with the plan: first %v, %q", got.LineFirst, got.Line)
	}
	if got.Title != "plan" || got.Label != "plan" || !strings.HasPrefix(got.Sub, "4 of 7 · 2 done · 1 dropped · updated ") {
		t.Errorf("the sheet is headed %q (%q), %q", got.Title, got.Label, got.Sub)
	}
	if got.Note != "waits on the test base" || !got.NoteOnTop {
		t.Errorf("the note %q is not over the steps (%v)", got.Note, got.NoteOnTop)
	}
	type want struct{ cls, text, at, tag string }
	wants := []want{
		{"plstep s-done", "read the code", got.Times["done1"], "span"},
		{"plstep s-dropped", "a second collector", "", "s"},
		{"plstep s-done", "write the tests", got.Times["done2"], "span"},
		{"plstep s-active now", "mutate the lines of the feature", "since " + got.Times["active"], "span"},
		{"plstep s-pending", "the full check", "", "span"},
		{"plstep s-pending", "shots of the live screen", "", "span"},
		{"plstep s-pending", "hand it back", "", "span"},
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

// At a desk the plan is a block at the top of the timeline column, not a line
// over the composer, and it stays at the top of the feed while the feed
// scrolls under it, over the entries it covers. Folded, it is one line with
// the step; the fold is kept by the browser, so the conversation drawn again
// comes up folded, and unfolding is kept the same way.
func TestTheDeskKeepsThePlanAtTheTopOfTheTimeline(t *testing.T) {
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
	runWideFixture(t, "plandesk.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !got.First || got.Steps != 7 || !strings.HasPrefix(got.Head, "Plan4 of 7") || got.Note != "waits on the test base" {
		t.Errorf("the timeline column does not open with the whole plan: first %v, %q, %d steps, note %q", got.First, got.Head, got.Steps, got.Note)
	}
	if got.InComposer || !got.NowBar {
		t.Errorf("at a desk the plan stands over the composer too (%v), or the card of what is going on is gone (%v)", got.InComposer, !got.NowBar)
	}
	if got.Scrolls < 1000 {
		t.Fatalf("the feed scrolls %dpx: too little to see the plan held", got.Scrolls)
	}
	if got.AtTop < -1 || got.AtTop > 2 || got.Over > 0 {
		t.Errorf("at the top of the feed the plan stands %dpx under the top of the column, %dpx off its line", got.AtTop, got.Over)
	}
	if got.LineGone < 500 || got.Held < -1 || got.Held > 2 {
		t.Errorf("scrolled down, the column is %dpx above the feed and the plan %dpx under its top: it is not held", got.LineGone, got.Held)
	}
	if got.CoveredHit != "" && got.CoveredHit != "plan" {
		t.Errorf("an entry under the plan shows through it: a point there is %q", got.CoveredHit)
	}
	f := got.Folded
	if f.Steps != 0 || f.Expanded != "false" || !f.Shorter || !f.OneLine || f.Kept != "folded" ||
		!strings.HasPrefix(f.Head, "Plan4 of 7") || f.Step != "mutate the lines of the feature" {
		t.Errorf("folded, the plan is %+v", f)
	}
	if !got.Again.Folded || got.Again.Steps != 0 {
		t.Errorf("drawn again, the plan forgot it was folded: %+v", got.Again)
	}
	if got.Unfolded.Steps != 7 || got.Unfolded.Kept != "open" {
		t.Errorf("unfolded, the plan is %+v", got.Unfolded)
	}
	if got.Overflow > 0 {
		t.Errorf("the desk scrolls %dpx sideways", got.Overflow)
	}
}

// A card of the sessions list says where a session is in its plan on the line
// of its state, after the state and in the room it leaves: the state stays
// whole on one line, the plan is cut before it is, and a session with no plan
// says nothing of one.
func TestTheCardSaysWhereTheSessionIsInItsPlan(t *testing.T) {
	type card struct {
		State      string `json:"state"`
		StateCut   bool   `json:"stateCut"`
		StateLines int    `json:"stateLines"`
		Plan       string `json:"plan"`
		InState    bool   `json:"inState"`
		SameLine   bool   `json:"sameLine"`
		PlanWidth  int    `json:"planWidth"`
		PlanInside bool   `json:"planInside"`
	}
	var got struct {
		Busy     card `json:"busy"`
		Done     card `json:"done"`
		Asks     card `json:"asks"`
		Plain    card `json:"plain"`
		Overflow int  `json:"overflow"`
	}
	runFixture(t, "plancard.html", &got)
	for name, c := range map[string]struct {
		card
		plan  string
		state string
	}{
		"busy": {got.Busy, "4/7 · the watcher waits for the background and checks a fresh snapshot again", "working"},
		"done": {got.Done, "plan done · 7/7", "idle"},
		"asks": {got.Asks, "2/3 · later 1", "asks you · Which way"},
	} {
		if c.Plan != c.plan || !c.InState || !c.SameLine || c.PlanWidth < 40 || !c.PlanInside {
			t.Errorf("%s: the card says %q (in the state %v, on its line %v, %dpx, inside %v), expected %q",
				name, c.Plan, c.InState, c.SameLine, c.PlanWidth, c.PlanInside, c.plan)
		}
		if c.State != c.state || c.StateCut || c.StateLines != 1 {
			t.Errorf("%s: the state reads %q on %d lines (cut %v), expected %q whole on one", name, c.State, c.StateLines, c.StateCut, c.state)
		}
	}
	if got.Plain.Plan != "" || got.Plain.InState || got.Plain.State != "idle" {
		t.Errorf("a session with no plan says %+v", got.Plain)
	}
	if got.Overflow > 0 {
		t.Errorf("the list pushes the phone %dpx sideways", got.Overflow)
	}
}
