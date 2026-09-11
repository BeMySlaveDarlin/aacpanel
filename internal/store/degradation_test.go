package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

type degStand struct {
	store *Store
	pool  *pgxpool.Pool
	host  int
	now   time.Time
}

func newDegStand(t *testing.T) *degStand {
	t.Helper()
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	st, err := New(dsn)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	st.Run(ctx)
	if !st.Ready() {
		t.Fatal("the database did not come up")
	}
	t.Cleanup(st.Close)

	pool, err := st.Pool()
	if err != nil {
		t.Fatalf("the pool: %v", err)
	}

	host, err := st.HostID(ctx, "stand")
	if err != nil {
		t.Fatalf("the host: %v", err)
	}

	s := &degStand{store: st, pool: pool, host: host, now: time.Now().UTC().Truncate(time.Minute)}
	s.clean(t)
	t.Cleanup(func() { s.clean(t) })
	s.partitionsBack(t)
	return s
}

func (s *degStand) clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{"metrics_container_1m", "metrics_container_1h", "metrics_host_1m", "metrics_host_1h"} {
		if _, err := s.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE host_id = $1", s.host); err != nil {
			t.Fatalf("cleaning %s: %v", table, err)
		}
	}
}

func (s *degStand) partitionsBack(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"metrics_container_1m", "metrics_container_1h",
		"metrics_host_1m", "metrics_host_1h",
	} {
		testdb.PartitionsBack(t, context.Background(), s.pool, table, 40*24*time.Hour)
	}
}

func (s *degStand) norm(t *testing.T, container string, cpu float64, mem int64) {
	t.Helper()
	for day := 1; day <= 7; day++ {
		bucket := s.now.Add(-time.Duration(day) * 24 * time.Hour).Truncate(time.Hour)
		_, err := s.pool.Exec(context.Background(), `
			INSERT INTO metrics_container_1h (bucket, host_id, container, cpu_avg, cpu_max, mem_avg, mem_max, samples)
			VALUES ($1, $2, $3, $4, $4, $5, $5, 60)
			ON CONFLICT (host_id, container, bucket) DO UPDATE SET cpu_avg = excluded.cpu_avg, mem_avg = excluded.mem_avg`,
			bucket, s.host, container, cpu, mem)
		if err != nil {
			t.Fatalf("the norm of %s: %v", container, err)
		}
	}
}

func (s *degStand) minutes(t *testing.T, container string, n int, value func(i int) (float64, int64)) {
	t.Helper()
	for i := range n {
		bucket := s.now.Add(-time.Duration(n-1-i) * time.Minute)
		cpu, mem := value(i)
		_, err := s.pool.Exec(context.Background(), `
			INSERT INTO metrics_container_1m (bucket, host_id, container, cpu_avg, cpu_max, mem_avg, mem_max, samples)
			VALUES ($1, $2, $3, $4, $4, $5, $5, 6)
			ON CONFLICT (host_id, container, bucket) DO UPDATE SET cpu_avg = excluded.cpu_avg, mem_avg = excluded.mem_avg`,
			bucket, s.host, container, cpu, mem)
		if err != nil {
			t.Fatalf("the minutes of %s: %v", container, err)
		}
	}
}

func (s *degStand) find(t *testing.T, subject, metric string) *Degradation {
	t.Helper()
	list, err := s.store.Degradations(context.Background(), DegradationReq{HostID: s.host, Now: s.now})
	if err != nil {
		t.Fatalf("the degradations: %v", err)
	}
	for i := range list {
		if list[i].Subject == subject && list[i].Metric == metric {
			return &list[i]
		}
	}
	return nil
}

const flatMem = 100 << 20

func TestDegradationCatchesSustainedLoad(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 5, flatMem)
	s.minutes(t, "shop", 30, func(int) (float64, int64) { return 40, flatMem })

	d := s.find(t, "container:shop", "cpu")
	if d == nil {
		t.Fatal("the sustained load did not make it into the degradations")
	}
	if d.Norm != 5 || d.Current != 40 {
		t.Fatalf("the norm and the current value were computed wrong: %+v", d)
	}
	if d.Ratio < 7.9 || d.Ratio > 8.1 {
		t.Fatalf("a deviation of %.2f, eightfold was expected", d.Ratio)
	}
	if d.Samples != 30 {
		t.Fatalf("%d points in the window", d.Samples)
	}
}

func TestDegradationIgnoresShortSpike(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 5, flatMem)
	s.minutes(t, "shop", 30, func(i int) (float64, int64) {
		if i >= 25 {
			return 50, flatMem
		}
		return 5, flatMem
	})

	if d := s.find(t, "container:shop", "cpu"); d != nil {
		t.Fatalf("a short spike made it into the degradations: %+v", d)
	}
}

func TestDegradationIgnoresQuietNight(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 30, flatMem)
	s.minutes(t, "shop", 30, func(int) (float64, int64) { return 2, flatMem })

	if d := s.find(t, "container:shop", "cpu"); d != nil {
		t.Fatalf("the quiet made it into the degradations: %+v", d)
	}
}

