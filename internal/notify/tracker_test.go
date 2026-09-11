package notify

import (
	"context"
	"errors"
	"testing"
)

type memJournal struct {
	rows   map[string]Event
	broken bool
	reads  int
}

func newJournal() *memJournal { return &memJournal{rows: map[string]Event{}} }

func (m *memJournal) Raised(context.Context) ([]Event, error) {
	m.reads++
	if m.broken {
		return nil, errors.New("the database is unavailable")
	}
	out := make([]Event, 0, len(m.rows))
	for _, e := range m.rows {
		out = append(out, e)
	}
	return out, nil
}

func (m *memJournal) Raise(_ context.Context, e Event) error {
	if m.broken {
		return errors.New("the database is unavailable")
	}
	m.rows[e.Key] = e
	return nil
}

func (m *memJournal) Drop(_ context.Context, key string) error {
	if m.broken {
		return errors.New("the database is unavailable")
	}
	delete(m.rows, key)
	return nil
}

func alertEventFixture() Event {
	return Event{
		Key: "alert:12", Domain: DomainStore,
		Title: "Low disk space · /", Body: "Now 91.2%, threshold > 90%",
		Severity:  Warning,
		GoneTitle: "Alert cleared · /", GoneBody: "Low disk space — back to normal.",
		GoneSeverity: Info,
	}
}

func holding(e Event) Report {
	return Report{Raise: []Event{e}, Hold: []string{e.Key}, Seen: []string{e.Domain}}
}

func quiet(domain string) Report { return Report{Seen: []string{domain}} }

func started(j Journal) *Tracker {
	tr := NewTracker(j)
	tr.Step(context.Background(), Report{})
	return tr
}

func TestFirstRunAcceptsThePastInSilence(t *testing.T) {
	ctx := context.Background()
	journal := newJournal()
	old := alertEventFixture()

	tr := NewTracker(journal)
	if said := tr.Step(ctx, holding(old)); len(said) > 0 {
		t.Fatalf("the first run announced the past: %+v", said)
	}
	if gone := tr.Step(ctx, quiet(DomainStore)); len(gone) > 0 {
		t.Fatalf("clearing an event accepted in silence was announced out loud: %+v", gone)
	}

	fresh := alertEventFixture()
	fresh.Key = "alert:13"
	if said := tr.Step(ctx, holding(fresh)); len(said) != 1 {
		t.Fatalf("a fresh event got lost together with the past: %+v", said)
	}

	after := NewTracker(journal)
	said := after.Step(ctx, holding(Event{Key: "alert:14", Domain: DomainStore,
		Title: "Disk", Body: "91%", Severity: Warning}))
	if !saidAbout(said, "alert:14") {
		t.Fatalf("after a restart the event was accepted in silence again: %+v", said)
	}
}

func TestOnePushOnRaiseAndOneOnClear(t *testing.T) {
	ctx := context.Background()
	e := alertEventFixture()
	tr := started(newJournal())

	raised := tr.Step(ctx, holding(e))
	if len(raised) != 1 || raised[0].Title != e.Title {
		t.Fatalf("the raise was not announced: %+v", raised)
	}

	for range 5 {
		if again := tr.Step(ctx, holding(e)); len(again) > 0 {
			t.Fatalf("a repeated notification about the same event: %+v", again)
		}
	}

	gone := tr.Step(ctx, quiet(DomainStore))
	if len(gone) != 1 || gone[0].Title != e.GoneTitle || gone[0].Body != e.GoneBody {
		t.Fatalf("the clear was not announced, or announced with the wrong wording: %+v", gone)
	}
	if gone[0].Tag != raised[0].Tag {
		t.Errorf("the tags diverged: %q and %q", raised[0].Tag, gone[0].Tag)
	}
	if tr.Active() != 0 {
		t.Errorf("a cleared event stayed on the count: %d", tr.Active())
	}
}

func TestOneShotEventIsSaidOnce(t *testing.T) {
	ctx := context.Background()
	e := Event{Key: "gone:4d89ed41", Domain: DomainSession,
		Title: "Session closed · aacpanel", Body: "Closed outside the panel", Severity: Warning}
	tr := started(newJournal())

	if said := tr.Step(ctx, Report{Raise: []Event{e}, Seen: []string{DomainSession}}); len(said) != 1 {
		t.Fatalf("a one-shot event was not announced: %+v", said)
	}
	if again := tr.Step(ctx, quiet(DomainSession)); len(again) != 0 {
		t.Fatalf("clearing a one-shot event was announced out loud: %+v", again)
	}
	if tr.Active() != 0 {
		t.Errorf("a one-shot event stayed on the count: %d", tr.Active())
	}
}

