package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestStepFor(t *testing.T) {
	hour := time.Hour
	cases := []struct {
		name      string
		res       string
		period    time.Duration
		maxPoints int
		want      time.Duration
	}{
		{"an hour of raw data fits its own step", ResRaw, hour, 500, 10 * time.Second},
		{"a day of raw data is downsampled", ResRaw, 24 * hour, 500, 180 * time.Second},
		{"minute data over a day goes as it is", Res1m, 24 * hour, 2000, time.Minute},
		{"minute data over two weeks is downsampled", Res1m, 14 * 24 * hour, 500, 41 * time.Minute},
		{"hourly data over a year is downsampled", Res1h, 365 * 24 * hour, 500, 18 * hour},
		{"the point limit is capped from above", ResRaw, 24 * hour, 100000, 50 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			to := time.Now()
			got := stepFor(c.res, to.Add(-c.period), to, c.maxPoints)
			if got != c.want {
				t.Errorf("step %s, %s was expected", got, c.want)
			}
		})
	}
}

func TestHistoryPG(t *testing.T) {
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
	hostID, err := s.HostID(ctx, "HISTORY-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM metrics_container_raw WHERE host_id = $1", hostID); err != nil {
		t.Fatal(err)
	}

	to := time.Now().Truncate(time.Minute)
	from := to.Add(-time.Hour)
	peakAt := from.Add(30 * time.Minute)
	for _, table := range []string{"metrics_container_raw", "metrics_container_1m", "metrics_container_1h"} {
		testdb.PartitionsBack(t, ctx, pool, table, 3*time.Hour)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO metrics_container_raw (ts, host_id, container, cpu_pct, mem_bytes, state)
		SELECT g, $1, 'hist', CASE WHEN g = $2 THEN 99 ELSE 5 END, 1000, 'running'
		FROM generate_series($3::timestamptz, $4::timestamptz, interval '10 sec') g
		ON CONFLICT DO NOTHING`, hostID, peakAt, from, to.Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	res := func(period time.Duration) string {
		t.Helper()
		now := time.Now()
		return s.pickResolution(ctx, "metrics_container", now.Add(-period), now)
	}
	if got := res(time.Hour); got != ResRaw {
		t.Errorf("%s was picked for an hour, %s was expected", got, ResRaw)
	}
	if got := res(3 * 24 * time.Hour); got != Res1m {
		t.Errorf("%s was picked for three days, %s was expected", got, Res1m)
	}
	if got := res(60 * 24 * time.Hour); got != Res1h {
		t.Errorf("%s was picked for two months, %s was expected", got, Res1h)
	}

	series, err := s.SeriesFor(ctx, SeriesReq{
		HostID: hostID, Subject: "container:hist", Metric: "cpu",
		From: from, To: to, MaxPoints: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if series.Resolution != ResRaw {
		t.Errorf("resolution %s, %s was expected", series.Resolution, ResRaw)
	}
	if len(series.T) == 0 || len(series.T) > 50 {
		t.Fatalf("%d points, between 1 and 50 were expected", len(series.T))
	}
	if series.StepSec < 10 {
		t.Errorf("a step of %d s — finer than the raw data's own step", series.StepSec)
	}

	var peak float64
	for _, v := range series.Max {
		if v != nil && *v > peak {
			peak = *v
		}
	}
	if peak != 99 {
		t.Errorf("the series maximum is %v, 99 was expected: the peak was lost in the downsampling", peak)
	}
	var maxAvg float64
	for _, v := range series.Avg {
		if v != nil && *v > maxAvg {
			maxAvg = *v
		}
	}
	if maxAvg >= 99 {
		t.Errorf("the mean %v matched the peak — it looks as though avg and max are computed the same way", maxAvg)
	}

	t.Run("a gap is visible as null", func(t *testing.T) {
		gapFrom := to.Add(-15 * time.Minute)
		gapTo := to.Add(-5 * time.Minute)
		if _, err := pool.Exec(ctx, `DELETE FROM metrics_container_raw
			WHERE host_id = $1 AND container = 'hist' AND ts >= $2 AND ts < $3`, hostID, gapFrom, gapTo); err != nil {
			t.Fatal(err)
		}

		series, err := s.SeriesFor(ctx, SeriesReq{
			HostID: hostID, Subject: "container:hist", Metric: "cpu",
			From: from, To: to, MaxPoints: 60,
		})
		if err != nil {
			t.Fatal(err)
		}

		var nulls int
		for i, v := range series.Avg {
			if v == nil {
				nulls++
				continue
			}
			if i > 0 && series.T[i]-series.T[i-1] > int64(series.StepSec)*3/2 {
				t.Errorf("a break in time between points %d and %d with no null in between", i-1, i)
			}
		}
		if nulls == 0 {
			t.Error("there is not a single null in the series — the gap did not reach the client")
		}
	})

	old := time.Now().AddDate(-1, 0, 0)
	empty, err := s.SeriesFor(ctx, SeriesReq{
		HostID: hostID, Subject: "container:hist", Metric: "cpu",
		From: old, To: old.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("an empty period returned an error: %v", err)
	}
	if len(empty.T) != 0 {
		t.Errorf("%d points arrived for last year", len(empty.T))
	}

	if _, err := s.SeriesFor(ctx, SeriesReq{HostID: hostID, Subject: "container:hist", Metric: "no such metric", From: from, To: to}); err == nil {
		t.Error("an unknown metric was accepted")
	} else if _, ok := err.(ErrBadRequest); !ok {
		t.Errorf("an unknown metric gave a %T, ErrBadRequest was expected", err)
	}

	top, err := s.TopFor(ctx, TopReq{HostID: hostID, Metric: "cpu", From: from, To: to, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Rows) != 1 || top.Rows[0].Container != "hist" {
		t.Fatalf("the top returned %+v, a single hist container was expected", top.Rows)
	}
	if top.Rows[0].Max != 99 {
		t.Errorf("the maximum in the top is %v, 99 was expected", top.Rows[0].Max)
	}
}
