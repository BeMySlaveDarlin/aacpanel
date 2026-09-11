package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func openRetentionStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	s, err := New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEveryPartitionedTableHasRetentionPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s := openRetentionStore(t, ctx)
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
		WHERE c.relkind = 'p'
		  AND c.relname NOT IN (SELECT relname FROM partition_config)
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		t.Errorf("table %s is partitioned but absent from partition_config: nobody will cut partitions for it or throw the old ones out", name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestPartitionConfigNamesRealTablesPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s := openRetentionStore(t, ctx)
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `
		SELECT relname FROM partition_config
		WHERE to_regclass('public.' || quote_ident(relname)) IS NULL
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		t.Errorf("partition_config holds %s, yet there is no such table: ensure_partitions will fail on it and never reach the rest", name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionRollupDepsAreDeclaredPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s := openRetentionStore(t, ctx)
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx,
		"SELECT relname, rollup_dep FROM partition_config WHERE rollup_dep IS NOT NULL ORDER BY relname")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var seen int
	for rows.Next() {
		var table, dep string
		if err := rows.Scan(&table, &dep); err != nil {
			t.Fatal(err)
		}
		seen++
		if !declaredRollup(dep) {
			t.Errorf("%s waits for the rollup %q, and there is no task by that name: nothing will ever throw this table's partitions out", table, dep)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("not a single reference to a rollup was read — there was nothing to check")
	}
}

func TestRetentionShoutsAboutUnknownRollupPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s := openRetentionStore(t, ctx)
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	const broken, works = "metrics_disk_raw", "metrics_host_raw"

	restore := func(table string) {
		t.Helper()
		var keep string
		var dep *string
		if err := pool.QueryRow(ctx,
			"SELECT keep::text, rollup_dep FROM partition_config WHERE relname = $1", table).Scan(&keep, &dep); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(context.Background(),
				"UPDATE partition_config SET keep = $1::interval, rollup_dep = $2 WHERE relname = $3",
				keep, dep, table); err != nil {
				t.Errorf("the setting of %s was not restored: %v", table, err)
			}
		})
	}
	restore(broken)
	restore(works)

	if _, err := pool.Exec(ctx,
		"UPDATE partition_config SET rollup_dep = 'disk_1m_typo' WHERE relname = $1", broken); err != nil {
		t.Fatal(err)
	}

	var hostID int
	if err := pool.QueryRow(ctx, `INSERT INTO hosts (name, kind) VALUES ('RETENTION-DEP-TEST', 'host')
		ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`).Scan(&hostID); err != nil {
		t.Fatal(err)
	}

	testdb.PartitionsBack(t, ctx, pool, works, 48*time.Hour)
	old := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	oldPart := works + "_" + old.Format("20060102")
	if _, err := pool.Exec(ctx, `INSERT INTO metrics_host_raw (ts, host_id, cpu_pct)
		VALUES ($1, $2, 1) ON CONFLICT DO NOTHING`, old.Add(time.Hour), hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE partition_config SET keep = '1 second'::interval WHERE relname = $1", works); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO rollup_state (name, last_bucket) VALUES ('host_1m', $1)
		ON CONFLICT (name) DO UPDATE SET last_bucket = excluded.last_bucket`, time.Now()); err != nil {
		t.Fatal(err)
	}

	got, err := s.retainOnce(ctx)
	if err == nil {
		t.Fatal("retention said nothing about a rollup that does not exist: silence here is indistinguishable from \"it has not caught up yet\", and nothing will ever throw the partitions out")
	}
	if !strings.Contains(err.Error(), broken) || !strings.Contains(err.Error(), "disk_1m_typo") {
		t.Errorf("the refusal names neither the table nor the task: %v", err)
	}

	var wasDropped bool
	for _, d := range got {
		if d.part == oldPart {
			wasDropped = true
		}
	}
	if !wasDropped {
		t.Errorf("%s was not thrown out: the failure on %s took the retention of the following tables with it", oldPart, broken)
	}
}
