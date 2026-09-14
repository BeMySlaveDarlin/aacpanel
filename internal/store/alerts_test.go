package store

import (
	"context"
	"strconv"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestAlertsAndProbesPG(t *testing.T) {
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

	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM alerts")
		pool.Exec(ctx, "DELETE FROM probes WHERE name LIKE 'reading:%'")
	}
	cleanup()
	defer cleanup()

	var ruleID int
	if err := pool.QueryRow(ctx,
		"SELECT id FROM rules WHERE subject = 'stack.running_pct' LIMIT 1").Scan(&ruleID); err != nil {
		t.Fatal(err)
	}

	// The closed ones were opened yesterday and closed a minute and two minutes
	// ago: the latest event of each is the closing.
	var closedIDs []int64
	for i, subj := range []string{"old-1", "old-2"} {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO alerts (rule_id, subject, severity, value, worst, opened_at, closed_at, payload)
			VALUES ($1, $2, 'warning', 1, 2, now() - interval '1 day', now() - make_interval(mins => $3), '{}')
			RETURNING id`, ruleID, subj, i+1).Scan(&id); err != nil {
			t.Fatal(err)
		}
		closedIDs = append(closedIDs, id)
	}
	var liveID int64
	if err := pool.QueryRow(ctx, `INSERT INTO alerts (rule_id, subject, severity, value, worst, payload)
		VALUES ($1, 'alive', 'critical', 0, 0, '{"suggest": {"kind": "stack.up", "target": "alive"}}')
		RETURNING id`, ruleID).Scan(&liveID); err != nil {
		t.Fatal(err)
	}

	own := map[int64]bool{liveID: true}
	for _, id := range closedIDs {
		own[id] = true
	}
	mine := func(list []Alert) []Alert {
		out := make([]Alert, 0, len(own))
		for _, a := range list {
			if own[a.ID] {
				out = append(out, a)
			}
		}
		return out
	}

	t.Run("the latest event comes first", func(t *testing.T) {
		all, err := s.Alerts(ctx, AlertsReq{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		list := mine(all)
		if len(list) != 3 {
			t.Fatalf("%d alerts of our own, 3 were expected", len(list))
		}
		if list[0].Subject != "alive" || list[0].ClosedAt != nil {
			t.Errorf("%q comes first (closed: %v), the one just opened was expected", list[0].Subject, list[0].ClosedAt != nil)
		}
		if list[0].Suggested == nil || list[0].Suggested.Kind != "stack.up" {
			t.Errorf("the suggested action did not parse: %+v", list[0].Suggested)
		}
		if list[1].Subject != "old-1" || list[2].Subject != "old-2" {
			t.Errorf("the closed ones run %q, %q; the one closed a minute ago was expected before the one closed two minutes ago",
				list[1].Subject, list[2].Subject)
		}
	})

	t.Run("a page continues below the cursor and repeats nothing", func(t *testing.T) {
		all, err := s.Alerts(ctx, AlertsReq{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		first := mine(all)
		cursor := first[1]
		next, err := s.Alerts(ctx, AlertsReq{Limit: 50, Before: cursor.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(next) != 1 || next[0].ID != first[2].ID {
			t.Fatalf("below alert %d came %d alerts, only alert %d was expected", cursor.ID, len(next), first[2].ID)
		}
		if next[0].ClosedAt == nil || *next[0].ClosedAt > *cursor.ClosedAt {
			t.Errorf("alert %d is not older than the cursor by its latest event", next[0].ID)
		}
	})

	find := func(t *testing.T, list []Alert, id int64) Alert {
		t.Helper()
		for _, a := range list {
			if a.ID == id {
				return a
			}
		}
		t.Fatalf("alert %d disappeared from the result", id)
		return Alert{}
	}

	t.Run("acknowledging silences the counter but does not close the alert", func(t *testing.T) {
		list, err := s.Alerts(ctx, AlertsReq{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		live := find(t, list, liveID)
		if live.AckedAt != nil {
			t.Fatalf("a fresh alert is already acknowledged: %v", *live.AckedAt)
		}

		ok, err := s.Ack(ctx, live.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("the open alert was not acknowledged")
		}

		list, err = s.Alerts(ctx, AlertsReq{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		after := find(t, list, liveID)
		if after.AckedAt == nil {
			t.Error("the acknowledgement did not come back in the result")
		}
		if after.ClosedAt != nil {
			t.Error("the acknowledgement closed the alert")
		}

		again, err := s.Ack(ctx, live.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again {
			t.Error("a repeated acknowledgement moved the mark")
		}

		closed := find(t, list, closedIDs[0])
		if closed.ClosedAt == nil {
			t.Fatal("there is nothing to acknowledge: this alert is open")
		}
		if ok, err := s.Ack(ctx, closed.ID); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Error("a closed alert accepted an acknowledgement")
		}

		if _, err := pool.Exec(ctx, "UPDATE alerts SET acknowledged_at = NULL WHERE id = $1", live.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("the probes serve their outcomes separately", func(t *testing.T) {
		var probeID int
		if err := pool.QueryRow(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec)
			VALUES ('reading: network', 'tcp', 'x:443', 60, 5) RETURNING id`).Scan(&probeID); err != nil {
			t.Fatal(err)
		}
		var httpID int
		if err := pool.QueryRow(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec)
			VALUES ('reading: service', 'http', 'http://x/health', 60, 5) RETURNING id`).Scan(&httpID); err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			if _, err := pool.Exec(ctx, `INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome, error)
				VALUES (now() - make_interval(secs => $2), $1, false, 5000, 'network', 'no route')`,
				probeID, float64(60*(2-i))); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome, error)
			VALUES (now(), $1, false, 120, 'status', 'answered 500')`, httpID); err != nil {
			t.Fatal(err)
		}

		list, err := s.Probes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		byName := map[string]ProbeState{}
		for _, p := range list {
			byName[p.Name] = p
		}

		net := byName["reading: network"]
		if net.Last == nil || net.Last.Outcome != "network" {
			t.Errorf("the network probe: %+v, an outcome of network was expected", net.Last)
		}
		if net.FailStreak != 2 {
			t.Errorf("a failure streak of %d, 2 was expected", net.FailStreak)
		}

		svc := byName["reading: service"]
		if svc.Last == nil || svc.Last.Outcome != "status" {
			t.Errorf("the service probe: %+v, an outcome of status was expected", svc.Last)
		}
		if net.Last.Outcome == svc.Last.Outcome {
			t.Error("the outcomes collapsed together: the network and the service's answer must differ")
		}

		if net.LastOK != nil {
			t.Errorf("the probe has never passed, yet there is a success mark: %v", *net.LastOK)
		}
	})

	t.Run("the time of the last successful probe", func(t *testing.T) {
		var id int
		if err := pool.QueryRow(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec)
			VALUES ('reading: with history', 'tcp', 'z:443', 60, 5) RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome)
			VALUES (now() - interval '30 min', $1, true, 12, 'ok')`, id); err != nil {
			t.Fatal(err)
		}
		for i := range 3 {
			if _, err := pool.Exec(ctx, `INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome, error)
				VALUES (now() - make_interval(secs => $2), $1, false, 5000, 'timeout', 'no answer')`,
				id, float64(60*(3-i))); err != nil {
				t.Fatal(err)
			}
		}

		list, err := s.Probes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got *ProbeState
		for i := range list {
			if list[i].ID == id {
				got = &list[i]
			}
		}
		if got == nil {
			t.Fatal("the probe disappeared from the result")
		}
		if got.LastOK == nil {
			t.Fatal("the probe passed half an hour ago, yet there is no success mark")
		}
		age := time.Since(time.Unix(*got.LastOK, 0))
		if age < 29*time.Minute || age > 31*time.Minute {
			t.Errorf("the last success was %s ago, about half an hour was expected", age.Round(time.Minute))
		}
		if got.FailStreak != 3 {
			t.Errorf("a failure streak of %d, 3 was expected", got.FailStreak)
		}
	})

	t.Run("a probe without results does not break the result", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec)
			VALUES ('reading: new', 'tcp', 'y:443', 60, 5)`); err != nil {
			t.Fatal(err)
		}
		list, err := s.Probes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range list {
			if p.Name == "reading: new" && p.Last != nil {
				t.Errorf("a result came from nowhere for the new probe: %+v", p.Last)
			}
		}
	})
}

