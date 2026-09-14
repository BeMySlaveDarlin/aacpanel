package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestNetHistoryPG(t *testing.T) {
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
	hostID, err := s.HostID(ctx, "NET-TEST")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { pool.Exec(ctx, "DELETE FROM metrics_net_raw WHERE host_id = $1", hostID) }
	cleanup()
	defer cleanup()

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	half := base.Add(30 * time.Minute)
	for _, table := range []string{"metrics_net_raw", "metrics_net_1m", "metrics_net_1h"} {
		testdb.PartitionsBack(t, ctx, pool, table, 3*time.Hour)
	}
	const ethStep = 10000
	for i := range 360 {
		at := base.Add(time.Duration(i) * 10 * time.Second)
		if _, err := pool.Exec(ctx,
			`INSERT INTO metrics_net_raw (ts, host_id, iface, rx_rate, tx_rate, rx_total, tx_total)
			 VALUES ($1, $2, 'eth', $3, 1000, $4, $5) ON CONFLICT DO NOTHING`,
			at, hostID, 1000+i, int64(i)*ethStep, int64(i)*1000); err != nil {
			t.Fatal(err)
		}
		if at.Before(half) {
			if _, err := pool.Exec(ctx,
				`INSERT INTO metrics_net_raw (ts, host_id, iface, rx_rate, tx_rate, rx_total, tx_total)
				 VALUES ($1, $2, 'wifi', 500, 500, $3, $3) ON CONFLICT DO NOTHING`,
				at, hostID, int64(i)*5000); err != nil {
				t.Fatal(err)
			}
		}
	}

	got, err := s.NetFor(ctx, NetReq{HostID: hostID, From: base, To: base.Add(time.Hour), MaxPoints: 60})
	if err != nil {
		t.Fatal(err)
	}
	if got.StepSec != 60 {
		t.Fatalf("a downsampling step of %d s, a minute was expected: an hour of measurements at MaxPoints=60", got.StepSec)
	}

	t.Run("the interfaces stand on one grid of time", func(t *testing.T) {
		if len(got.Ifaces) != 2 {
			t.Fatalf("%d interfaces in the reply, two were expected: %+v", len(got.Ifaces), got.Ifaces)
		}
		for _, iface := range got.Ifaces {
			if len(iface.Rx) != len(got.T) {
				t.Errorf("%s: %d values against %d points of time — the series drifted apart",
					iface.Name, len(iface.Rx), len(got.T))
			}
		}
	})

	t.Run("an interface that fell off stays in the reply as a hole", func(t *testing.T) {
		var wifi *NetSeries
		for i := range got.Ifaces {
			if got.Ifaces[i].Name == "wifi" {
				wifi = &got.Ifaces[i]
			}
		}
		if wifi == nil {
			t.Fatal("an interface that went quiet halfway through the period vanished from the reply entirely")
		}
		var holes, zeros int
		for _, v := range wifi.Rx {
			switch {
			case v == nil:
				holes++
			case *v == 0:
				zeros++
			}
		}
		if holes == 0 {
			t.Error("there is not a single hole after the break — the interface's silence looks like work")
		}
		if zeros > 0 {
			t.Errorf("%d buckets with a zero — an absence of data was substituted with a zero rate", zeros)
		}
	})

	t.Run("both the peak and the mean are computed", func(t *testing.T) {
		var eth *NetSeries
		for i := range got.Ifaces {
			if got.Ifaces[i].Name == "eth" {
				eth = &got.Ifaces[i]
			}
		}
		if eth == nil {
			t.Fatal("the wired interface is not in the reply")
		}
		for i := range eth.Rx {
			if eth.Rx[i] == nil || eth.RxMax[i] == nil {
				t.Errorf("point %d is empty, although the wired interface worked for the whole hour", i)
				continue
			}
			if *eth.RxMax[i] <= *eth.Rx[i] {
				t.Errorf("point %d: the peak %.0f is not above the mean %.0f — the maximum was lost in the downsampling "+
					"(step %d s, %d points, layer %s)",
					i, *eth.RxMax[i], *eth.Rx[i], got.StepSec, len(got.T), got.Resolution)
			}
		}
		want := float64(359 * ethStep)
		if eth.RxTotal != want {
			t.Errorf("%.0f bytes counted over the period, the counters say %.0f", eth.RxTotal, want)
		}
	})

	t.Run("a period without data is an empty reply and not an error", func(t *testing.T) {
		from := time.Now().UTC().AddDate(-1, 0, 0)
		empty, err := s.NetFor(ctx, NetReq{HostID: hostID, From: from, To: from.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if len(empty.Ifaces) != 0 || len(empty.T) != 0 {
			t.Errorf("%d interfaces and %d points arrived for last year", len(empty.Ifaces), len(empty.T))
		}
		if empty.Ifaces == nil || empty.T == nil {
			t.Error("the empty reply was served as null and not as []: the client will have to handle that separately")
		}
	})
}

func TestNetRollupPG(t *testing.T) {
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
	hostID, err := s.HostID(ctx, "NET-ROLLUP-TEST")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		for _, tbl := range []string{"metrics_net_raw", "metrics_net_1m", "metrics_net_1h"} {
			pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE host_id = $1", hostID)
		}
		pool.Exec(ctx, "DELETE FROM rollup_state WHERE name IN ('net_1m', 'net_1h')")
	}
	cleanup()
	defer cleanup()

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	for _, table := range []string{"metrics_net_raw", "metrics_net_1m", "metrics_net_1h"} {
		testdb.PartitionsBack(t, ctx, pool, table, 4*time.Hour)
	}

	const step = 100000
	const samples = 60
	var counter int64
	for i := range samples {
		at := base.Add(time.Duration(i) * 10 * time.Second)
		counter += step
		if _, err := pool.Exec(ctx,
			`INSERT INTO metrics_net_raw (ts, host_id, iface, rx_rate, tx_rate, rx_total, tx_total)
			 VALUES ($1, $2, 'eth', $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
			at, hostID, step/10, step/20, counter, counter/2); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.rollupOnce(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("the minute buckets do not lose traffic at the boundaries", func(t *testing.T) {
		var buckets int
		var bytes int64
		if err := pool.QueryRow(ctx,
			`SELECT count(*), coalesce(sum(rx_bytes), 0) FROM metrics_net_1m
			 WHERE host_id = $1 AND iface = 'eth'`, hostID).Scan(&buckets, &bytes); err != nil {
			t.Fatal(err)
		}
		if buckets != 10 {
			t.Errorf("%d minute buckets, 10 were expected", buckets)
		}
		if want := int64(59 * step); bytes != want {
			t.Errorf("%d bytes counted over ten minutes, the measurements give %d — traffic was lost at the seams between buckets",
				bytes, want)
		}
	})

	t.Run("an hourly bucket sums the bytes and averages the rates", func(t *testing.T) {
		var bytes int64
		var avg, max float64
		if err := pool.QueryRow(ctx,
			`SELECT rx_bytes, rx_avg::float8, rx_max::float8 FROM metrics_net_1h
			 WHERE host_id = $1 AND iface = 'eth' AND bucket = $2`, hostID, base).Scan(&bytes, &avg, &max); err != nil {
			t.Fatal(err)
		}
		if want := int64(59 * step); bytes != want {
			t.Errorf("%d bytes counted over the hour, the minute buckets give %d", bytes, want)
		}
		if avg != step/10 || max != step/10 {
			t.Errorf("the rate in the hourly bucket: mean %.0f, peak %.0f, %d was expected — the bytes were summed instead of averaged",
				avg, max, step/10)
		}
	})

	t.Run("the volume over the period is computed from the rollup", func(t *testing.T) {
		got, err := s.NetFor(ctx, NetReq{
			HostID: hostID, From: base, To: base.Add(time.Hour), Resolution: Res1h,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Ifaces) != 1 {
			t.Fatalf("%d interfaces in the reply, one was expected", len(got.Ifaces))
		}
		if want := float64(59 * step); got.Ifaces[0].RxTotal != want {
			t.Errorf("the hourly layer served %.0f bytes, %.0f was expected", got.Ifaces[0].RxTotal, want)
		}
	})

	t.Run("a host reboot does not produce negative traffic", func(t *testing.T) {
		after := base.Add(20 * time.Minute)
		for i := range 6 {
			at := after.Add(time.Duration(i) * 10 * time.Second)
			total := int64(1000000 + i*step)
			if i >= 3 {
				total = int64((i - 3) * step)
			}
			if _, err := pool.Exec(ctx,
				`INSERT INTO metrics_net_raw (ts, host_id, iface, rx_rate, tx_rate, rx_total, tx_total)
				 VALUES ($1, $2, 'eth', 10, 10, $3, $3) ON CONFLICT DO NOTHING`,
				at, hostID, total); err != nil {
				t.Fatal(err)
			}
		}
		pool.Exec(ctx, "DELETE FROM rollup_state WHERE name IN ('net_1m', 'net_1h')")
		if err := s.rollupOnce(ctx); err != nil {
			t.Fatal(err)
		}

		var bytes int64
		if err := pool.QueryRow(ctx,
			`SELECT rx_bytes FROM metrics_net_1m
			 WHERE host_id = $1 AND iface = 'eth' AND bucket = $2`, hostID, after).Scan(&bytes); err != nil {
			t.Fatal(err)
		}
		if bytes < 0 {
			t.Errorf("after the counter reset the bucket holds %d bytes — negative traffic", bytes)
		}
		if want := int64(4 * step); bytes != want {
			t.Errorf("%d bytes counted around the reboot, %d was expected", bytes, want)
		}
	})
}
