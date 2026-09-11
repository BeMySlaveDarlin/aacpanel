package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/notify"
	"aacpanel/internal/testdb"
)

func TestPushStateSurvivesPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { pool.Exec(ctx, "DELETE FROM push_state WHERE key LIKE 'probe:%'") }
	cleanup()
	defer cleanup()

	journal := s.PushJournal()
	want := notify.Event{
		Key: "probe:container", Domain: notify.DomainContainer,
		Title: "The container fell over · aacpanel-db", Body: "Exited (1) 2 seconds ago · the aacpanel stack",
		Severity:  notify.Critical,
		GoneTitle: "The container came up · aacpanel-db", GoneBody: "Working again · the aacpanel stack",
		GoneSeverity: notify.Info,
	}
	if err := journal.Raise(ctx, want); err != nil {
		t.Fatalf("writing the reason: %v", err)
	}

	got := findEvent(t, journal, want.Key)
	if got != want {
		t.Fatalf("the reason was read back differently from how it was written:\n  got    %+v\n  wanted %+v", got, want)
	}

	want.Body = "Exited (137) 1 second ago · the aacpanel stack"
	if err := journal.Raise(ctx, want); err != nil {
		t.Fatalf("the repeated write: %v", err)
	}
	if got := findEvent(t, journal, want.Key); got.Body != want.Body {
		t.Fatalf("the repeat did not update the memory: %+v", got)
	}

	if err := journal.Drop(ctx, want.Key); err != nil {
		t.Fatalf("clearing the reason: %v", err)
	}
	list, err := journal.Raised(ctx)
	if err != nil {
		t.Fatalf("reading the reasons: %v", err)
	}
	for _, e := range list {
		if e.Key == want.Key {
			t.Fatal("a cleared reason stayed in the database — after a restart it will be announced as \"cleared\" a second time")
		}
	}
}

func findEvent(t *testing.T, journal *PushState, key string) notify.Event {
	t.Helper()
	list, err := journal.Raised(context.Background())
	if err != nil {
		t.Fatalf("reading the reasons: %v", err)
	}
	for _, e := range list {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("the reason %q is not in the database", key)
	return notify.Event{}
}
