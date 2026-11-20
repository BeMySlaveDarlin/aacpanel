package store

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestAlertsOrderByStateAndLastEventPG(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "DELETE FROM alerts"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DELETE FROM alerts"); err != nil {
			t.Error(err)
		}
	}()
	var ruleID int
	if err := pool.QueryRow(ctx, "SELECT id FROM rules WHERE subject = 'stack.running_pct' LIMIT 1").Scan(&ruleID); err != nil {
		t.Fatal(err)
	}

	// A shared clock makes equal events rely on the id tie-breaker.
	clock := time.Now().UTC().Truncate(time.Second)
	insert := func(subject, opened, closed, acked string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO alerts (rule_id, subject, severity, payload,
                opened_at, closed_at, acknowledged_at)
            VALUES ($1, $2, 'warning', '{}',
                $6::timestamptz - $3::interval,
                CASE WHEN $4 = '' THEN NULL ELSE $6::timestamptz - $4::interval END,
                CASE WHEN $5 = '' THEN NULL ELSE $6::timestamptz - $5::interval END)
            RETURNING id`, ruleID, subject, opened, closed, acked, clock).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	last := insert("closed-old", "4 days", "2 days", "")
	insert("unread-old", "1 day", "", "")
	insert("seen-old", "2 days", "", "3 hours")
	insert("closed-fresh", "4 days", "1 hour", "")
	insert("unread-fresh", "2 hours", "", "")
	insert("seen-fresh", "2 days", "", "30 minutes")
	unread := insert("unread-tie", "2 hours", "", "")
	insert("seen-tie", "2 days", "", "30 minutes")
	insert("closed-tie", "4 days", "1 hour", "2 days")

	want := []string{"unread-tie", "unread-fresh", "unread-old", "seen-tie", "seen-fresh", "seen-old", "closed-tie", "closed-fresh", "closed-old"}
	expect := func(t *testing.T, list []Alert, want []string) {
		t.Helper()
		got := make([]string, 0, len(list))
		for _, row := range list {
			got = append(got, row.Subject)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("alerts are %v, expected %v", got, want)
		}
	}
	t.Run("unseen open alerts precede seen and closed alerts", func(t *testing.T) {
		list, err := s.Alerts(ctx, AlertsReq{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, list, want)
		for i, row := range list {
			if (row.ClosedAt != nil) != (i >= 6) || (row.AckedAt != nil) != (i >= 3 && i <= 6) {
				t.Errorf("alert %s has the wrong state at position %d", row.Subject, i)
			}
		}
	})
	t.Run("acknowledging moves an alert below the unseen ones", func(t *testing.T) {
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, "UPDATE alerts SET acknowledged_at = NULL WHERE id = $1", unread); err != nil {
				t.Error(err)
			}
		})
		if ok, err := s.Ack(ctx, unread); err != nil || !ok {
			t.Fatalf("ack: ok=%v, err=%v", ok, err)
		}
		list, err := s.Alerts(ctx, AlertsReq{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, list, append([]string{want[1], want[2], want[0]}, want[3:]...))
	})
	for _, limit := range []int{1, 2, 3, 4} {
		t.Run("pages of "+strconv.Itoa(limit)+" cross every group", func(t *testing.T) {
			var all []Alert
			var before int64
			seen := make(map[int64]bool)
			for {
				page, err := s.Alerts(ctx, AlertsReq{Limit: limit, Before: before})
				if err != nil {
					t.Fatal(err)
				}
				if len(page) == 0 {
					break
				}
				if len(page) > limit {
					t.Fatalf("page contains %d alerts, limit is %d", len(page), limit)
				}
				for _, row := range page {
					if seen[row.ID] {
						t.Fatalf("alert %s repeated across pages", row.Subject)
					}
					seen[row.ID] = true
				}
				all = append(all, page...)
				if len(all) > len(want) {
					t.Fatal("paging returned more alerts than exist")
				}
				before = page[len(page)-1].ID
			}
			expect(t, all, want)
		})
	}
	t.Run("a page beyond the last alert is empty", func(t *testing.T) {
		list, err := s.Alerts(ctx, AlertsReq{Limit: 50, Before: last})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Errorf("alerts beyond the last one: %v", list)
		}
	})
}
