package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestWriterPG(t *testing.T) {
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

	w := NewWriter(s, "WRITER-TEST")
	hostID, err := s.HostID(ctx, "WRITER-TEST")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM metrics_container_raw WHERE host_id = $1", hostID); err != nil {
		t.Fatal(err)
	}

	cpu, mem := 12.5, int64(1024)
	ts := time.Now().Truncate(time.Second)
	w.Containers(ts, []ContainerSample{
		{Container: "w-sick", State: "running", Health: "unhealthy", Stack: "probe", CPU: &cpu, Mem: &mem},
		{Container: "w-plain", State: "exited"},
	})
	if w.Queued() != 1 {
		t.Fatalf("%d batches in the queue, one was expected", w.Queued())
	}
	if !w.flushOne(ctx) {
		t.Fatal("the batch was not written")
	}
	if w.Queued() != 0 {
		t.Errorf("%d batches were left in the queue after the write", w.Queued())
	}

	var health, stack *string
	var gotCPU *float64
	if err := pool.QueryRow(ctx, `SELECT health, stack, cpu_pct FROM metrics_container_raw
		WHERE host_id = $1 AND container = 'w-sick'`, hostID).Scan(&health, &stack, &gotCPU); err != nil {
		t.Fatal(err)
	}
	if health == nil || *health != "unhealthy" {
		t.Errorf("health = %v, unhealthy was expected", health)
	}
	if stack == nil || *stack != "probe" {
		t.Errorf("stack = %v, probe was expected", stack)
	}
	if gotCPU == nil || *gotCPU != 12.5 {
		t.Errorf("cpu_pct = %v, 12.5 was expected", gotCPU)
	}

	if err := pool.QueryRow(ctx, `SELECT health, stack FROM metrics_container_raw
		WHERE host_id = $1 AND container = 'w-plain'`, hostID).Scan(&health, &stack); err != nil {
		t.Fatal(err)
	}
	if health != nil || stack != nil {
		t.Errorf("the empty health/stack were written as %v/%v, NULL was expected", health, stack)
	}
}
