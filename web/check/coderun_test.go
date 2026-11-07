package check

import (
	"reflect"
	"strings"
	"testing"
)

type codeRunShot struct {
	Buttons []bool
	Outside int
	Draft   string
	Pairs   []struct {
		Label, Title string
	}
	Runs []struct {
		Index                                     int
		Preview, Title, Going, Local, Output, Yes string
		BarGoing, BarRunning, BarEnded            string
		Before, BeforeRows, Ended                 int
		Sends                                     []struct {
			Kind, Target string
			Params       struct {
				Text, MessageID string
			}
		}
	}
	Cancelled           struct{ Sends, Rows int }
	BarTakesDown        bool
	BarGoneAfterMessage bool
	Absent              map[string]int
	Codex               map[string][]bool
}

func TestCodeBlocksRunThroughTheSessionGate(t *testing.T) {
	parallel(t)
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got codeRunShot
			screen.run(t, "coderun.html", &got)
			checkCodeRun(t, got)
		})
	}
}

func checkCodeRun(t *testing.T, got codeRunShot) {
	t.Helper()
	want := []bool{true, true, true, false, false, true, true, true, true, true, true, false, true, true}
	if !reflect.DeepEqual(got.Buttons, want) {
		t.Errorf("runnable fences = %v, want %v", got.Buttons, want)
	}
	if len(got.Codex) != 2 {
		t.Errorf("codex transports checked = %v", got.Codex)
	}
	for transport, buttons := range got.Codex {
		if !reflect.DeepEqual(buttons, want) {
			t.Errorf("codex %s runnable fences = %v, expected %v", transport, buttons, want)
		}
	}
	if got.Outside != 0 || len(got.Absent) != 5 {
		t.Errorf("buttons outside the feed = %d; unavailable sessions checked = %v", got.Outside, got.Absent)
	}
	for name, count := range got.Absent {
		if count != 0 {
			t.Errorf("%s has %d run buttons", name, count)
		}
	}
	if got.Cancelled.Sends != 0 || got.Cancelled.Rows != 0 {
		t.Errorf("Cancel sent an action or added a local row: %+v", got.Cancelled)
	}
	if got.Draft != "keep my draft" {
		t.Errorf("running a fence changed the draft to %q", got.Draft)
	}
	commands := map[int]string{
		0: "printf ready", 1: "printf plain", 2: "python3 - <<'PY'\nif True:\n    print('$HOME')\nPY",
		5: "printf bang", 13: "printf request", 9: "python3 - <<'PY_2'\nPY\nPY_1\nprint('done')\nPY_2",
	}
	if len(got.Runs) != 7 {
		t.Fatalf("ran %d fences, want 7", len(got.Runs))
	}
	for _, r := range got.Runs {
		if want, ok := commands[r.Index]; ok && r.Preview != want {
			t.Errorf("block %d preview = %q, want %q", r.Index, r.Preview, want)
		}
		if r.Index == 12 && (!strings.HasSuffix(r.Preview, "printf 'line 44'") || len(r.Preview) < 900) {
			t.Errorf("long command was cut: %q", r.Preview)
		}
		if r.Title != "Run in demo?" || r.Yes != "Run" || r.Before != 0 || r.BeforeRows != 0 {
			t.Errorf("block %d did not wait at its gate: %+v", r.Index, r)
		}
		if len(r.Sends) != 1 {
			t.Errorf("block %d sent %d actions", r.Index, len(r.Sends))
			continue
		}
		s := r.Sends[0]
		if s.Kind != "session.shell" || s.Target != "demo" || s.Params.Text != r.Preview || !uuidShape.MatchString(s.Params.MessageID) {
			t.Errorf("block %d sent %+v instead of its preview %q", r.Index, s, r.Preview)
		}
		if r.Local != r.Preview || r.Going != "starting" || r.Ended != 1 || r.Output != "finished" {
			t.Errorf("block %d did not follow the command in one shell card: %+v", r.Index, r)
		}
		// The bar above the composer says what the card says, at every step:
		// the command started from far up the feed is followed from there.
		command := "!" + strings.Join(strings.Fields(r.Preview), " ")
		for step, bar := range map[string]string{"starting": r.BarGoing, "running": r.BarRunning, "exit 0": r.BarEnded} {
			if !strings.HasPrefix(bar, command) || !strings.Contains(bar[len(command):], step) {
				t.Errorf("block %d: the bar over the composer reads %q, want the command and %q", r.Index, bar, step)
			}
		}
	}
	if !got.BarTakesDown || !got.BarGoneAfterMessage {
		t.Errorf("the command bar: takes the feed down %v, leaves after the next message %v", got.BarTakesDown, got.BarGoneAfterMessage)
	}
	if len(got.Pairs) != 10 {
		t.Fatalf("read %d run buttons, want 10", len(got.Pairs))
	}
	for i, b := range got.Pairs {
		if b.Label != "Run in the session" || b.Title != b.Label {
			t.Errorf("button pair %d has label %q and title %q", i, b.Label, b.Title)
		}
	}
}
