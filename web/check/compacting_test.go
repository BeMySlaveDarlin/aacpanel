package check

import (
	"strings"
	"testing"
)

type compactLine struct {
	Text   string `json:"text"`
	NowTop string `json:"nowTop"`
	Pct    string `json:"pct"`
	Now    string `json:"now"`
	Fill   string `json:"fill"`
}

type compactingShot struct {
	Compacting   compactLine `json:"compacting"`
	Busy         compactLine `json:"busy"`
	Idle         string      `json:"idle"`
	SkewedBefore compactLine `json:"skewedBefore"`
	SkewedAfter  compactLine `json:"skewedAfter"`
	Again        compactLine `json:"again"`
	Pcts         []int       `json:"pcts"`
}

func compactingFixture(t *testing.T) compactingShot {
	t.Helper()
	var got compactingShot
	runFixture(t, "compacting.html", &got)
	return got
}

// While a session compacts, the line above the composer says so, with the time
// it has taken and the share a terminal would show for that time — rather
// than the call before it, as if that were still going.
func TestACompactionIsSaidAboveTheComposer(t *testing.T) {
	got := compactingFixture(t)
	if got.Compacting.Text != "compacting the conversation… (54s)" {
		t.Errorf("the line of a compaction says %q", got.Compacting.Text)
	}
	if got.Compacting.Pct != "45%" || got.Compacting.Now != "45" || got.Compacting.Fill != "45%" {
		t.Errorf("fifty-four seconds in, the share is %q (aria %q, bar %q), the terminal shows 45%%",
			got.Compacting.Pct, got.Compacting.Now, got.Compacting.Fill)
	}
	if got.Busy.NowTop != "thinking" || got.Busy.Text != "" || got.Busy.Pct != "" {
		t.Errorf("a session at work with no call yet says %q / %q / %q, expected that it is thinking", got.Busy.NowTop, got.Busy.Text, got.Busy.Pct)
	}
	if got.Idle != "" {
		t.Errorf("a session that neither works nor compacts shows a line: %s", got.Idle)
	}
}

// The share is the terminal's: a guess from the time alone that climbs fast,
// slows down and stops short of the end.
func TestTheShareOfACompactionIsTheTerminals(t *testing.T) {
	got := compactingFixture(t)
	want := []int{0, 45, 63, 86, 95, 0}
	if len(got.Pcts) != len(want) {
		t.Fatalf("shares: %v", got.Pcts)
	}
	for i, w := range want {
		if got.Pcts[i] != w {
			t.Errorf("shares for 0, 54, 90, 180, 1000 and -5 seconds are %v, want %v", got.Pcts, want)
			break
		}
	}
}

// A clock of the phone set back pulls the time said back with it, but not the
// bar: a bar that shrinks under the eye reads as the work going backwards.
func TestTheShareOfACompactionNeverDrops(t *testing.T) {
	got := compactingFixture(t)
	if got.SkewedBefore.Pct != "45%" {
		t.Fatalf("before the clock went back the share is %q", got.SkewedBefore.Pct)
	}
	if got.SkewedAfter.Pct != "45%" {
		t.Errorf("the clock went back and the share dropped to %q", got.SkewedAfter.Pct)
	}
	if !strings.Contains(got.SkewedAfter.Text, "(2") {
		t.Errorf("the time said did not follow the clock back: %q", got.SkewedAfter.Text)
	}
}

// Another compaction of the same session starts from nothing, not from where
// the last one stopped.
func TestANewCompactionStartsFromNothing(t *testing.T) {
	got := compactingFixture(t)
	if got.Again.Pct != "6%" && got.Again.Pct != "5%" {
		t.Errorf("a compaction five seconds old shows %q", got.Again.Pct)
	}
}
