package notify

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

var when = time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC)

func ms(t time.Time) int64 { return t.UnixMilli() }

func sess() Session {
	return Session{
		ID: "4d89ed41", Name: "aacpanel", Profile: "personal",
		CWD: "/srv/proj/aacpanel", Status: "idle",
	}
}

func snapshotWorld() World {
	return World{Now: when}
}

func TestEveryEventLooksLikeThis(t *testing.T) {
	value := 91.2

	asking := sess()
	asking.Ask = &Ask{Header: "Approach", Text: "How do we bring the stack down?", Count: 3, At: "2026-09-08T03:00:00Z"}

	permitting := sess()
	permitting.Status, permitting.StatusAt = "waiting", ms(when)
	permitting.WaitingFor = "input needed"

	busy, idle := sess(), sess()
	busy.Status, busy.StatusAt = "busy", ms(when.Add(-42*time.Minute))
	idle.Status, idle.StatusAt = "idle", ms(when)

	fallen := Container{Name: "aacpanel-db", Stack: "aacpanel", State: "exited", Status: "Exited (1) 2 seconds ago"}
	alive := Container{Name: "aacpanel-db", Stack: "aacpanel", State: "running", Status: "Up 3 days"}
	sickly := Container{Name: "aacpanel", Stack: "aacpanel", State: "running", Status: "Up 2 hours (unhealthy)", Health: "unhealthy"}

	cases := []struct {
		name       string
		prev, cur  World
		key        string
		title      string
		body       string
		gone       string
		goneBody   string
		severity   Severity
		goneSilent bool
	}{
		{
			name: "a session asked a question",
			cur:  world(snapshotWorld(), func(w *World) { w.Sessions = []Session{asking} }),
			key:  "ask:4d89ed41:2026-09-08T03:00:00Z",

			title:      "Question · aacpanel",
			body:       "Approach: How do we bring the stack down? · and 2 more in this round · personal · aacpanel",
			severity:   Critical,
			goneSilent: true,
		},
		{
			name:  "a session is waiting for permission",
			prev:  world(snapshotWorld(), func(w *World) { w.Sessions = []Session{permitting} }),
			cur:   world(snapshotWorld(), func(w *World) { w.Sessions = []Session{permitting} }),
			key:   "wait:4d89ed41:" + itoa(ms(when)),
			title: "Waiting for permission · aacpanel",

			body:       "Input needed — the session is blocked until you answer · personal · aacpanel",
			severity:   Critical,
			goneSilent: true,
		},
		{
			name: "a session came free after a long turn",
			prev: world(snapshotWorld(), func(w *World) { w.Sessions = []Session{busy} }),
			cur:  world(snapshotWorld(), func(w *World) { w.Sessions = []Session{idle} }),
			key:  "done:4d89ed41:" + itoa(ms(when)),

			title:      "Turn finished · aacpanel",
			body:       "The turn took 42 min · personal · aacpanel",
			severity:   Info,
			goneSilent: true,
		},
		{
			name: "a session closed outside the panel",
			prev: world(snapshotWorld(), func(w *World) { w.Sessions = []Session{sess()} }),
			cur:  snapshotWorld(),
			key:  "gone:4d89ed41",

			title:      "Session closed · aacpanel",
			body:       "Closed outside the panel: it crashed or was closed at the machine · personal · aacpanel",
			severity:   Warning,
			goneSilent: true,
		},
		{
			name: "a session on the stream ended outside the panel",
			prev: world(snapshotWorld(), func(w *World) {
				held := sess()
				held.ID, held.Transport = "4d89ed41-06bf-41fb-8693-1bb025c09e7d", "stream"
				w.Sessions = []Session{held}
			}),
			cur: snapshotWorld(),
			key: "gone:4d89ed41-06bf-41fb-8693-1bb025c09e7d",

			title: "Session closed · aacpanel",
			body: "Ended outside the panel: its holder or its claude died. The conversation is whole — " +
				"resume it from the archive · conversation 4d89ed41 · personal · aacpanel",
			severity:   Warning,
			goneSilent: true,
		},
		{
			name: "a container fell",
			prev: world(snapshotWorld(), func(w *World) { w.Containers = []Container{alive} }),
			cur:  world(snapshotWorld(), func(w *World) { w.Containers = []Container{fallen} }),
			key:  "container:aacpanel-db",

			title:    "Container down · aacpanel-db",
			body:     "Exited (1) 2 seconds ago · stack aacpanel",
			severity: Critical,
			gone:     "Container up · aacpanel-db",
			goneBody: "Running again · stack aacpanel",
		},
		{
			name: "a container is unhealthy",
			cur:  world(snapshotWorld(), func(w *World) { w.Containers = []Container{sickly} }),
			key:  "health:aacpanel",

			title:    "Container unhealthy · aacpanel",
			body:     "healthcheck failing · stack aacpanel · Up 2 hours (unhealthy)",
			severity: Warning,
			gone:     "Container healthy · aacpanel",
			goneBody: "healthcheck passing again · stack aacpanel",
		},
		{
			name: "a stack went down",
			prev: world(snapshotWorld(), func(w *World) {
				w.Stacks = []Stack{{Name: "sample", Running: 5, Total: 5}}
			}),
			cur: world(snapshotWorld(), func(w *World) {
				w.Stacks = []Stack{{Name: "sample", Running: 0, Total: 5}}
			}),
			key: "stack:sample",

			title:    "Stack down · sample",
			body:     "None of its 5 containers is running, and the panel did not stop it.",
			severity: Critical,
			gone:     "Stack up · sample",
			goneBody: "0 of 5 running.",
		},
		{
			name: "a rule alert",
			cur: world(snapshotWorld(), func(w *World) {
				w.Alerts = []Alert{{
					ID: 12, Rule: "Low disk space", Subject: "/", Severity: "warning",
					Value: &value, Threshold: 90, Op: ">", Unit: "%", ForSec: 300,
				}}
			}),
			key: "alert:12",

			title:    "Low disk space · /",
			body:     "Now 91.2%, threshold > 90% · lasting 5 min",
			severity: Warning,
			gone:     "Alert cleared · /",
			goneBody: "Low disk space — back to normal.",
		},
		{
			name: "a probe does not answer",
			cur: world(snapshotWorld(), func(w *World) {
				w.Probes = []Probe{{
					ID: 3, Name: "Anthropic API", Target: "https://api.anthropic.com",
					OK: false, Outcome: "timeout", Error: "no answer in 5s", Streak: 3,
				}}
			}),
			key: "probe:3",

			title:    "Not answering · Anthropic API",
			body:     "Did not answer in time: no answer in 5s · 3 failures in a row · https://api.anthropic.com",
			severity: Warning,
			gone:     "Answers again · Anthropic API",
			goneBody: "The probe passes again.",
		},
		{
			name: "a probe degraded",
			cur: world(snapshotWorld(), func(w *World) {
				w.Probes = []Probe{{
					ID: 4, Name: "Anthropic status", OK: false, Outcome: "degraded",
					Error: "Claude API: degraded performance", Streak: 1,
				}}
			}),
			key: "probe:4",

			title:    "Works worse · Anthropic status",
			body:     "Works worse than usual: Claude API: degraded performance",
			severity: Warning,
			gone:     "Answers again · Anthropic status",
			goneBody: "The probe passes again.",
		},
		{
			name: "the agent is silent",
			cur:  world(snapshotWorld(), func(w *World) { w.AgentAge = 4 * time.Minute }),
			key:  "blind:agent",

			title:    "Panel is blind · agent",
			body:     "The host snapshot has not updated for 4 min — metrics, sessions and questions are frozen.",
			severity: Warning,
			gone:     "Panel sees again · agent",
			goneBody: "The host snapshot is updating.",
		},
		{
			name: "the database is unavailable",
			cur:  world(snapshotWorld(), func(w *World) { w.DBErr = "connection refused" }),
			key:  "blind:db",

			title:    "Panel is blind · database",
			body:     "History, alerts and the action log are unavailable: connection refused",
			severity: Warning,
			gone:     "Panel sees again · database",
			goneBody: "History and the action log are back.",
		},
		{
			name: "the five-hour limit",
			cur: world(snapshotWorld(), func(w *World) {
				w.LimitsSeen = true
				w.Limits = []Limit{{Contour: "personal", Window: "5h", Pct: 82,
					ResetsAt: when.Add(70 * time.Minute).Unix()}}
			}),
			key: "limit:5h:personal:" + itoa(when.Add(70*time.Minute).Unix()),

			title:    "5-hour limit · personal",
			body:     "82% used · the window resets in 1 h 10 m.",
			severity: Warning,
			gone:     "Limit reset · personal",
			goneBody: "The 5-hour window has started over.",
		},
		{
			name: "the weekly limit",
			cur: world(snapshotWorld(), func(w *World) {
				w.LimitsSeen = true
				w.Limits = []Limit{{Contour: "acme", Window: "7d", Pct: 91,
					ResetsAt: when.Add(50 * time.Hour).Unix()}}
			}),
			key: "limit:7d:acme:" + itoa(when.Add(50*time.Hour).Unix()),

			title:    "Weekly limit · acme",
			body:     "91% used · the window resets in 2 d 2 h.",
			severity: Warning,
			gone:     "Limit reset · acme",
			goneBody: "The weekly window has started over.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := only(t, Look(c.prev, c.cur), c.key)
			if got.Title != c.title {
				t.Errorf("title:\n  got       %q\n  expected  %q", got.Title, c.title)
			}
			if got.Body != c.body {
				t.Errorf("body:\n  got       %q\n  expected  %q", got.Body, c.body)
			}
			if got.Severity != c.severity {
				t.Errorf("severity %q, expected %q", got.Severity, c.severity)
			}
			if c.goneSilent {
				if got.GoneTitle != "" {
					t.Errorf("there is nothing to say about clearing this event, yet it says: %q", got.GoneTitle)
				}
				return
			}
			if got.GoneTitle != c.gone || got.GoneBody != c.goneBody {
				t.Errorf("clear:\n  got       %q / %q\n  expected  %q / %q",
					got.GoneTitle, got.GoneBody, c.gone, c.goneBody)
			}
		})
	}
}

