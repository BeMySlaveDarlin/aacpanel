package check

import (
	"reflect"
	"strings"
	"testing"
)

type codeRunStyle struct {
	PreWhiteSpace, CodeWhiteSpace, PaddingRight, OverflowX, OverflowWrap string
}

type codeRunShot struct {
	Buttons  []bool
	Outside  int
	Draft    string
	Geometry []struct {
		Clear, Aligned, Centered, Inside, OneLine bool
		Top, Gap, Edge, RunHeight, CopyHeight     float64
		Label, Title                              string
		Style, PlainStyle                         codeRunStyle
	}
	Runs []struct {
		Index                                           int
		Preview, Title, Going, Local, Output, Font, Yes string
		Before, BeforeRows, Ended                       int
		Wraps, Scrolls, Bounded                         bool
		Sends                                           []struct {
			Kind, Target string
			Params       struct {
				Text, MessageID string
			}
		}
	}
	Cancelled struct{ Sends, Rows int }
	Absent    map[string]int
}

func TestCodeBlocksRunThroughTheSessionGate(t *testing.T) {
	var got codeRunShot
	runFixtureOn(t, "coderun.html", `{"width":390,"height":852,"deviceScaleFactor":2,"mobile":true}`, phonePointer, &got)
	checkCodeRun(t, got)
}

func TestCodeBlocksRunThroughTheSessionGateAtADesk(t *testing.T) {
	var got codeRunShot
	runWideFixture(t, "coderun.html", &got)
	checkCodeRun(t, got)
}

func checkCodeRun(t *testing.T, got codeRunShot) {
	t.Helper()
	want := []bool{true, true, true, false, false, true, true, true, true, true, true, false, true}
	if !reflect.DeepEqual(got.Buttons, want) {
		t.Errorf("runnable fences = %v, want %v", got.Buttons, want)
	}
	if got.Outside != 0 || len(got.Absent) != 6 {
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
		5: "printf bang", 9: "python3 - <<'PY_2'\nPY\nPY_1\nprint('done')\nPY_2",
	}
	if len(got.Runs) != 6 {
		t.Fatalf("ran %d fences, want 6", len(got.Runs))
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
		if !r.Wraps || !r.Scrolls || !r.Bounded || !strings.Contains(strings.ToLower(r.Font), "mono") {
			t.Errorf("block %d preview cannot be read whole: %+v", r.Index, r)
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
	}
	if len(got.Geometry) != 10 {
		t.Fatalf("measured %d button pairs, want 10", len(got.Geometry))
	}
	for i, b := range got.Geometry {
		if !b.Aligned || !b.Inside || b.RunHeight != b.CopyHeight || b.Gap < 2 || b.Gap > 8 || b.Edge < 2 || b.Edge > 8 {
			t.Errorf("button pair %d does not fit in the block: %+v", i, b)
		}
		if b.OneLine {
			if !b.Clear || !b.Centered {
				t.Errorf("one-line button pair %d overlaps the code or is not centered: %+v", i, b)
			}
		} else {
			if b.Style != b.PlainStyle || b.Style.CodeWhiteSpace != "pre" || b.Style.OverflowX != "auto" {
				t.Errorf("multiline block %d differs from the same block without Run: %+v versus %+v", i, b.Style, b.PlainStyle)
			}
			if b.Top < 2 || b.Top > 8 {
				t.Errorf("multiline button pair %d is %.1fpx below the top corner", i, b.Top)
			}
		}
		if b.Label != "Run in the session" || b.Title != b.Label {
			t.Errorf("button pair %d has label %q and title %q", i, b.Label, b.Title)
		}
	}
}
