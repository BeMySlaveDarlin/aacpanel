package action

import (
	"strings"
	"testing"
	"time"
)

func TestPauseScopeAndTimeStayInTheService(t *testing.T) {
	if Valid(ProjectPause) || (Request{ID: "p", Kind: ProjectPause, Target: "lab"}).Validate() == nil {
		t.Fatal("the host accepted the service's pause action")
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		pause Pause
		ok    bool
	}{
		{Pause{Project: 1, Until: now.Add(time.Hour).Format(time.RFC3339)}, true},
		{Pause{Project: 1, Resume: true}, true},
		{Pause{Resume: true}, false},
		{Pause{Project: 1}, false},
		{Pause{Project: 1, Until: now.Format(time.RFC3339)}, false},
		{Pause{Project: 1, Until: now.Add(8 * 24 * time.Hour).Format(time.RFC3339)}, false},
		{Pause{Project: 1, Resume: true, Until: now.Add(time.Hour).Format(time.RFC3339)}, false},
		{Pause{Project: 1, Session: "lead", Until: now.Add(time.Hour).Format(time.RFC3339)}, true},
		{Pause{Project: 1, Session: "lead", Resume: true}, true},
		{Pause{Project: 1, Session: "lead", Group: true, Resume: true}, false},
		{Pause{Project: 1, NoEnd: true}, true},
		{Pause{Project: 1, Session: "lead", NoEnd: true}, true},
		{Pause{Project: 1, NoEnd: true, Until: now.Add(time.Hour).Format(time.RFC3339)}, false},
		{Pause{Project: 1, NoEnd: true, Resume: true}, false},
	} {
		at, err := c.pause.Validate(now)
		if (err == nil) != c.ok {
			t.Errorf("%+v: %v, want valid %v", c.pause, err, c.ok)
		}
		if err == nil && c.pause.NoEnd != at.IsZero() && !c.pause.Resume {
			t.Errorf("%+v ends at %v", c.pause, at)
		}
	}
}

// A pause until a moment carries the moment in its envelope and sets the
// alarm that rings at it; a pause with no end carries none and sets the
// repeat that marks it. Each clears the alarm of the other kind first.
func TestPauseMessagesSetTheAlarmOfTheirKind(t *testing.T) {
	until := time.Date(2026, 10, 26, 9, 0, 0, 0, time.FixedZone("local", 7*3600))
	timed, open := PauseMessage(until), PauseMessage(time.Time{})
	for _, want := range []string{`<panel-message from="pause" until="2026-10-26T09:00:00+07:00">`, "Pause until 09:00, 26 Oct 2026",
		`name: "pause", at: "2026-10-26T09:00:00+07:00"`, "alarm named pause or pause-no-end"} {
		if !strings.Contains(timed, want) {
			t.Errorf("the pause until a moment lacks %q:\n%s", want, timed)
		}
	}
	for _, want := range []string{"<panel-message from=\"pause with no end\">\n", "Pause with no end.", `name: "pause-no-end", every: "168h"`,
		"alarm named pause or pause-no-end", "until the person lifts it"} {
		if !strings.Contains(open, want) {
			t.Errorf("the pause with no end lacks %q:\n%s", want, open)
		}
	}
	if strings.Contains(open, "until=") || strings.Contains(open, "0001") {
		t.Errorf("the pause with no end names a moment:\n%s", open)
	}
}

func TestSleepMessagesLeaveTheDeadlineWithThePanel(t *testing.T) {
	until := time.Date(2026, 10, 26, 9, 0, 0, 0, time.FixedZone("local", 7*3600))
	for _, at := range []time.Time{until, {}} {
		text := SleepMessage(at)
		if strings.Contains(text, "alarm_set") || !strings.Contains(text, "end the turn") || !strings.Contains(text, "wake it with a message") {
			t.Errorf("sleep instruction: %s", text)
		}
		if at.IsZero() {
			if !strings.Contains(text, `from="pause with no end"`) || strings.Contains(text, "until=") {
				t.Errorf("indefinite sleep: %s", text)
			}
		} else if !strings.Contains(text, `until="2026-10-26T09:00:00+07:00"`) || !strings.Contains(text, "09:00, 26 Oct 2026") {
			t.Errorf("timed sleep: %s", text)
		}
	}
}
