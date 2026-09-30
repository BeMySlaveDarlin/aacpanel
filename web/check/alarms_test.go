package check

import (
	"strings"
	"testing"
)

type alarmRow struct {
	Text  string `json:"text"`
	Kind  string `json:"kind"`
	When  string `json:"when"`
	Opens bool   `json:"opens"`
	Stops bool   `json:"stops"`
}

type alarmsShot struct {
	Chip string     `json:"chip"`
	Rows []alarmRow `json:"rows"`
}

// A job the session put on its cron stands among its background work the way
// its wake-up does: a row that says what prompt comes back and when — the date
// of a job that fires once, how often a job that repeats fires — and that
// neither opens nor stops, since nothing runs until it fires. The chip counts
// it among the work still ahead. A watch the session keeps reads as monitoring,
// not as the command every unknown kind falls back to.
func TestAJobOfTheCronStandsAmongTheAlarms(t *testing.T) {
	var got alarmsShot
	runFixture(t, "alarms.html", &got)

	if len(got.Rows) != 5 {
		t.Fatalf("the list drew %d rows over a command, a watch, a wake-up and two jobs of the cron: %+v",
			len(got.Rows), got.Rows)
	}
	if got.Chip != "5" {
		t.Errorf("the chip counts %q of the five: an alarm set is work the session has ahead", got.Chip)
	}
	want := []struct {
		text, kind, when string
		alarm            bool
	}{
		{"Waiting for the tests", "command", "", false},
		{"Watching the build", "monitoring", "", false},
		{"watching CI", "wake-up", "in 20 min", true},
		{"Check that the shop cache was rebuilt", "alarm", "in 3 h", true},
		{"Hourly check of the shop deploy", "repeats", "Every hour at :13", true},
	}
	for i, w := range want {
		row := got.Rows[i]
		if !strings.HasPrefix(row.Text, w.text) || row.Kind != w.kind {
			t.Errorf("row %d reads %q as %q, expected %q as %q", i, row.Text, row.Kind, w.text, w.kind)
		}
		if !w.alarm {
			continue
		}
		if row.When != w.when {
			t.Errorf("the row of %q says %q of its time, expected %q", w.text, row.When, w.when)
		}
		if row.Opens || row.Stops {
			t.Errorf("the row of %q opens or offers a stop: an alarm runs nothing to read or stop", w.text)
		}
	}
}
