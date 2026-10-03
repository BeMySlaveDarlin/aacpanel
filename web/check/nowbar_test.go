package check

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

func twoDigits(n int) string { return fmt.Sprintf("%02d", n) }

// decodeInto reads what a module returned into the shape a test expects.
func decodeInto(t *testing.T, raw any, into any) {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("the module returned %s: %v", b, err)
	}
}

type nowSeen struct {
	Call    *struct{ Name string } `json:"call"`
	Kind    string                 `json:"kind"`
	Running bool                   `json:"running"`
	Since   float64                `json:"since"`
	Kinds   []struct {
		Kind   string `json:"kind"`
		Count  int    `json:"count"`
		Failed int    `json:"failed"`
	} `json:"kinds"`
	Think *struct{ Count int } `json:"think"`
	Run   *float64             `json:"run"`
}

// The bar above the composer reads the turn going on, and only it: its last
// run, the latest call of that run and whether the host says it is still out.
// A call that came back leaves the session thinking since the last thing it
// did; a message still in the queue has not begun a turn; a run of a turn
// before is not what the session does now.
func TestTheBarSaysTheCallGoingOutNow(t *testing.T) {
	at := func(sec int) string { return "2026-09-27T12:00:" + twoDigits(sec) + ".000Z" }
	me := map[string]any{"role": "me", "text": "go", "at": at(0), "pos": 1}
	run := func(calls ...map[string]any) map[string]any {
		out := make([]any, len(calls))
		for i, c := range calls {
			out[i] = c
		}
		return map[string]any{"role": "tools", "kind": "bash", "run": 5, "pos": 5, "at": at(1), "calls": out}
	}
	call := func(name string, pos, sec int, marks map[string]any) map[string]any {
		c := map[string]any{"name": name, "arg": name + " arg", "pos": pos, "index": 0, "at": at(sec)}
		for k, v := range marks {
			c[k] = v
		}
		return c
	}
	think := map[string]any{"role": "think", "run": 5, "pos": 4, "at": at(1), "count": 2, "tokens": 900}
	said := map[string]any{"role": "ai", "text": "done", "at": at(20), "pos": 9}
	queued := map[string]any{"role": "me", "text": "later", "state": "queued", "at": at(30), "pos": 10}
	turn := map[string]any{"role": "turn", "at": at(25), "pos": 11}
	files := map[string]any{"role": "tools", "kind": "files", "run": 5, "pos": 6, "at": at(3),
		"calls": []any{call("Read", 6, 3, map[string]any{"failed": true})}}

	cases := [][]any{
		{[]any{me, think, run(call("Bash", 5, 2, nil), call("Bash", 7, 10, map[string]any{"open": true})), files}},
		{[]any{me, think, run(call("Bash", 5, 2, nil), call("Bash", 7, 10, nil)), said}},
		{[]any{me, run(call("Bash", 5, 2, map[string]any{"open": true})), queued}},
		{[]any{me}},
		{[]any{me, run(call("Bash", 5, 2, nil)), said, turn, map[string]any{"role": "me", "text": "next", "at": at(40), "pos": 12}}},
		{[]any{me, run(call("Bash", 5, 2, nil)), said, turn}},
	}
	raw := runModuleJS(t, "src/screens/chat/now.js", "nowOf", cases)
	got := make([]nowSeen, len(raw))
	for i, r := range raw {
		decodeInto(t, r, &got[i])
	}
	sec := func(s int) float64 { return float64(time.Date(2026, 9, 27, 12, 0, s, 0, time.UTC).UnixMilli()) }

	if g := got[0]; !g.Running || g.Call == nil || g.Call.Name != "Bash" || g.Since != sec(10) {
		t.Errorf("the call the host says is out is not the one running since it went out: %+v", g)
	}
	if g := got[0]; len(g.Kinds) != 2 || g.Kinds[0].Count != 2 || g.Kinds[1].Kind != "files" || g.Kinds[1].Failed != 1 || g.Think == nil || g.Think.Count != 2 {
		t.Errorf("the badges of the run are %+v / think %+v — expected two commands, a failed read and two thoughts", g.Kinds, g.Think)
	}
	if g := got[1]; g.Running || g.Call == nil || g.Since != sec(20) {
		t.Errorf("with its call back the session is taken as running, or not thinking since the last thing it did: %+v", g)
	}
	if g := got[2]; !g.Running {
		t.Errorf("a message waiting in the queue ended the turn going on: %+v", g)
	}
	if g := got[3]; g.Call != nil || g.Running || g.Since != sec(0) {
		t.Errorf("a turn with no call yet says %+v, expected thinking since the message", g)
	}
	if g := got[4]; g.Call != nil || g.Run != nil {
		t.Errorf("the run of the turn before is shown as what the session does now: %+v", g)
	}
	// A session woken after its turn ended, before anything of the new turn
	// is in the feed, has not made a call in it yet.
	if g := got[5]; g.Call != nil || g.Run != nil || g.Since != sec(25) {
		t.Errorf("a turn begun after the end of the last one shows %+v, expected thinking since that end", g)
	}
}