func TestPanelStaysSilentAboutItsOwnDoing(t *testing.T) {
	prev := World{Now: when,
		Sessions: []Session{sess()},
		Containers: []Container{
			{Name: "aacpanel-db", Stack: "aacpanel", State: "running"},
			{Name: "aacpanel", Stack: "aacpanel", State: "running"},
		},
		Stacks: []Stack{{Name: "aacpanel", Running: 2, Total: 2}},
	}
	cur := World{Now: when,
		Containers: []Container{
			{Name: "aacpanel-db", Stack: "aacpanel", State: "exited"},
			{Name: "aacpanel", Stack: "aacpanel", State: "running", Health: "unhealthy"},
		},
		Stacks: []Stack{{Name: "aacpanel", Running: 0, Total: 2}},
		PanelDid: map[string]bool{
			"session:aacpanel": true, "container:aacpanel-db": true,
			"container:aacpanel": true, "stack:aacpanel": true,
		},
	}

	if r := Look(prev, cur); len(r.Raise) > 0 {
		t.Fatalf("the panel notified about what it did itself: %+v", r.Raise)
	}
}

func TestLongDeadContainerIsNotNews(t *testing.T) {
	down := Container{Name: "old", State: "exited"}
	r := Look(
		World{Now: when, Containers: []Container{down}},
		World{Now: when, Containers: []Container{down}},
	)
	if len(r.Raise) > 0 {
		t.Errorf("a container that never fell was announced: %+v", r.Raise)
	}
	if !has(r.Hold, "container:old") {
		t.Errorf("the event is not held: %v", r.Hold)
	}
}

