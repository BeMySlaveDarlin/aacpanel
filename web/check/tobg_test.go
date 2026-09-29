package check

import (
	"strings"
	"testing"
)

type pill struct {
	Text string `json:"text"`
	Off  bool   `json:"off"`
	Why  string `json:"why"`
}

// A call the turn waits on goes to the background from where the session's
// work is shown, as Ctrl+B moves it in the terminal. On the stream the call
// going out has a button of its own and the calls of the run go all at once;
// in the console, where one key moves every call, only all at once is offered.
// A host that does not know the move says so on the button; a call that
// cannot go — a read — and a session the panel does not reach have none. In
// the calls sheet the button stands beside the row of a call, not inside the
// button the row is, and the call opened from its row carries it too.
func TestACallInTheForegroundGoesToTheBackgroundFromTheBar(t *testing.T) {
	var got struct {
		Stream    []pill `json:"stream"`
		Console   []pill `json:"console"`
		Old       []pill `json:"old"`
		Reading   []pill `json:"reading"`
		Away      []pill `json:"away"`
		SheetRows []struct {
			Name   string `json:"name"`
			Pill   string `json:"pill"`
			Nested bool   `json:"nested"`
		} `json:"sheetRows"`
		ConsoleSheet []pill `json:"consoleSheet"`
		Overflow     int    `json:"overflow"`
		ArgWidth     int    `json:"argWidth"`
		ChipsOut     int    `json:"chipsOut"`
		AfterOne     pill   `json:"afterOne"`
		Sent         []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
		View  []pill `json:"view"`
		Error string `json:"error"`
	}
	runFixture(t, "tobg.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}

	if len(got.Stream) != 2 || got.Stream[0].Text != "to background" || got.Stream[1].Text != "all 2 to background" ||
		got.Stream[0].Off || got.Stream[1].Off {
		t.Errorf("the bar of a session on the stream offers %+v: the command going out, and both calls at once", got.Stream)
	}
	if len(got.Console) != 1 || got.Console[0].Text != "all 2 to background" {
		t.Errorf("the bar of a session in the console offers %+v: one key moves every call there", got.Console)
	}
	if len(got.Old) != 2 || !got.Old[0].Off || !strings.Contains(got.Old[0].Why, "session.background") {
		t.Errorf("the bar on a host that does not know the move offers %+v", got.Old)
	}
	if len(got.Reading) != 0 {
		t.Errorf("a read going out offers %+v", got.Reading)
	}
	if len(got.Away) != 0 {
		t.Errorf("a session the panel does not reach offers %+v", got.Away)
	}

	rows := map[string]string{}
	for _, r := range got.SheetRows {
		rows[r.Name] = r.Pill
		if r.Nested {
			t.Errorf("the button of %s stands inside the button of its row", r.Name)
		}
	}
	if rows["Bash"] != "to background" || rows["Agent"] != "to background" || rows["Read"] != "" || len(rows) != 3 {
		t.Errorf("the calls sheet offers %v: the command and the subagent the turn waits on, not the read", rows)
	}
	if len(got.ConsoleSheet) != 0 {
		t.Errorf("the calls sheet of a session in the console aims at one call: %+v", got.ConsoleSheet)
	}
	if got.Overflow > 0 {
		t.Errorf("the buttons push the phone %dpx sideways", got.Overflow)
	}
	if got.ArgWidth < 120 || got.ChipsOut > 0 {
		t.Errorf("the buttons squeeze the command to %dpx and push the badges %dpx out of the bar", got.ArgWidth, got.ChipsOut)
	}

	if len(got.Sent) != 3 {
		t.Fatalf("the presses sent %+v", got.Sent)
	}
	one, all, console := got.Sent[0], got.Sent[1], got.Sent[2]
	if one.Kind != "session.background" || one.Target != "lab" || one.Params["use"] != "toolu_01Fg" || len(one.Params) != 1 {
		t.Errorf("the button of the command sent %+v: the command by the id of its call", one)
	}
	if all.Kind != "session.background" || len(all.Params) != 0 || len(console.Params) != 0 {
		t.Errorf("all at once sent %+v and %+v: no call named", all, console)
	}
	if got.AfterOne.Text != "in background" || !got.AfterOne.Off {
		t.Errorf("the button after the move reads %+v", got.AfterOne)
	}
	if len(got.View) != 1 || got.View[0].Text != "to background" {
		t.Errorf("the call opened from its row offers %+v", got.View)
	}
}
