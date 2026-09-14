package store

import (
	"context"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

// A row can be stamped a moment before a period ends and written a moment
// after it. With only the period in progress on disk such a row has nowhere to
// go, and the write fails for as long as the fresh period is young — which is
// why tests that write into the past used to cut their own partitions and went
// red in the first hours of a day in UTC. The schema now keeps the period just
// ended as well, so nobody has to remember.
func TestAPartitionIsKeptForThePeriodJustEnded(t *testing.T) {
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
	hostID, err := s.HostID(ctx, "PARTITION-TEST")
	if err != nil {
		t.Fatal(err)
	}

	// A minute before the day in progress began: the sorest point there is,
	// and the one a run just after midnight in UTC lands on.
	if _, err := pool.Exec(ctx, `INSERT INTO metrics_host_raw (ts, host_id, cpu_pct)
		VALUES (date_trunc('day', timezone('UTC', now())) - interval '1 minute', $1, 1)`, hostID); err != nil {
		t.Fatalf("a row stamped a minute before the day began has nowhere to go: %v", err)
	}

	var rows []struct {
		table string
		step  string
	}
	found, err := pool.Query(ctx, "SELECT relname, step_unit FROM partition_config ORDER BY relname")
	if err != nil {
		t.Fatal(err)
	}
	for found.Next() {
		var r struct {
			table string
			step  string
		}
		if err := found.Scan(&r.table, &r.step); err != nil {
			found.Close()
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	found.Close()
	if err := found.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no table is partitioned — the test guards an empty place")
	}

	for _, r := range rows {
		var covered bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
			    SELECT 1
			    FROM pg_inherits i
			    JOIN pg_class parent ON parent.oid = i.inhparent
			    JOIN pg_class child  ON child.oid  = i.inhrelid
			    WHERE parent.relname = $1
			      AND (regexp_match(pg_get_expr(child.relpartbound, child.oid),
			                        'FROM \((?:''|'')([^'']+)'))[1]::timestamptz
			          = date_trunc($2, timezone('UTC', now())) - ('1 ' || $2)::interval)`,
			r.table, r.step).Scan(&covered); err != nil {
			t.Fatalf("the partitions of %s: %v", r.table, err)
		}
		if !covered {
			t.Errorf("%s has no partition for the %s just ended: a row stamped inside it is refused, "+
				"and every test writing into the past has to cut one for itself", r.table, r.step)
		}
	}
}