// nowShape is how one bar is laid out: what its line says, its state word
// and clock, the parts of the line in order and what each says, its height in
// lines of its own font, and the size and place of the clock against the line.
type nowShape struct {
	Top       string   `json:"top"`
	State     string   `json:"state"`
	Clock     string   `json:"clock"`
	Order     []string `json:"order"`
	Parts     []string `json:"parts"`
	Lines     float64  `json:"lines"`
	ClockSize string   `json:"clockSize"`
	ClockGap  int      `json:"clockGap"`
	LineGap   float64  `json:"lineGap"`
}

type nowBarSeen struct {
	Top      string `json:"top"`
	Arg      string `json:"arg"`
	Badges   int    `json:"badges"`
	Failed   int    `json:"failed"`
	Opened   string `json:"opened"`
	Old      bool   `json:"old"`
	Overflow int    `json:"overflow"`
	Call     struct {
		Tag    string `json:"tag"`
		Nested bool   `json:"nested"`
		Label  string `json:"label"`
		Text   string `json:"text"`
	} `json:"call"`
	Called        string   `json:"called"`
	Running       nowShape `json:"running"`
	Back          nowShape `json:"back"`
	Thinking      nowShape `json:"thinking"`
	ThinkChipsGap int      `json:"thinkChipsGap"`
	Narrow        struct {
		nowShape
		Text             string    `json:"text"`
		Cut              bool      `json:"cut"`
		Ellipsis         string    `json:"ellipsis"`
		NameWhole        bool      `json:"nameWhole"`
		StateWhole       bool      `json:"stateWhole"`
		ClockWhole       bool      `json:"clockWhole"`
		ClockWidth       []float64 `json:"clockWidth"`
		RoomyClockWidth  []float64 `json:"roomyClockWidth"`
		BadgeWidths      []float64 `json:"badgeWidths"`
		RoomyBadgeWidths []float64 `json:"roomyBadgeWidths"`
		ChipsOut         int       `json:"chipsOut"`
	} `json:"narrow"`
	Error string `json:"error"`
}

