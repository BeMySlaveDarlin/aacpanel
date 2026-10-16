package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func openCalls(t *testing.T, ctx context.Context, conversations ...string) *SessionCalls {
	t.Helper()
	s, err := New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	forget := func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM session_calls WHERE conversation = ANY($1)`, conversations); err != nil {
			t.Fatalf("clearing the calls of the probe: %v", err)
		}
	}
	forget()
	t.Cleanup(forget)
	return s.SessionCalls()
}

func unseenCall(t *testing.T, ctx context.Context, calls *SessionCalls, conversation string) (time.Time, bool) {
	t.Helper()
	got, err := calls.Unseen(ctx)
	if err != nil {
		t.Fatalf("the calls not seen: %v", err)
	}
	at, ok := got[conversation]
	return at, ok
}

func called(t *testing.T, ctx context.Context, calls *SessionCalls, conversation, session string, at time.Time, seen bool) {
	t.Helper()
	if err := calls.Called(ctx, conversation, session, at, seen); err != nil {
		t.Fatalf("the call of %s: %v", conversation, err)
	}
}

// A call stands until the session is opened, whoever opens it, and a listing
// of the same call does not bring it back; a newer call does, as one mark
// however many calls came before the visit.
func TestSessionCallStandsUntilTheSessionIsOpenedPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	const conv = "calls-probe-stands"
	calls := openCalls(t, ctx, conv)

	first := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	called(t, ctx, calls, conv, "aacpanel", first, false)
	if at, ok := unseenCall(t, ctx, calls, conv); !ok || !at.Equal(first) {
		t.Fatalf("a call nobody saw is not marked: %v %v", at, ok)
	}
	called(t, ctx, calls, conv, "aacpanel", first, false)
	if _, ok := unseenCall(t, ctx, calls, conv); !ok {
		t.Fatal("a second listing of the call took its mark away")
	}

	if err := calls.Seen(ctx, "refactor", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := unseenCall(t, ctx, calls, conv); !ok {
		t.Fatal("opening another session took the mark of this one away")
	}
	if err := calls.Seen(ctx, "aacpanel", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := unseenCall(t, ctx, calls, conv); ok {
		t.Fatal("the session was opened and its call is still marked")
	}
	called(t, ctx, calls, conv, "aacpanel", first, false)
	if _, ok := unseenCall(t, ctx, calls, conv); ok {
		t.Fatal("the collector listed the call it had already listed, and the mark came back")
	}

	second := first.Add(2 * time.Minute)
	third := first.Add(4 * time.Minute)
	called(t, ctx, calls, conv, "aacpanel", second, false)
	called(t, ctx, calls, conv, "aacpanel", third, false)
	called(t, ctx, calls, conv, "aacpanel", second, false)
	if at, ok := unseenCall(t, ctx, calls, conv); !ok || !at.Equal(third) {
		t.Fatalf("two calls before the visit stand as %v %v, wanted one mark at the later %v", at, ok, third)
	}

	// A session renamed after the call is still opened under its
	// conversation.
	if err := calls.Seen(ctx, "aacpanel-renamed", conv); err != nil {
		t.Fatal(err)
	}
	if _, ok := unseenCall(t, ctx, calls, conv); ok {
		t.Fatal("the renamed session was opened and its call is still marked")
	}
}

// A call made while someone has the session on screen is seen as it comes,
// and the calls of a month ago are gone.
func TestSessionCallSeenAsItComesPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	const watched, old = "calls-probe-watched", "calls-probe-old"
	calls := openCalls(t, ctx, watched, old)

	now := time.Now().Truncate(time.Second)
	called(t, ctx, calls, watched, "person", now, true)
	if _, ok := unseenCall(t, ctx, calls, watched); ok {
		t.Fatal("a call made with the session on screen marks it")
	}
	called(t, ctx, calls, watched, "person", now, false)
	if _, ok := unseenCall(t, ctx, calls, watched); ok {
		t.Fatal("a later listing of a call seen as it came marked it")
	}

	called(t, ctx, calls, old, "blog", now.Add(-31*24*time.Hour), false)
	if _, ok := unseenCall(t, ctx, calls, old); ok {
		t.Fatal("a call of a month ago is still kept")
	}
}