func TestDegradationClosesAfterRecovery(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 5, flatMem)
	s.minutes(t, "shop", 30, func(int) (float64, int64) { return 40, flatMem })
	if s.find(t, "container:shop", "cpu") == nil {
		t.Fatal("the load did not make it into the degradations")
	}

	s.now = s.now.Add(30 * time.Minute)
	s.minutes(t, "shop", 30, func(int) (float64, int64) { return 5, flatMem })

	if d := s.find(t, "container:shop", "cpu"); d != nil {
		t.Fatalf("the degradation stayed after the return to the norm: %+v", d)
	}
}

func TestDegradationIgnoresNoiseBelowFloor(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "tiny", 0.5, 8<<20)
	s.minutes(t, "tiny", 30, func(int) (float64, int64) { return 2, 32 << 20 })

	if d := s.find(t, "container:tiny", "cpu"); d != nil {
		t.Fatalf("noise from a micro-consumer made it into the degradations: %+v", d)
	}
	if d := s.find(t, "container:tiny", "mem"); d != nil {
		t.Fatalf("memory noise made it into the degradations: %+v", d)
	}
}

func TestDegradationSkipsSubjectWithoutNorm(t *testing.T) {
	s := newDegStand(t)
	s.minutes(t, "newcomer", 30, func(int) (float64, int64) { return 80, 900 << 20 })

	if d := s.find(t, "container:newcomer", "cpu"); d != nil {
		t.Fatalf("a container without a norm made it into the degradations: %+v", d)
	}
}

func TestDegradationReportsStart(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 5, flatMem)
	s.minutes(t, "shop", 60, func(i int) (float64, int64) {
		if i >= 30 {
			return 40, flatMem
		}
		return 5, flatMem
	})

	d := s.find(t, "container:shop", "cpu")
	if d == nil {
		t.Fatal("the load did not make it into the degradations")
	}
	want := s.now.Add(-29 * time.Minute).Unix()
	if d.Since != want {
		t.Fatalf("started at %s, expected %s",
			time.Unix(d.Since, 0).UTC(), time.Unix(want, 0).UTC())
	}
}

func TestDegradationCatchesMemoryGrowth(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 5, 200<<20)
	s.minutes(t, "shop", 30, func(int) (float64, int64) { return 5, 800 << 20 })

	d := s.find(t, "container:shop", "mem")
	if d == nil {
		t.Fatal("the memory growth did not make it into the degradations")
	}
	if d.Ratio < 3.9 || d.Ratio > 4.1 {
		t.Fatalf("a memory deviation of %.2f, fourfold was expected", d.Ratio)
	}
	if cpu := s.find(t, "container:shop", "cpu"); cpu != nil {
		t.Fatalf("the processor made it into the degradations for nothing: %+v", cpu)
	}
}

func TestDegradationFallsBackToDayNorm(t *testing.T) {
	s := newDegStand(t)

	base := s.now.Add(-24 * time.Hour).Truncate(time.Hour)
	for _, offset := range []time.Duration{0, -5 * time.Hour, -9 * time.Hour} {
		bucket := base.Add(offset)
		_, err := s.pool.Exec(context.Background(), `
			INSERT INTO metrics_container_1h (bucket, host_id, container, cpu_avg, cpu_max, mem_avg, mem_max, samples)
			VALUES ($1, $2, 'shop', 5, 5, $3, $3, 60)
			ON CONFLICT (host_id, container, bucket) DO UPDATE SET cpu_avg = excluded.cpu_avg`,
			bucket, s.host, int64(flatMem))
		if err != nil {
			t.Fatalf("the hourly points: %v", err)
		}
	}
	s.minutes(t, "shop", 30, func(int) (float64, int64) { return 40, flatMem })

	d := s.find(t, "container:shop", "cpu")
	if d == nil {
		t.Fatal("with the norm by day the degradation was not found")
	}
	if d.Basis != "day" {
		t.Fatalf("the norm's basis is %q, day was expected", d.Basis)
	}

	s.norm(t, "shop", 5, flatMem)
	d = s.find(t, "container:shop", "cpu")
	if d == nil {
		t.Fatal("with the norm by hour the degradation was not found")
	}
	if d.Basis != "hour" {
		t.Fatalf("the norm's basis is %q, hour was expected", d.Basis)
	}
}

func TestDegradationShortWindowStillWorks(t *testing.T) {
	s := newDegStand(t)
	s.norm(t, "shop", 5, flatMem)
	s.minutes(t, "shop", 30, func(i int) (float64, int64) {
		if i >= 25 {
			return 40, flatMem
		}
		return 5, flatMem
	})

	list, err := s.store.Degradations(context.Background(), DegradationReq{
		HostID: s.host, Now: s.now, Window: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("the degradations: %v", err)
	}
	var found *Degradation
	for i := range list {
		if list[i].Subject == "container:shop" && list[i].Metric == "cpu" {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatal("with a five-minute window the answer is empty — the point threshold did not adapt to the window")
	}
	if found.Samples < 3 {
		t.Fatalf("%d points in the window", found.Samples)
	}
}