func seeNowBar(t *testing.T) nowBarSeen {
	t.Helper()
	var got nowBarSeen
	runFixture(t, "nowbar.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	return got
}

// stopwatch is a clock that says the time alone: the state beside it says
// what it counts, so no word stands in front of the digits.
var stopwatch = regexp.MustCompile(`^\d+:\d\d(:\d\d)?$`)

// While a session is at work the bar says whether it is running a call or
// thinking between calls, how long it has stood so, the call — the one out or
// the last one back — by its name and argument, and the badges of its run,
// which open its calls. The call is a button of its own that hands over the
// very call it names.
func TestTheBarAboveTheComposerSaysWhatIsGoingOn(t *testing.T) {
	got := seeNowBar(t)
	for _, want := range []string{"running", "0:1", "Bash", "go test ./web/check/"} {
		if !strings.Contains(got.Top, want) {
			t.Errorf("the bar says %q, without %q", got.Top, want)
		}
	}
	for name, s := range map[string]nowShape{"a call out": got.Running, "the last call back": got.Back, "no call yet": got.Thinking} {
		for _, part := range s.Parts {
			if part == "now" || strings.Contains(part, "last call") {
				t.Errorf("%s: the bar still says %q: %q", name, part, s.Parts)
			}
		}
		if !stopwatch.MatchString(s.Clock) {
			t.Errorf("%s: the clock reads %q, expected the time alone", name, s.Clock)
		}
	}
	if got.Running.State != "running" || got.Back.State != "thinking" || got.Thinking.State != "thinking" {
		t.Errorf("the state reads %q with a call out, %q with it back, %q before any call — expected running, thinking, thinking",
			got.Running.State, got.Back.State, got.Thinking.State)
	}
	if !strings.Contains(got.Back.Top, "Bash journalctl -u aacpanel -n 50") {
		t.Errorf("thinking after a call came back, the bar says %q, expected the last call", got.Back.Top)
	}
	if got.Arg != "go test ./web/check/" {
		t.Errorf("the bar shows the command %q", got.Arg)
	}
	if got.Badges != 2 || got.Failed != 1 {
		t.Errorf("the bar carries %d badges, %d marked failed — expected the commands and the files, the read failed", got.Badges, got.Failed)
	}
	if got.Opened != "5" {
		t.Errorf("a badge of the bar opened the calls of run %q, expected the run going on", got.Opened)
	}
	c := got.Call
	if c.Tag != "BUTTON" || c.Nested || c.Label != "open the call Bash" || c.Text != "Bash go test ./web/check/" {
		t.Errorf("the call in the bar is %+v, expected a button of its own named after the call, its name and argument", c)
	}
	if got.Called != "5:6:0" {
		t.Errorf("a press on the call handed over %q, expected run 5 and the command at 6", got.Called)
	}
	if got.Old {
		t.Error("the bar still says only that the request is being handled")
	}
	if got.Overflow > 0 {
		t.Errorf("the bar pushes the phone %dpx sideways", got.Overflow)
	}
	if got.ThinkChipsGap < 0 || got.ThinkChipsGap > 24 {
		t.Errorf("with little said, the badges stand %dpx off the far end of the bar — the bar shrank to its words", got.ThinkChipsGap)
	}
}

// The bar is one line: the pulsing dot, the state, the clock beside it in the
// size of the line, the call after a separator and the badges at the end —
// with a call out, with the last one back, before any call and on a narrow
// phone alike. Its content is about one line of its own font high, never two.
func TestTheBarAboveTheComposerIsOneLine(t *testing.T) {
	got := seeNowBar(t)
	full := []string{"nowdot", "nowstate", "nowel", "nowsep", "nowcall", "nowchips"}
	for name, s := range map[string]struct {
		nowShape
		order []string
	}{
		"a call out":         {got.Running, full},
		"the last call back": {got.Back, full},
		"no call yet":        {got.Thinking, []string{"nowdot", "nowstate", "nowel", "nowchips"}},
		"a phone 360px wide": {got.Narrow.nowShape, full},
	} {
		if strings.Join(s.Order, " ") != strings.Join(s.order, " ") {
			t.Errorf("%s: the line is laid out %v, expected %v", name, s.Order, s.order)
		}
		if s.Lines <= 0 || s.Lines >= 1.5 {
			t.Errorf("%s: the bar is %.2f lines of its font high, expected one", name, s.Lines)
		}
		if size := strings.Fields(s.ClockSize); len(size) != 2 || size[0] != size[1] {
			t.Errorf("%s: the clock is set at %v, the line at the second — expected the size of the line", name, size)
		}
		if s.ClockGap < 0 || float64(s.ClockGap) > s.LineGap+1 {
			t.Errorf("%s: the clock stands %dpx after the state, the line's gap is %.1fpx — expected beside it", name, s.ClockGap, s.LineGap)
		}
	}
}

// On a phone 360px wide a long command gives way first: its name and argument
// are one string cut with an ellipsis, while the state, the clock and the
// badges stay whole and as wide as on a bar with all the room it wants.
func TestTheCallGivesWayOnANarrowPhone(t *testing.T) {
	n := seeNowBar(t).Narrow
	if !strings.HasPrefix(n.Text, "Bash ssh -p 69 -o BatchMode=yes") || !n.Cut || n.Ellipsis != "ellipsis" || !n.NameWhole {
		t.Errorf("the call reads %q (cut %v, %q, name whole %v), expected the name and the argument cut with an ellipsis",
			n.Text, n.Cut, n.Ellipsis, n.NameWhole)
	}
	if !n.StateWhole || n.State != "running" {
		t.Errorf("the state gave way before the call did: %q, whole %v", n.State, n.StateWhole)
	}
	if !n.ClockWhole || len(n.ClockWidth) != 1 || len(n.RoomyClockWidth) != 1 || math.Abs(n.ClockWidth[0]-n.RoomyClockWidth[0]) > 0.5 {
		t.Errorf("the clock is %v wide on the narrow phone and %v with room (whole %v)", n.ClockWidth, n.RoomyClockWidth, n.ClockWhole)
	}
	if len(n.BadgeWidths) != 3 || len(n.RoomyBadgeWidths) != 3 {
		t.Fatalf("the badges are %v and %v, expected the thought, the read and the command", n.BadgeWidths, n.RoomyBadgeWidths)
	}
	for i := range n.BadgeWidths {
		if math.Abs(n.BadgeWidths[i]-n.RoomyBadgeWidths[i]) > 0.5 {
			t.Errorf("badge %d is %.1fpx wide on the narrow phone and %.1fpx with room", i, n.BadgeWidths[i], n.RoomyBadgeWidths[i])
		}
	}
	if n.ChipsOut > 0 {
		t.Errorf("the badges stand %dpx out of the bar", n.ChipsOut)
	}
}

// The call the bar names opens itself on the screen of the conversation: the
// calls sheet of the run comes up on that very call, its arrow and the back
// gesture both lead to the list of the run with the sheet still open, and a
// sheet shut over another call and opened again from the bar starts on the
// bar's call. A badge of the bar opens the list, as it always has.
func TestTheCallInTheBarOpensItselfInTheCallsOfTheRun(t *testing.T) {
	type sheetSeen struct {
		Open  bool     `json:"open"`
		View  bool     `json:"view"`
		Title string   `json:"title"`
		Place string   `json:"place"`
		Rows  []string `json:"rows"`
	}
	var got struct {
		Call     sheetSeen `json:"call"`
		Asked    []string  `json:"asked"`
		Arrow    sheetSeen `json:"arrow"`
		Again    sheetSeen `json:"again"`
		Gesture  sheetSeen `json:"gesture"`
		Badge    sheetSeen `json:"badge"`
		Overflow int       `json:"overflow"`
		Error    string    `json:"error"`
	}
	runFixture(t, "nowcall.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if c := got.Call; !c.Open || !c.View || c.Title != "Bash" || c.Place != "2 of 2" {
		t.Errorf("a press on the call in the bar opened %+v, expected the command, the second of two calls", c)
	}
	if strings.Join(got.Asked, ",") != "6:0" {
		t.Errorf("the call opened asked the host for %v, expected the command at 6", got.Asked)
	}
	list := []string{"thinking", "Read", "Bash"}
	for name, s := range map[string]sheetSeen{"its arrow": got.Arrow, "the back gesture": got.Gesture} {
		if !s.Open || s.View || s.Title != "Calls" || strings.Join(s.Rows, ",") != strings.Join(list, ",") {
			t.Errorf("%s over the call leads to %+v, expected the list of the run in the open sheet", name, s)
		}
	}
	if a := got.Again; !a.Open || !a.View || a.Title != "Bash" {
		t.Errorf("opened again from the bar after another call, the sheet shows %+v, expected the command", a)
	}
	if b := got.Badge; !b.Open || b.View || b.Title != "Calls" || strings.Join(b.Rows, ",") != strings.Join(list, ",") {
		t.Errorf("a badge of the bar opened %+v, expected the list of the run", b)
	}
	if got.Overflow > 0 {
		t.Errorf("the screen pushes the phone %dpx sideways", got.Overflow)
	}
}

// claude keeps a session on the stream busy while the agents it sent off work,
// long after its own turn ended. The bar then says that work — how many are at
// it and since when the first began — and opens its list, rather than counting
// a thought that ended with the answer or naming the last call of that answer.
// It reads as the bar of a turn does — the words, then the clock beside them,
// on one line however narrow the screen. With nothing the panel sees at work
// it says nothing; the header says the session waits on its agents rather
// than that it is answering.
func TestTheBarOfATurnThatEndedSaysTheAgentsAtWork(t *testing.T) {
	type waitShape struct {
		Order      []string `json:"order"`
		Words      string   `json:"words"`
		Clock      string   `json:"clock"`
		Lines      float64  `json:"lines"`
		ClockGap   int      `json:"clockGap"`
		LineGap    float64  `json:"lineGap"`
		ClockWhole bool     `json:"clockWhole"`
		Inside     bool     `json:"inside"`
	}
	var got struct {
		WaitTop     string    `json:"waitTop"`
		WaitAll     string    `json:"waitAll"`
		WaitButton  bool      `json:"waitIsButton"`
		WaitFace    string    `json:"waitFace"`
		WaitShort   int       `json:"waitShort"`
		Wait        waitShape `json:"wait"`
		Narrow      waitShape `json:"narrow"`
		FlowsTop    string    `json:"flowsTop"`
		Opened      []string  `json:"opened"`
		None        string    `json:"none"`
		GoingTop    string    `json:"goingTop"`
		Overflow    int       `json:"overflow"`
		HeadOver    string    `json:"headOver"`
		HeadBusy    string    `json:"headBusy"`
		HeadWaiting string    `json:"headWaiting"`
		Error       string    `json:"error"`
	}
	runFixture(t, "waitbar.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	for _, want := range []string{"2 agents working", "6:4"} {
		if !strings.Contains(got.WaitTop, want) {
			t.Errorf("the bar of a session waiting on its agents says %q, without %q", got.WaitTop, want)
		}
	}
	if got.Wait.Words != "2 agents working" || !stopwatch.MatchString(got.Wait.Clock) {
		t.Errorf("the bar of a session waiting on its agents says %q and %q, expected its words and the time alone",
			got.Wait.Words, got.Wait.Clock)
	}
	for _, stale := range []string{"thinking", "last call", "TaskStop"} {
		if strings.Contains(got.WaitAll, stale) {
			t.Errorf("the bar of a turn that ended still says %q: %q", stale, got.WaitAll)
		}
	}
	if !got.WaitButton {
		t.Error("the bar of the agents at work does not open their list")
	}
	if got.WaitFace != "rgba(0, 0, 0, 0) 0px" {
		t.Errorf("the bar wears the face of a button of its own: %q", got.WaitFace)
	}
	if got.WaitShort < 0 || got.WaitShort > 1 {
		t.Errorf("the button is %dpx narrower than the card it stands in — it shrank to its words", got.WaitShort)
	}
	for name, s := range map[string]waitShape{"on the phone": got.Wait, "200px wide": got.Narrow} {
		if strings.Join(s.Order, " ") != "nowdot nowkind nowstate nowel" {
			t.Errorf("%s: the bar is laid out %v, expected the dot, the icon, the words and the clock", name, s.Order)
		}
		if s.Lines <= 0 || s.Lines >= 1.5 {
			t.Errorf("%s: the bar is %.2f lines of its font high, expected one", name, s.Lines)
		}
		if s.ClockGap < 0 || float64(s.ClockGap) > s.LineGap+1 || !s.ClockWhole || !s.Inside {
			t.Errorf("%s: the clock stands %dpx after the words (gap %.1fpx), whole %v, inside the bar %v",
				name, s.ClockGap, s.LineGap, s.ClockWhole, s.Inside)
		}
	}
	if !strings.Contains(got.FlowsTop, "1 workflow running") {
		t.Errorf("with no agent at work and a workflow running the bar says %q", got.FlowsTop)
	}
	if strings.Join(got.Opened, ",") != "agents,workflows" {
		t.Errorf("a tap on the bar opened %v, expected the agents, then the workflows", got.Opened)
	}
	if got.None != "" {
		t.Errorf("with nothing at work that the panel sees, the bar still stands: %s", got.None)
	}
	if !strings.Contains(got.GoingTop, "thinking") {
		t.Errorf("a turn going on no longer says it is thinking: %q", got.GoingTop)
	}
	if got.Overflow > 0 {
		t.Errorf("the bar pushes the phone %dpx sideways", got.Overflow)
	}
	if got.HeadOver != "agents at work" || got.HeadBusy != "answering" || got.HeadWaiting != "waiting for you" {
		t.Errorf("the header says %q past the end of the turn, %q in it, %q with a dialog open",
			got.HeadOver, got.HeadBusy, got.HeadWaiting)
	}
}

// The same on the screen of the conversation: the collector marks a session
// on the stream whose turn is over, the screen hands the mark to the bar and
// the header, the counter under the composer counts the agents at work and not
// the one stopped, and a tap on the bar opens the list of the agents.
func TestTheConversationWaitingOnItsAgentsSaysSo(t *testing.T) {
	var got struct {
		Bar      string `json:"bar"`
		Word     string `json:"word"`
		Robots   string `json:"robots"`
		Sheet    string `json:"sheet"`
		SheetSub string `json:"sheetSub"`
		Error    string `json:"error"`
	}
	runFixture(t, "waitchat.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !strings.Contains(got.Bar, "2 agents working") || strings.Contains(got.Bar, "thinking") {
		t.Errorf("the bar above the composer says %q", got.Bar)
	}
	if got.Word != "agents at work" {
		t.Errorf("the header says %q", got.Word)
	}
	if got.Robots != "2" {
		t.Errorf("the counter of agents says %q, expected the two at work", got.Robots)
	}
	if got.Sheet != "subagents" || !strings.Contains(got.SheetSub, "2 working") || !strings.Contains(got.SheetSub, "1 over") {
		t.Errorf("a tap on the bar opened %q (%q), expected the list of the agents", got.Sheet, got.SheetSub)
	}
}
