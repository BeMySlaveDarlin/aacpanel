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

type heldRow struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Marks     []string `json:"marks"`
	Command   string   `json:"command"`
	Note      string   `json:"note"`
	State     string   `json:"state"`
	When      string   `json:"when"`
	Opens     bool     `json:"opens"`
	Stop      string   `json:"stop"`
	StopLabel string   `json:"stopLabel"`
}

type alarmsShot struct {
	Chip       string     `json:"chip"`
	ChipLabel  string     `json:"chipLabel"`
	Sub        string     `json:"sub"`
	Rows       []alarmRow `json:"rows"`
	Held       []heldRow  `json:"held"`
	StopAsks   string     `json:"stopAsks"`
	CancelAsks string     `json:"cancelAsks"`
	Sent       []struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	} `json:"sent"`
	After []string `json:"after"`
	Log   struct {
		Title string `json:"title"`
		Sub   string `json:"sub"`
		Text  string `json:"text"`
		Cut   string `json:"cut"`
		Asked string `json:"asked"`
	} `json:"log"`
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
	if got.Chip != "9" {
		t.Errorf("the chip counts %q of the five of the session's own, the two observers that run and the two "+
			"alarms of the panel: an alarm set is work the session has ahead", got.Chip)
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

// What a session asked the panel to wake it with stands where its background
// work does, on the phone and at a desk: an observer says the command it
// watches, what of it wakes the session and when it last did — or that it has
// not yet — since when it watches, and opens the end of its log; it is
// stopped while its command runs, and one whose command ended says how. An
// alarm of the panel says the note the session left itself, when it rings
// next and every how long, and how often it has rung; it opens nothing and is
// cancelled. Both go through the gate by the id the panel read them under,
// and the log is asked of the conversation by the observer's id.
func TestObserversAndAlarmsOfThePanelStandAmongTheBackgroundWork(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got alarmsShot
			screen.run(t, "alarms.html", &got)
			checkHeld(t, got)
		})
	}
}

func checkHeld(t *testing.T, got alarmsShot) {
	t.Helper()
	if got.Sub != "10 tasks, 9 running · command, monitoring, wake-up, alarm, observer, panel alarm" {
		t.Errorf("the head of the list reads %q", got.Sub)
	}
	if !strings.HasPrefix(got.ChipLabel, "background work: 10, 9 running") {
		t.Errorf("the chip is read out as %q", got.ChipLabel)
	}
	if len(got.Held) != 5 {
		t.Fatalf("the list drew %d rows of the panel's over three observers and two alarms: %+v", len(got.Held), got.Held)
	}
	byName := map[string]heldRow{}
	for _, r := range got.Held {
		byName[r.Name] = r
	}
	ci, waits, ended := byName["ci"], byName["wake-2030msk"], byName["deploy-log"]
	if ci.Kind != "observer" || strings.Join(ci.Marks, ",") != "observer" || ci.Command != "gh run watch 4242 --exit-status" ||
		!ci.Opens || ci.Stop != "on" {
		t.Errorf("the observer that woke the session reads %+v", ci)
	}
	if ci.State != "wakes it on a line matching ^(FAIL|ok) · woke it 2 times, last 3 min ago" || ci.When != "40 min" {
		t.Errorf("the observer that woke the session says %q, since %q", ci.State, ci.When)
	}
	if waits.State != "wakes it when it ends · has not woken it yet" || waits.Stop != "on" ||
		waits.StopLabel != "stop observer wake-2030msk" {
		t.Errorf("the observer that has not woken the session reads %+v", waits)
	}
	if !strings.HasPrefix(ended.State, "the command ended: exit code 0, 58 min ago · woke it once 1 h") ||
		ended.Stop != "off" || ended.When != "32 min" {
		t.Errorf("the observer whose command ended reads %+v: how it ended, how long it ran, and no stop", ended)
	}
	standup, once := byName["standup"], byName["check-cache"]
	if standup.Kind != "alarm" || strings.Join(standup.Marks, ",") != "panel alarm" || standup.Note != "Post the summary of the night to the channel" ||
		standup.When != "in 35 min" || standup.State != "every 1 h · rang 2 times, last 25 min ago" ||
		standup.Opens || standup.Stop != "on" || standup.StopLabel != "cancel alarm standup" {
		t.Errorf("the alarm that repeats reads %+v", standup)
	}
	if once.When != "in 15 min" || once.State != "rings once · has not rung yet" || once.Note != "" {
		t.Errorf("the alarm that rings once reads %+v", once)
	}

	if got.StopAsks != "Stop the observer ci in lab?" || got.CancelAsks != "Cancel the alarm standup in lab?" {
		t.Errorf("the gate asked %q and %q", got.StopAsks, got.CancelAsks)
	}
	if len(got.Sent) != 2 || got.Sent[0].Kind != "observer.stop" || got.Sent[0].Target != "lab" ||
		got.Sent[0].Params["id"] != "0123456789abcdef" || got.Sent[1].Kind != "alarm.cancel" ||
		got.Sent[1].Params["id"] != "a1b2c3d4e5f60718" {
		t.Errorf("the presses sent %+v: a stop and a cancel by the id the panel read them under", got.Sent)
	}
	after := strings.Join(got.After, " | ")
	if !strings.Contains(after, "ci observer stopped") || !strings.Contains(after, "standup panel alarm cancelled") {
		t.Errorf("after the presses the rows read %q", after)
	}

	if got.Log.Title != "Observer" || got.Log.Sub != "wake-2030msk" || got.Log.Text != "run 4242: 3 jobs\n✓ build\n✗ test\n" ||
		got.Log.Cut == "" {
		t.Errorf("the log of the observer opened as %+v", got.Log)
	}
	if !strings.HasPrefix(got.Log.Asked, "/api/chat/observer?session=lab&id=1&observer=00112233445566aa") {
		t.Errorf("the log was asked as %q", got.Log.Asked)
	}
}