func TestRemovedContainerIsForgottenSilently(t *testing.T) {
	r := Look(
		World{Now: when, Containers: []Container{{Name: "removed", State: "exited"}}},
		World{Now: when},
	)
	if !has(r.Drop, "container:removed") {
		t.Errorf("a removed container is not forgotten silently: %+v", r)
	}
}

func TestAnsweredQuestionDoesNotBecomeAPermitRequest(t *testing.T) {
	asked := sess()
	asked.Status, asked.StatusAt = "waiting", ms(when)
	asked.WaitingFor = "dialog open"
	asked.Ask = &Ask{Header: "Approach", Text: "How do we bring the stack down?", Count: 1, At: "2026-09-08T03:00:00Z"}

	answered := asked
	answered.Ask = nil

	r := Look(
		World{Now: when, Sessions: []Session{asked}},
		World{Now: when, Sessions: []Session{answered}},
	)
	if len(r.Raise) > 0 {
		t.Fatalf("after the answer a new event was raised: %+v", r.Raise)
	}
}

func TestShortTurnIsNotWorthAPush(t *testing.T) {
	busy, idle := sess(), sess()
	busy.Status, busy.StatusAt = "busy", ms(when.Add(-3*time.Minute))
	idle.Status, idle.StatusAt = "idle", ms(when)

	r := Look(
		World{Now: when, Sessions: []Session{busy}},
		World{Now: when, Sessions: []Session{idle}},
	)
	if len(r.Raise) > 0 {
		t.Fatalf("a three-minute turn reached the phone: %+v", r.Raise)
	}
}

