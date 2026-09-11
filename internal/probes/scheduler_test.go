package probes

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/rules"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestProbesPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM probes WHERE name LIKE 'test:%'")
		pool.Exec(ctx, "DELETE FROM alerts WHERE subject LIKE 'test:%'")
	}
	cleanup()
	defer cleanup()

	var probeID int
	if err := pool.QueryRow(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec)
		VALUES ('test: tunnel', 'tcp', '127.0.0.1:1', 60, 5) RETURNING id`).Scan(&probeID); err != nil {
		t.Fatal(err)
	}
	var offID int
	if err := pool.QueryRow(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec, enabled)
		VALUES ('test: disabled', 'tcp', '127.0.0.1:2', 60, 5, false) RETURNING id`).Scan(&offID); err != nil {
		t.Fatal(err)
	}
	var agentID int
	if err := pool.QueryRow(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec, runner)
		VALUES ('test: from the host', 'tcp', '10.0.0.1:51820', 60, 5, 'agent') RETURNING id`).Scan(&agentID); err != nil {
		t.Fatal(err)
	}

	s := New(db)

	t.Run("the scheduler takes only its own enabled probes", func(t *testing.T) {
		list, err := s.load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var sawMine, sawOff, sawAgent bool
		for _, p := range list {
			switch p.ID {
			case probeID:
				sawMine = true
			case offID:
				sawOff = true
			case agentID:
				sawAgent = true
			}
		}
		if !sawMine {
			t.Error("an enabled probe did not make the list")
		}
		if sawOff {
			t.Error("a disabled probe made the list")
		}
		if sawAgent {
			t.Error("an agent probe reached the service scheduler — there is nothing in the container to run it with")
		}
	})

	t.Run("the result is saved with its reason", func(t *testing.T) {
		res := Check(ctx, s.client, Probe{Kind: "tcp", Target: "127.0.0.1:1", Timeout: 2 * time.Second})
		if res.OK {
			t.Fatal("connecting to a closed port counted as a success")
		}
		if err := s.save(ctx, Probe{ID: probeID}, res); err != nil {
			t.Fatal(err)
		}
		var outcome string
		if err := pool.QueryRow(ctx,
			"SELECT outcome FROM probe_results WHERE probe_id = $1 ORDER BY ts DESC LIMIT 1", probeID).Scan(&outcome); err != nil {
			t.Fatal(err)
		}
		if outcome != string(Network) {
			t.Errorf("outcome %q, expected %q", outcome, Network)
		}
	})

	engine := rules.NewEngine(db, "PROBE-TEST")
	fail := func(n int) {
		t.Helper()
		for i := range n {
			if _, err := pool.Exec(ctx, `INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome, error)
				VALUES (now() - make_interval(secs => $2), $1, false, 5000, 'timeout', 'no answer')
				ON CONFLICT DO NOTHING`, probeID, float64((n-i)*60)); err != nil {
				t.Fatal(err)
			}
		}
	}
	openAlerts := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM alerts a
			JOIN rules r ON r.id = a.rule_id
			WHERE r.subject = 'probe.fail_streak' AND a.subject = 'test: tunnel' AND a.closed_at IS NULL`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("one failure is not an event", func(t *testing.T) {
		pool.Exec(ctx, "DELETE FROM probe_results WHERE probe_id = $1", probeID)
		fail(1)
		if _, err := engine.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if openAlerts() != 0 {
			t.Error("an alert opened on a single failure — a single timeout to an external API is a fact of life")
		}
	})

	t.Run("three in a row raise an alert", func(t *testing.T) {
		fail(3)
		if _, err := engine.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if openAlerts() != 1 {
			t.Fatalf("no alert opened after three failures in a row")
		}
	})

	t.Run("the probe passes and the alert closes", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome)
			VALUES (now(), $1, true, 12, 'ok') ON CONFLICT DO NOTHING`, probeID); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if openAlerts() != 0 {
			t.Error("the alert stayed open while the probe passes again")
		}
	})

	t.Run("the history lives by the common rules", func(t *testing.T) {
		var step, keep string
		if err := pool.QueryRow(ctx,
			"SELECT step_unit, keep::text FROM partition_config WHERE relname = 'probe_results'").Scan(&step, &keep); err != nil {
			t.Fatalf("probe_results is not registered in partition_config: %v", err)
		}
		var partitions int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_inherits i ON i.inhrelid = c.oid
			JOIN pg_class p ON p.oid = i.inhparent WHERE p.relname = 'probe_results'`).Scan(&partitions); err != nil {
			t.Fatal(err)
		}
		if partitions == 0 {
			t.Error("no partitions are cut for probe_results")
		}
	})
}
