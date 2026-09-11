package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestSessionViewGoesStalePG(t *testing.T) {
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

	var device int64
	err = pool.QueryRow(ctx, `
		INSERT INTO devices (name, credential_id, public_key)
		VALUES ('view probe', $1, $2) RETURNING id`,
		[]byte("cred-view"), []byte("key-view")).Scan(&device)
	if err != nil {
		t.Fatalf("the device for the probe: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, device)

	views := s.SessionViews()
	if err := views.Mark(ctx, device, "aacpanel"); err != nil {
		t.Fatalf("the mark: %v", err)
	}

	if got := watchers(t, ctx, views, "aacpanel"); len(got) != 1 || got[0] != device {
		t.Fatalf("the fresh mark is not visible: %v", got)
	}
	if got := watchers(t, ctx, views, "refactor"); len(got) != 0 {
		t.Fatalf("a mark about one session answered for another: %v", got)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE session_views SET seen_at = now() - interval '5 minutes' WHERE device_id = $1`, device); err != nil {
		t.Fatalf("ageing the mark: %v", err)
	}
	if got := watchers(t, ctx, views, "aacpanel"); len(got) != 0 {
		t.Fatalf("a stale mark still silences the notifications: %v", got)
	}

	if err := views.Mark(ctx, device, "aacpanel"); err != nil {
		t.Fatalf("the repeated mark: %v", err)
	}
	if got := watchers(t, ctx, views, "aacpanel"); len(got) != 1 {
		t.Fatalf("%d marks after the repeat, wanted one: %v", len(got), got)
	}

	if err := views.Forget(ctx, device); err != nil {
		t.Fatalf("removing the mark: %v", err)
	}
	if got := watchers(t, ctx, views, "aacpanel"); len(got) != 0 {
		t.Fatalf("the removed mark is still there: %v", got)
	}
}

func TestSessionViewLeavesWithTheDevicePG(t *testing.T) {
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

	var device int64
	err = pool.QueryRow(ctx, `
		INSERT INTO devices (name, credential_id, public_key)
		VALUES ('revocation probe', $1, $2) RETURNING id`,
		[]byte("cred-gone"), []byte("key-gone")).Scan(&device)
	if err != nil {
		t.Fatalf("the device for the probe: %v", err)
	}

	views := s.SessionViews()
	if err := views.Mark(ctx, device, "aacpanel"); err != nil {
		t.Fatalf("the mark: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, device); err != nil {
		t.Fatalf("deleting the device: %v", err)
	}
	if got := watchers(t, ctx, views, "aacpanel"); len(got) != 0 {
		t.Fatalf("the mark outlived the device: %v", got)
	}
}

func watchers(t *testing.T, ctx context.Context, views *SessionViews, session string) []int64 {
	t.Helper()
	got, err := views.Watching(ctx, session, time.Minute)
	if err != nil {
		t.Fatalf("who is watching %q: %v", session, err)
	}
	return got
}