func TestBlindSourceStopsJudging(t *testing.T) {
	alive := World{Now: when, Sessions: []Session{sess()}}

	t.Run("the database is silent", func(t *testing.T) {
		r := Look(alive, World{Now: when, DBErr: "connection refused"})
		if has(r.Seen, DomainStore) {
			t.Error("the panel started judging alerts while the database is silent")
		}
	})

	t.Run("the agent is silent", func(t *testing.T) {
		r := Look(alive, World{Now: when, AgentAge: 5 * time.Minute})
		if has(r.Seen, DomainSession) {
			t.Error("the panel started judging sessions from a five-minute-old snapshot")
		}
		for _, e := range r.Raise {
			if strings.HasPrefix(e.Key, "gone:") {
				t.Errorf("the sessions “closed” together with the snapshot: %s", e.Key)
			}
		}
	})

	t.Run("there is no database at all", func(t *testing.T) {
		r := Look(alive, World{Now: when, DBOff: true})
		for _, e := range r.Raise {
			if e.Key == "blind:db" {
				t.Error("an unconfigured database was announced as an incident")
			}
		}
	})
}

func TestLimitWindowRollClearsTheWarning(t *testing.T) {
	first := when.Add(time.Hour).Unix()
	second := when.Add(6 * time.Hour).Unix()

	hot := World{Now: when, LimitsSeen: true,
		Limits: []Limit{{Contour: "personal", Window: "5h", Pct: 82, ResetsAt: first}}}
	fresh := World{Now: when, LimitsSeen: true,
		Limits: []Limit{{Contour: "personal", Window: "5h", Pct: 4, ResetsAt: second}}}

	before := Look(World{Now: when}, hot)
	if _, ok := find(before, "limit:5h:personal:"+itoa(first)); !ok {
		t.Fatal("the limit event did not rise")
	}

	after := Look(hot, fresh)
	if has(after.Hold, "limit:5h:personal:"+itoa(first)) {
		t.Error("the event of the previous window is held — nobody will say it reset")
	}
	if !has(after.Seen, "limit:5h:personal") {
		t.Error("the watch class is lost — the clear will not arrive")
	}
	if len(after.Raise) > 0 {
		t.Errorf("a new window at four percent raised an event: %+v", after.Raise)
	}
}

func TestLostContourIsNotAReset(t *testing.T) {
	r := Look(World{Now: when}, World{Now: when, LimitsSeen: true})
	if has(r.Seen, "limit:5h:personal") {
		t.Error("the panel judges a contour that is not in the snapshot")
	}
}

func world(base World, tune func(*World)) World {
	tune(&base)
	return base
}

func only(t *testing.T, r Report, key string) Event {
	t.Helper()
	e, ok := find(r, key)
	if !ok {
		t.Fatalf("event %q is not among those raised: %+v", key, keys(r.Raise))
	}
	if len(r.Raise) != 1 {
		t.Fatalf("%d events raised, one expected: %v", len(r.Raise), keys(r.Raise))
	}
	return e
}

func find(r Report, key string) (Event, bool) {
	for _, e := range r.Raise {
		if e.Key == key {
			return e, true
		}
	}
	return Event{}, false
}

