package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestRollupRestPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	hostID, err := s.HostID(ctx, "ROLLUP-REST")
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"metrics_disk_raw", "metrics_disk_1m", "metrics_disk_1h",
		"sessions_raw", "sessions_1m", "sessions_1h"} {
		if _, err := pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE host_id = $1", hostID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "DELETE FROM rollup_state"); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)

	for i := range 300 {
		at := base.Add(time.Duration(i) * 10 * time.Second)
		used := int64(100<<30) + int64(i)*(1<<20)
		if _, err := pool.Exec(ctx, `INSERT INTO metrics_disk_raw (ts, host_id, mount, used, total)
			VALUES ($1, $2, '/', $3, $4) ON CONFLICT DO NOTHING`, at, hostID, used, int64(500<<30)); err != nil {
			t.Fatal(err)
		}

		pct := 20.0
		if i == 150 {
			pct = 95.0
		}
		sessionID := "aaaaaaaa-0000-0000-0000-000000000001"
		if i >= 150 {
			sessionID = "bbbbbbbb-0000-0000-0000-000000000002"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO sessions_raw (ts, host_id, name, pct, tokens, messages, model, session_id, cwd)
			VALUES ($1, $2, 'keeper', $3, 1000, 10, 'opus', $4, '/srv/proj/keeper') ON CONFLICT DO NOTHING`,
			at, hostID, pct, sessionID); err != nil {
			t.Fatal(err)
		}
	}

	for range 3 {
		if err := s.rollupOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE rollup_state SET updated_at = now() - interval '1 hour'"); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("disk: what is used at the bucket's end, plus the maximum", func(t *testing.T) {
		var last, max, avg int64
		if err := pool.QueryRow(ctx, `SELECT used_last, used_max, used_avg FROM metrics_disk_1m
			WHERE host_id = $1 AND mount = '/' ORDER BY bucket LIMIT 1`, hostID).Scan(&last, &max, &avg); err != nil {
			t.Fatal(err)
		}
		if want := int64(100<<30) + 5<<20; last != want {
			t.Errorf("used_last = %d, %d was expected — that is the bucket's last measurement", last, want)
		}
		if max != last {
			t.Errorf("used_max = %d, %d was expected", max, last)
		}
		if avg >= max {
			t.Error("the mean is not below the maximum — it looks as though the same thing is computed twice")
		}
	})

	t.Run("the layer's growth reaches the rollups", func(t *testing.T) {
		if _, err := pool.Exec(ctx, "DELETE FROM metrics_container_raw WHERE host_id = $1", hostID); err != nil {
			t.Fatal(err)
		}
		for i := range 12 {
			at := base.Add(time.Duration(i) * 5 * time.Minute)
			disk := int64(100<<20) + int64(i)*(100<<20)
			if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw
				(ts, host_id, container, cpu_pct, mem_bytes, state, disk_rw)
				VALUES ($1, $2, 'fatty', 1, 1000, 'running', $3) ON CONFLICT DO NOTHING`,
				at, hostID, disk); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, "DELETE FROM rollup_state"); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if err := s.rollupOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "UPDATE rollup_state SET updated_at = now() - interval '1 hour'"); err != nil {
				t.Fatal(err)
			}
		}

		var lo, hi *int64
		if err := pool.QueryRow(ctx, `SELECT min(disk_rw_min), max(disk_rw_max) FROM metrics_container_1h
			WHERE host_id = $1 AND container = 'fatty'`, hostID).Scan(&lo, &hi); err != nil {
			t.Fatal(err)
		}
		if lo == nil || hi == nil {
			t.Fatal("the hourly rollup holds no layer sizes")
		}
		if want := int64(1100 << 20); *hi-*lo != want {
			t.Errorf("the growth from the rollup is %d bytes, %d was expected", *hi-*lo, want)
		}
	})

	t.Run("sessions: the filling peak is preserved", func(t *testing.T) {
		var pctMax, pctAvg float64
		if err := pool.QueryRow(ctx, `SELECT max(pct_max), avg(pct_avg) FROM sessions_1h
			WHERE host_id = $1 AND name = 'keeper'`, hostID).Scan(&pctMax, &pctAvg); err != nil {
			t.Fatal(err)
		}
		if pctMax != 95 {
			t.Errorf("the filling peak is %v, 95 was expected", pctMax)
		}
		if pctAvg > 25 {
			t.Errorf("the mean %v is suspiciously close to the peak, check the weighting", pctAvg)
		}
	})

	t.Run("sessions: the conversation's identifier reaches the hourly rollup", func(t *testing.T) {
		for _, table := range []string{"sessions_1m", "sessions_1h"} {
			var sessionID, cwd *string
			err := pool.QueryRow(ctx, `SELECT session_id, cwd FROM `+table+`
				WHERE host_id = $1 AND name = 'keeper' ORDER BY bucket DESC LIMIT 1`,
				hostID).Scan(&sessionID, &cwd)
			if err != nil {
				t.Fatalf("%s: %v", table, err)
			}
			if sessionID == nil {
				t.Fatalf("%s: the conversation's identifier did not arrive — there will be nothing to resume the session with", table)
			}
			if want := "bbbbbbbb-0000-0000-0000-000000000002"; *sessionID != want {
				t.Errorf("%s: uuid %q, %q was expected — the conversation taken was not the last one", table, *sessionID, want)
			}
			if cwd == nil || *cwd != "/srv/proj/keeper" {
				t.Errorf("%s: directory %v, /srv/proj/keeper was expected — there is nowhere to launch the resume", table, cwd)
			}
		}
	})
}
