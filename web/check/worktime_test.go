package check

import "testing"

type workTimeShot struct {
	Live         string `json:"live"`
	Over         string `json:"over"`
	MissedTheEnd string `json:"missedTheEnd"`
}

// Work that is over says how long it took. The age of its beginning is a
// different number and the wrong one: a run of twelve minutes last night would
// read as twelve hours by morning, and a list of finished runs would say every
// one of them took all night.
func TestFinishedWorkSaysHowLongItTook(t *testing.T) {
	var got workTimeShot
	runFixture(t, "worktime.html", &got)

	if got.Over != "12 min" {
		t.Errorf("a run that took twelve minutes says %q", got.Over)
	}
}

// Work still running has no length yet, so the row says how long it has been
// going — that is the number somebody waiting on it wants.
func TestRunningWorkSaysHowLongItHasBeenGoing(t *testing.T) {
	var got workTimeShot
	runFixture(t, "worktime.html", &got)

	if got.Live != "5 min" {
		t.Errorf("a run started five minutes ago says %q", got.Live)
	}
}

// When the end was never seen there is nothing to measure, and the row falls
// back to the age rather than showing a dash or a zero.
func TestWorkWhoseEndWasMissedFallsBackToItsAge(t *testing.T) {
	var got workTimeShot
	runFixture(t, "worktime.html", &got)

	if got.MissedTheEnd != "2 h 0 m" && got.MissedTheEnd != "1 h 30 m" {
		t.Errorf("a finished run with no end recorded says %q — expected its age", got.MissedTheEnd)
	}
}
