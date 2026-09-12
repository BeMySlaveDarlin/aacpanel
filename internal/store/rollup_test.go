package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

func TestRollupPG(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE metrics_container_raw, metrics_container_1m,
		metrics_container_1h, metrics_host_raw, metrics_host_1m, metrics_host_1h, rollup_state`); err != nil {
		t.Fatal(err)
	}

	var hostID int
	if err := pool.QueryRow(ctx, `INSERT INTO hosts (name, kind) VALUES ('ROLLUP-TEST', 'host')
		ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`).Scan(&hostID); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	add := func(ts time.Time, cpu float64, mem int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw
			(ts, host_id, container, cpu_pct, mem_bytes, state) VALUES ($1, $2, 't-a', $3, $4, 'running')`,
			ts, hostID, cpu, mem); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 6 {
		add(base.Add(time.Duration(i)*10*time.Second), 10, 1000)
	}
	for i := range 2 {
		add(base.Add(time.Minute+time.Duration(i)*10*time.Second), 100, 2000)
	}
	add(time.Now().UTC(), 50, 3000)

	// The host rows carry a temperature in the first minute and none in the
	// second: a sensor that appears mid-hour must not drag the hourly average
	// down through the minutes it was absent from.
	host := func(ts time.Time, cpu float64, temp *float64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO metrics_host_raw
			(ts, host_id, cpu_pct, mem_used, mem_total, cpu_temp) VALUES ($1, $2, $3, 100, 200, $4)`,
			ts, hostID, cpu, temp); err != nil {
			t.Fatal(err)
		}
	}
	deg := func(v float64) *float64 { return &v }
	for i := range 6 {
		host(base.Add(time.Duration(i)*10*time.Second), 10, deg(40+float64(i)*2))
	}
	for i := range 2 {
		host(base.Add(time.Minute+time.Duration(i)*10*time.Second), 100, nil)
	}

	if err := s.rollupOnce(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("host temperatures roll up like the load", func(t *testing.T) {
		var avg, peak *float64
		if err := pool.QueryRow(ctx, `SELECT cpu_temp_avg, cpu_temp_max FROM metrics_host_1m
			WHERE host_id = $1 AND bucket = $2`, hostID, base).Scan(&avg, &peak); err != nil {
			t.Fatalf("the host minute bucket: %v", err)
		}
		if avg == nil || peak == nil || *avg != 45 || *peak != 50 {
			t.Errorf("the first minute: temperature avg %v max %v, 45/50 was expected", avg, peak)
		}
		if err := pool.QueryRow(ctx, `SELECT cpu_temp_avg, cpu_temp_max FROM metrics_host_1m
			WHERE host_id = $1 AND bucket = $2`, hostID, base.Add(time.Minute)).Scan(&avg, &peak); err != nil {
			t.Fatalf("the second host minute bucket: %v", err)
		}
		if avg != nil || peak != nil {
			t.Errorf("the second minute: temperature avg %v max %v, a null was expected — there was no sensor", avg, peak)
		}
		if err := pool.QueryRow(ctx, `SELECT cpu_temp_avg, cpu_temp_max FROM metrics_host_1h
			WHERE host_id = $1 AND bucket = $2`, hostID, base).Scan(&avg, &peak); err != nil {
			t.Fatalf("the host hourly bucket: %v", err)
		}
		if avg == nil || peak == nil {
			t.Fatalf("the hour: temperature avg %v max %v, numbers were expected", avg, peak)
		}
		if *avg != 45 {
			t.Errorf("cpu_temp_avg over the hour = %v, 45 was expected; 33.75 would mean the sensorless minute weighed in", *avg)
		}
		if *peak != 50 {
			t.Errorf("cpu_temp_max over the hour = %v, 50 was expected", *peak)
		}
	})

	type agg struct {
		cpuAvg, cpuMax float64
		memAvg, memMax int64
		samples        int
	}
	get1m := func(bucket time.Time) agg {
		t.Helper()
		var a agg
		if err := pool.QueryRow(ctx, `SELECT cpu_avg, cpu_max, mem_avg, mem_max, samples
			FROM metrics_container_1m WHERE host_id = $1 AND container = 't-a' AND bucket = $2`,
			hostID, bucket).Scan(&a.cpuAvg, &a.cpuMax, &a.memAvg, &a.memMax, &a.samples); err != nil {
			t.Fatalf("the minute bucket %s: %v", bucket, err)
		}
		return a
	}

	if a := get1m(base); a.cpuAvg != 10 || a.cpuMax != 10 || a.memAvg != 1000 || a.samples != 6 {
		t.Errorf("the first minute: %+v, cpu 10/10, mem 1000 and 6 measurements were expected", a)
	}
	if a := get1m(base.Add(time.Minute)); a.cpuAvg != 100 || a.samples != 2 {
		t.Errorf("the second minute: %+v, cpu 100 and 2 measurements were expected", a)
	}

	var h agg
	if err := pool.QueryRow(ctx, `SELECT cpu_avg, cpu_max, mem_avg, mem_max, samples
		FROM metrics_container_1h WHERE host_id = $1 AND container = 't-a' AND bucket = $2`,
		hostID, base).Scan(&h.cpuAvg, &h.cpuMax, &h.memAvg, &h.memMax, &h.samples); err != nil {
		t.Fatalf("the hourly bucket: %v", err)
	}
	if h.cpuAvg != 32.5 {
		t.Errorf("cpu_avg over the hour = %v, 32.5 (weighted) was expected; 55 would mean an average of averages", h.cpuAvg)
	}
	if h.cpuMax != 100 {
		t.Errorf("cpu_max over the hour = %v, 100 was expected: the maximum is taken from the maximums", h.cpuMax)
	}
	if h.memAvg != 1250 || h.memMax != 2000 || h.samples != 8 {
		t.Errorf("the hour: %+v, mem 1250/2000 and 8 measurements were expected", h)
	}

	var late int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metrics_container_1m
		WHERE host_id = $1 AND bucket > now() - interval '2 minutes'`, hostID).Scan(&late); err != nil {
		t.Fatal(err)
	}
	if late != 0 {
		t.Errorf("%d buckets younger than two minutes were rolled up — the minute in progress got into the aggregate", late)
	}

	before := checksum(ctx, t, pool)
	if _, err := pool.Exec(ctx, "UPDATE rollup_state SET updated_at = now() - interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	if err := s.rollupOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if after := checksum(ctx, t, pool); after != before {
		t.Errorf("a repeated rollup changed the result:\nwas:  %s\nnow:  %s", before, after)
	}
}

func checksum(ctx context.Context, t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*)::text || ':' || coalesce(sum(cpu_avg)::numeric(20,4)::text, '-') FROM metrics_container_1m)
		|| ' / ' ||
		(SELECT count(*)::text || ':' || coalesce(sum(cpu_avg)::numeric(20,4)::text, '-') FROM metrics_container_1h)`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