func keys(list []Event) []string {
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, e.Key)
	}
	return out
}

func has(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestUnitFromAnOlderAlertStillPrints(t *testing.T) {
	value := 3.5 * 1024 * 1024 * 1024
	for _, unit := range []string{unitBytes, unitBytesWas} {
		t.Run(unit, func(t *testing.T) {
			ev := tripped(Alert{
				ID: 77, Rule: "Low memory", Subject: "host", Severity: "warning",
				Value: &value, Threshold: 4 * 1024 * 1024 * 1024, Op: "<", Unit: unit,
			})
			if !strings.Contains(ev.Body, "3.5 GB") {
				t.Errorf("the size is printed as a raw number instead of a size: %q", ev.Body)
			}
		})
	}
	for _, unit := range []string{unitFlag, unitFlagWas} {
		t.Run(unit, func(t *testing.T) {
			ev := tripped(Alert{ID: 78, Rule: "Service down", Subject: "host", Severity: "critical", Unit: unit})
			if !strings.EqualFold(ev.Body, "the flag is raised") {
				t.Errorf("the flag stopped being recognised, the push body: %q", ev.Body)
			}
		})
	}
}

func shelf(items ...Brief) World {
	w := snapshotWorld()
	w.BriefsSeen = true
	w.Briefs = items
	return w
}

var doc = Brief{
	ID: "seven-after-twelve", Title: "Seven questions after twelve",
	Session: "aacpanel", At: "2026-09-08T02:59:00Z", Questions: 7,
}

// A brief is published while the person is away from the screen: it is the one
// moment worth their phone, because from then on the document simply waits.
func TestANewBriefIsPushedOnce(t *testing.T) {
	prev := shelf()
	cur := shelf(doc)

	report := Look(prev, cur)
	if len(report.Raise) != 1 {
		t.Fatalf("events raised: %+v", report.Raise)
	}
	got := report.Raise[0]
	if !strings.Contains(got.Title, "Seven questions after twelve") {
		t.Errorf("the push does not name the document: %q", got.Title)
	}
	if !strings.Contains(got.Body, "7 questions") || !strings.Contains(got.Body, "aacpanel") {
		t.Errorf("the push does not say what it asks or who asked: %q", got.Body)
	}
	// Held reasons raise again every round; a brief waits for hours by design.
	for _, key := range report.Hold {
		if strings.HasPrefix(key, "brief:") {
			t.Errorf("the brief is held open and will push again every round: %q", key)
		}
	}

	// The same shelf a round later is not news.
	again := Look(cur, shelf(doc))
	if len(again.Raise) != 0 {
		t.Errorf("a brief that was already announced pushed again: %+v", again.Raise)
	}
}

// A shelf nobody has read yet says nothing about what is new. Without this the
// panel would announce every standing document each time the service restarts.
func TestTheFirstSightOfTheShelfIsSilent(t *testing.T) {
	report := Look(snapshotWorld(), shelf(doc))
	if len(report.Raise) != 0 {
		t.Errorf("the first look at the shelf pushed: %+v", report.Raise)
	}
}

// A document republished under the same name is a different document: the
// answers stay, the text does not.
func TestARepublishedBriefIsNewsAgain(t *testing.T) {
	second := doc
	second.At = "2026-09-08T03:00:00Z"

	report := Look(shelf(doc), shelf(second))
	if len(report.Raise) != 1 {
		t.Fatalf("events raised: %+v", report.Raise)
	}
	if !strings.Contains(report.Raise[0].Title, "again") {
		t.Errorf("a republished document reads like a new one: %q", report.Raise[0].Title)
	}
}

// A brief with no questions is a piece to read, and saying it asks nothing is
// the difference between "when you have a minute" and "this is waiting on you".
func TestABriefThatAsksNothingSaysSo(t *testing.T) {
	reading := doc
	reading.ID, reading.Questions = "what-the-scan-found", 0

	report := Look(shelf(), shelf(reading))
	if len(report.Raise) != 1 {
		t.Fatalf("events raised: %+v", report.Raise)
	}
	if !strings.Contains(report.Raise[0].Body, "nothing to answer") {
		t.Errorf("a piece to read promises questions: %q", report.Raise[0].Body)
	}
}
