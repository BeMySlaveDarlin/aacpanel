package rules

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestEnginePG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := store.New(dsn)
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

	const hostName = "ENGINE-TEST"
	hostID, err := s.HostID(ctx, hostName)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM rules WHERE name LIKE 'test:%'")
		pool.Exec(ctx, "DELETE FROM metrics_host_raw WHERE host_id = $1", hostID)
		pool.Exec(ctx, "DELETE FROM metrics_container_raw WHERE host_id = $1", hostID)
	}
	cleanup()
	defer cleanup()

	var ruleID int
	if err := pool.QueryRow(ctx, `INSERT INTO rules (name, subject, op, threshold, for_sec, severity)
		VALUES ('test: cpu', 'host.cpu_pct', '>', 90, 60, 'warning') RETURNING id`).Scan(&ruleID); err != nil {
		t.Fatal(err)
	}

	e := NewEngine(s, hostName)

	cpu := func(seconds int, value float64) {
		t.Helper()
		if _, err := pool.Exec(ctx, "DELETE FROM metrics_host_raw WHERE host_id = $1", hostID); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		for offset := seconds; offset >= 0; offset -= 10 {
			if _, err := pool.Exec(ctx, `INSERT INTO metrics_host_raw (ts, host_id, cpu_pct, mem_used, mem_total)
				VALUES ($1, $2, $3, 1, 2) ON CONFLICT DO NOTHING`,
				now.Add(-time.Duration(offset)*time.Second), hostID, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	openCount := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM alerts WHERE rule_id = $1 AND closed_at IS NULL", ruleID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("a spike shorter than the hold is ignored", func(t *testing.T) {
		cpu(30, 95)
		res, err := e.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Opened != 0 || openCount() != 0 {
			t.Errorf("an alert is opened on a 30 s spike: %+v", res)
		}
	})

	t.Run("the condition holds through the whole hold — the alert opens", func(t *testing.T) {
		cpu(70, 95)
		res, err := e.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Opened != 1 || openCount() != 1 {
			t.Fatalf("the alert did not open: %+v", res)
		}

		var payload []byte
		var value, worst float64
		if err := pool.QueryRow(ctx, `SELECT payload, value, worst FROM alerts
			WHERE rule_id = $1 AND closed_at IS NULL`, ruleID).Scan(&payload, &value, &worst); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"rule", "subject", "value", "threshold", "op", "forSec"} {
			if _, ok := body[key]; !ok {
				t.Errorf("the payload has no %q: %v", key, body)
			}
		}
		if _, ok := body["suggest"]; ok {
			t.Error("a busy cpu got a suggested action — a button does not help here")
		}
		if value != 95 {
			t.Errorf("value = %v, expected 95", value)
		}
	})

	t.Run("a second pass extends the alert instead of doubling it", func(t *testing.T) {
		res, err := e.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Opened != 0 || res.Kept != 1 || openCount() != 1 {
			t.Errorf("instead of an extension it came out as %+v", res)
		}
	})

	t.Run("the condition is gone — the alert closes", func(t *testing.T) {
		cpu(70, 5)
		res, err := e.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Closed != 1 || openCount() != 0 {
			t.Errorf("the alert did not close: %+v", res)
		}

		var closed *time.Time
		if err := pool.QueryRow(ctx,
			"SELECT closed_at FROM alerts WHERE rule_id = $1 ORDER BY id DESC LIMIT 1", ruleID).Scan(&closed); err != nil {
			t.Fatal(err)
		}
		if closed == nil {
			t.Error("closed_at is not set")
		}
	})

	t.Run("too few samples do not close the alert", func(t *testing.T) {
		cpu(70, 95)
		if _, err := e.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if openCount() != 1 {
			t.Fatalf("the alert did not open, there is nothing to check")
		}
		var id int64
		if err := pool.QueryRow(ctx,
			"SELECT id FROM alerts WHERE rule_id = $1 AND closed_at IS NULL", ruleID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE alerts SET acknowledged_at = now() WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}

		cpu(20, 95)
		res, err := e.Once(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Closed != 0 {
			t.Errorf("the alert is closed because the samples ran short: %+v", res)
		}
		var stillOpen bool
		var acked *time.Time
		if err := pool.QueryRow(ctx,
			"SELECT closed_at IS NULL, acknowledged_at FROM alerts WHERE id = $1", id).Scan(&stillOpen, &acked); err != nil {
			t.Fatal(err)
		}
		if !stillOpen {
			t.Error("the alert closed while the condition holds — the acknowledgement burns with it")
		}
		if acked == nil {
			t.Error("the acknowledgement is lost")
		}
		if openCount() != 1 {
			t.Errorf("%d open alerts, expected the same single one", openCount())
		}
	})

	t.Run("flapping does not spawn a hundred alerts", func(t *testing.T) {
		for i := 0; i < stormLimit+3; i++ {
			cpu(70, 95)
			if _, err := e.Once(ctx); err != nil {
				t.Fatal(err)
			}
			cpu(70, 5)
			if _, err := e.Once(ctx); err != nil {
				t.Fatal(err)
			}
		}
		var total int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM alerts WHERE rule_id = $1", ruleID).Scan(&total); err != nil {
			t.Fatal(err)
		}
		if total > stormLimit+1 {
			t.Errorf("flapping opened %d alerts with the limit at %d", total, stormLimit)
		}
	})

	t.Run("a stack that went down carries a suggested action", func(t *testing.T) {
		var stackRule int
		if err := pool.QueryRow(ctx, `INSERT INTO rules (name, subject, op, threshold, for_sec, severity)
			VALUES ('test: stack', 'stack.running_pct', '=', 0, 60, 'warning') RETURNING id`).Scan(&stackRule); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		for offset := 70; offset >= 0; offset -= 10 {
			if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw (ts, host_id, container, state, stack)
				VALUES ($1, $2, 'c1', 'exited', 'dead-stack') ON CONFLICT DO NOTHING`,
				now.Add(-time.Duration(offset)*time.Second), hostID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := e.Once(ctx); err != nil {
			t.Fatal(err)
		}

		var payload []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM alerts
			WHERE rule_id = $1 AND closed_at IS NULL`, stackRule).Scan(&payload); err != nil {
			t.Fatalf("the alert on the stack that is down did not open: %v", err)
		}
		var body struct {
			Suggest struct {
				Kind   string `json:"kind"`
				Target string `json:"target"`
			} `json:"suggest"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		if body.Suggest.Kind != "stack.up" || body.Suggest.Target != "dead-stack" {
			t.Errorf("suggested %+v, expected bringing the stack dead-stack up", body.Suggest)
		}

		t.Run("the stack alert does not close when the samples run short", func(t *testing.T) {
			var id int64
			if err := pool.QueryRow(ctx,
				`SELECT id FROM alerts WHERE rule_id = $1 AND closed_at IS NULL`, stackRule).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx,
				"UPDATE alerts SET acknowledged_at = now() WHERE id = $1", id); err != nil {
				t.Fatal(err)
			}

			if _, err := pool.Exec(ctx,
				"DELETE FROM metrics_container_raw WHERE host_id = $1", hostID); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			for offset := 20; offset >= 0; offset -= 10 {
				if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw (ts, host_id, container, state, stack)
					VALUES ($1, $2, 'c1', 'exited', 'dead-stack') ON CONFLICT DO NOTHING`,
					now.Add(-time.Duration(offset)*time.Second), hostID); err != nil {
					t.Fatal(err)
				}
			}

			res, err := e.Once(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if res.Closed != 0 {
				t.Errorf("the stack alert is closed because the samples ran short: %+v", res)
			}
			var stillOpen bool
			var acked *time.Time
			if err := pool.QueryRow(ctx,
				"SELECT closed_at IS NULL, acknowledged_at FROM alerts WHERE id = $1", id).Scan(&stillOpen, &acked); err != nil {
				t.Fatal(err)
			}
			if !stillOpen {
				t.Error("the alert closed while the stack is still down — a new, red one opens in its place")
			}
			if acked == nil {
				t.Error("the acknowledgement is lost together with the alert")
			}
		})

		t.Run("a fresh stack does not raise an alert before the hold", func(t *testing.T) {
			now := time.Now()
			for offset := 20; offset >= 0; offset -= 10 {
				if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw (ts, host_id, container, state, stack)
					VALUES ($1, $2, 'c2', 'exited', 'fresh-stack') ON CONFLICT DO NOTHING`,
					now.Add(-time.Duration(offset)*time.Second), hostID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := e.Once(ctx); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM alerts
				WHERE rule_id = $1 AND subject = 'fresh-stack'`, stackRule).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("an alert is raised on %d samples instead of the whole hold", 3)
			}
		})
	})
}
