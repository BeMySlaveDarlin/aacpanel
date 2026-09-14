package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestTopPG(t *testing.T) {
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
	hostID, err := s.HostID(ctx, "TOP-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM metrics_container_raw WHERE host_id = $1", hostID); err != nil {
		t.Fatal(err)
	}

	to := time.Now().Truncate(time.Minute)
	from := to.Add(-time.Hour)
	add := func(container string, at time.Time, cpu float64, mem int64, state string, disk *int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw
			(ts, host_id, container, cpu_pct, mem_bytes, state, disk_rw)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
			at, hostID, container, cpu, mem, state, disk); err != nil {
			t.Fatal(err)
		}
	}
	for at := from; at.Before(to); at = at.Add(10 * time.Second) {
		add("quiet", at, 10, 100<<20, "running", nil)
		if at.Before(from.Add(30 * time.Minute)) {
			add("glutton", at, 80, 2<<30, "running", nil)
		}
	}
	grow := int64(100 << 20)
	for at := from; at.Before(to); at = at.Add(5 * time.Minute) {
		d := grow
		add("fatty", at, 1, 50<<20, "running", &d)
		grow += 100 << 20
	}

	find := func(rows []TopRow, name string) *TopRow {
		for i := range rows {
			if rows[i].Container == name {
				return &rows[i]
			}
		}
		return nil
	}

	t.Run("a dead one does not drop out of the memory top", func(t *testing.T) {
		top, err := s.TopFor(ctx, TopReq{HostID: hostID, Metric: "mem", From: from, To: to, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if top.SortBy != "max" {
			t.Errorf("memory is sorted by %q, max was expected: averaged usage says nothing about who hit the limit", top.SortBy)
		}
		if len(top.Rows) == 0 || top.Rows[0].Container != "glutton" {
			t.Fatalf("%+v comes first, the glutton with a peak of 2 GB was expected", top.Rows)
		}

		died := find(top.Rows, "glutton")
		if died.Alive {
			t.Error("a container whose measurements broke off half an hour ago is considered alive")
		}
		if died.Coverage > 0.6 || died.Coverage < 0.4 {
			t.Errorf("coverage %.2f, about half the period was expected", died.Coverage)
		}
		if alive := find(top.Rows, "quiet"); alive == nil || !alive.Alive || alive.Coverage < 0.9 {
			t.Errorf("the live container: %+v, alive and coverage around one were expected", alive)
		}
	})

	t.Run("the processor is measured by the mean", func(t *testing.T) {
		top, err := s.TopFor(ctx, TopReq{HostID: hostID, Metric: "cpu", From: from, To: to, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if top.SortBy != "avg" {
			t.Errorf("the processor is sorted by %q, avg was expected", top.SortBy)
		}
		if top.Rows[0].Container != "glutton" {
			t.Errorf("%q comes first, the glutton was expected", top.Rows[0].Container)
		}
	})

	t.Run("the disk is measured by growth and not by size", func(t *testing.T) {
		top, err := s.TopFor(ctx, TopReq{HostID: hostID, Metric: "disk", From: from, To: to, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if top.SortBy != "delta" || top.Resolution != ResRaw {
			t.Errorf("disk: sort %q, resolution %q — delta and raw were expected", top.SortBy, top.Resolution)
		}
		fat := find(top.Rows, "fatty")
		if fat == nil || fat.Delta == nil {
			t.Fatalf("the fatty was not found, or has no growth: %+v", top.Rows)
		}
		if want := float64(1100 << 20); *fat.Delta < want*0.9 || *fat.Delta > want*1.1 {
			t.Errorf("growth of %.0f bytes, about %.0f was expected", *fat.Delta, want)
		}
		if find(top.Rows, "quiet") != nil {
			t.Error("a container with no size measurements ended up in the disk top")
		}
	})

	t.Run("the sort order can be chosen, a malformed one is refused", func(t *testing.T) {
		top, err := s.TopFor(ctx, TopReq{HostID: hostID, Metric: "cpu", From: from, To: to, SortBy: "max"})
		if err != nil {
			t.Fatal(err)
		}
		if top.SortBy != "max" {
			t.Errorf("SortBy = %q, max was expected", top.SortBy)
		}
		if _, err := s.TopFor(ctx, TopReq{HostID: hostID, Metric: "cpu", From: from, To: to, SortBy: "by-mood"}); err == nil {
			t.Error("a malformed sort order was accepted")
		} else if _, ok := err.(ErrBadRequest); !ok {
			t.Errorf("a malformed sort order gave a %T, ErrBadRequest was expected", err)
		}
	})
}