// The badge counts every open alert, not the ones that fit on a page: alerts
// come back sorted by their latest event, so an open alert with enough fresher
// events above it is off the first page while it still stands open.
func TestAlertCountsIgnorePagesPG(t *testing.T) {
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

	// Opened a day ago and never touched since: every later event outranks it.
	if _, err := pool.Exec(ctx, `INSERT INTO alerts (rule_id, subject, severity, value, worst, opened_at, payload)
		VALUES ($1, 'buried', 'critical', 1, 2, now() - interval '1 day', '{}')`, ruleID); err != nil {
		t.Fatal(err)
	}
	// Open but already seen: it belongs to the open ones and not to the badge.
	if _, err := pool.Exec(ctx, `INSERT INTO alerts (rule_id, subject, severity, value, worst, opened_at, acknowledged_at, payload)
		VALUES ($1, 'seen', 'warning', 1, 2, now() - interval '1 day', now() - interval '1 minute', '{}')`, ruleID); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := pool.Exec(ctx, `INSERT INTO alerts (rule_id, subject, severity, value, worst, opened_at, closed_at, payload)
			VALUES ($1, $2, 'info', 1, 2, now() - interval '1 day', now() - make_interval(secs => $3), '{}')`,
			ruleID, "closed-"+strconv.Itoa(i), i); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.Alerts(ctx, AlertsReq{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range page {
		if a.Subject == "buried" {
			t.Fatal("the buried alert is on the first page — the test proves nothing about the count")
		}
	}

	counts, err := s.AlertCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Open != 2 {
		t.Errorf("%d alerts stand open, 2 were expected (the buried one and the seen one)", counts.Open)
	}
	if counts.Unread != 1 {
		t.Errorf("the badge would show %d, 1 was expected: the buried alert alone is open and unseen", counts.Unread)
	}
}