func TestDroppedEventLeavesQuietly(t *testing.T) {
	ctx := context.Background()
	e := Event{Key: "container:removed", Domain: DomainContainer,
		Title: "Container down · removed", Body: "Exited (1)", Severity: Critical,
		GoneTitle: "Container up · removed", GoneBody: "Running again"}
	tr := started(newJournal())
	tr.Step(ctx, holding(e))

	gone := tr.Step(ctx, Report{Drop: []string{e.Key}, Seen: []string{DomainContainer}})
	if len(gone) != 0 {
		t.Fatalf("a removed container was announced as back up: %+v", gone)
	}
}

func TestRestartNeitherRepeatsNorForgets(t *testing.T) {
	ctx := context.Background()
	journal := newJournal()
	told := alertEventFixture()

	before := started(journal)
	before.Step(ctx, holding(told))

	after := NewTracker(journal)
	if again := after.Step(ctx, holding(told)); len(again) > 0 {
		t.Fatalf("after a restart the event was announced anew: %+v", again)
	}

	fresh := Event{Key: "alert:13", Domain: DomainStore,
		Title: "Host memory · host", Body: "Now 95%", Severity: Critical,
		GoneTitle: "Alert cleared · host", GoneBody: "Host memory — back to normal."}
	said := after.Step(ctx, Report{
		Raise: []Event{told, fresh},
		Hold:  []string{told.Key, fresh.Key},
		Seen:  []string{DomainStore},
	})
	if len(said) != 1 || said[0].Title != fresh.Title {
		t.Fatalf("an event from the downtime is lost: %+v", said)
	}

	gone := after.Step(ctx, quiet(DomainStore))
	if len(gone) != 2 {
		t.Fatalf("not both events were cleared: %+v", gone)
	}
}

func TestUnseenDomainKeepsTheEvent(t *testing.T) {
	ctx := context.Background()
	e := alertEventFixture()
	tr := started(newJournal())
	tr.Step(ctx, holding(e))

	if gone := tr.Step(ctx, Report{Seen: []string{DomainVision}}); len(gone) > 0 {
		t.Fatalf("the alert was cleared while the database is silent: %+v", gone)
	}
	if tr.Active() != 1 {
		t.Fatalf("the event is lost together with the database: %d", tr.Active())
	}
	if gone := tr.Step(ctx, quiet(DomainStore)); len(gone) != 1 {
		t.Fatalf("the clear was not announced after the database came back: %+v", gone)
	}
}

func TestMemoryWorksWhileTheJournalIsSilent(t *testing.T) {
	ctx := context.Background()
	journal := newJournal()
	journal.rows["alert:99"] = Event{Key: "alert:99", Domain: DomainStore,
		Title: "Old alert", Body: "from last time", GoneTitle: "Cleared"}
	journal.broken = true

	tr := NewTracker(journal)
	e := alertEventFixture()
	if said := tr.Step(ctx, holding(e)); len(said) != 1 {
		t.Fatalf("with a silent database the panel kept quiet too: %+v", said)
	}

	journal.broken = false
	said := tr.Step(ctx, holding(e))
	if len(said) != 0 {
		t.Fatalf("the past that arrived after the report said something: %+v", said)
	}
	if tr.Active() != 1 {
		t.Fatalf("the past got mixed in: %d events, one expected", tr.Active())
	}
}

func TestNoJournalIsFine(t *testing.T) {
	ctx := context.Background()
	tr := started(nil)
	e := alertEventFixture()

	if said := tr.Step(ctx, holding(e)); len(said) != 1 {
		t.Fatalf("without a database the event was not announced: %+v", said)
	}
	if gone := tr.Step(ctx, quiet(DomainStore)); len(gone) != 1 {
		t.Fatalf("without a database the clear was not announced: %+v", gone)
	}
}

func saidAbout(list []Message, tag string) bool {
	for _, m := range list {
		if m.Tag == tag {
			return true
		}
	}
	return false
}
