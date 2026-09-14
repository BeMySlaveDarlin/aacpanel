package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

// TestAlertsOrderByLastEventPG checks that the list runs by the latest event
// of each alert, whatever its state: an alert closed an hour ago stands above
// one that has been open for a day, and a page picks up below the cursor
// without losing the open ones.
func TestAlertsOrderByLastEventPG(t *testing.T) {
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

	cleanup := func() { pool.Exec(ctx, "DELETE FROM alerts") }
	cleanup()
	defer cleanup()

	var ruleID int
	if err := pool.QueryRow(ctx,
		"SELECT id FROM rules WHERE subject = 'stack.running_pct' LIMIT 1").Scan(&ruleID); err != nil {
		t.Fatal(err)
	}

	insert := func(subject, opened, closed, acked string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO alerts (rule_id, subject, severity, payload,
				opened_at, closed_at, acknowledged_at)
			VALUES ($1, $2, 'warning', '{}',
				now() - $3::interval,
				CASE WHEN $4 = '' THEN NULL ELSE now() - $4::interval END,
				CASE WHEN $5 = '' THEN NULL ELSE now() - $5::interval END)
			RETURNING id`, ruleID, subject, opened, closed, acked).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// Inserted from the oldest event to the newest so that the id order
	// contradicts the expected one and cannot pass for it.
	stale := insert("stale", "3 days", "2 days", "")
	open := insert("open", "1 day", "", "")
	seen := insert("seen", "2 days", "", "3 hours")
	closed := insert("closed", "23 days", "1 hour", "")

	subjects := func(list []Alert) []string {
		out := make([]string, 0, len(list))
		for _, a := range list {
			out = append(out, a.Subject)
		}
		return out
	}
	expect := func(t *testing.T, list []Alert, want ...string) {
		t.Helper()
		got := subjects(list)
		if len(got) != len(want) {
			t.Fatalf("got %v, wanted %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, wanted %v", got, want)
			}
		}
	}

	t.Run("the latest event comes first regardless of the state", func(t *testing.T) {
		list, err := s.Alerts(ctx, AlertsReq{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, list, "closed", "seen", "open", "stale")
		if list[0].ClosedAt == nil || list[2].ClosedAt != nil {
			t.Error("the states came back wrong: the first one is closed, the third one is open")
		}
	})

	t.Run("acknowledging lifts the alert to the top", func(t *testing.T) {
		if ok, err := s.Ack(ctx, open); err != nil || !ok {
			t.Fatalf("ack: ok=%v, err=%v", ok, err)
		}
		list, err := s.Alerts(ctx, AlertsReq{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, list, "open", "closed", "seen", "stale")
		if _, err := pool.Exec(ctx, "UPDATE alerts SET acknowledged_at = NULL WHERE id = $1", open); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a page continues below the cursor and keeps the open ones", func(t *testing.T) {
		first, err := s.Alerts(ctx, AlertsReq{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, first, "closed")
		if first[0].ID != closed {
			t.Fatalf("the first page is alert %d, %d was expected", first[0].ID, closed)
		}

		next, err := s.Alerts(ctx, AlertsReq{Limit: 50, Before: closed})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, next, "seen", "open", "stale")
		if next[0].ID != seen {
			t.Fatalf("the second page starts at alert %d, %d was expected", next[0].ID, seen)
		}

		last, err := s.Alerts(ctx, AlertsReq{Limit: 50, Before: open})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, last, "stale")
		if last[0].ID != stale {
			t.Fatalf("the last page is alert %d, %d was expected", last[0].ID, stale)
		}
	})

	t.Run("a page beyond the last alert is empty", func(t *testing.T) {
		list, err := s.Alerts(ctx, AlertsReq{Limit: 50, Before: stale})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Errorf("got %v beyond the last alert, nothing was expected", subjects(list))
		}
	})
}
