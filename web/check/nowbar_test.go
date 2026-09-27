package check

import (
	"encoding/json"
	"fmt"
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

// While a session is at work the bar says the call going out, how long it has
// been out, its command, and the badges of its run, which open its calls — in
// place of a line that said only that the request was being handled.
func TestTheBarAboveTheComposerSaysWhatIsGoingOn(t *testing.T) {
	var got struct {
		Top      string `json:"top"`
		Arg      string `json:"arg"`
		Badges   int    `json:"badges"`
		Failed   int    `json:"failed"`
		Opened   string `json:"opened"`
		Old      bool   `json:"old"`
		Overflow int    `json:"overflow"`
		Error    string `json:"error"`
	}
	runFixture(t, "nowbar.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	for _, want := range []string{"now", "Bash", "commands", "running", "0:1"} {
		if !strings.Contains(got.Top, want) {
			t.Errorf("the bar says %q, without %q", got.Top, want)
		}
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
	if got.Old {
		t.Error("the bar still says only that the request is being handled")
	}
	if got.Overflow > 0 {
		t.Errorf("the bar pushes the phone %dpx sideways", got.Overflow)
	}
}
