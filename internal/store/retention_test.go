package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestRetentionPG(t *testing.T) {
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

	var hostID int
	if err := pool.QueryRow(ctx, `INSERT INTO hosts (name, kind) VALUES ('RETENTION-TEST', 'host')
		ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`).Scan(&hostID); err != nil {
		t.Fatal(err)
	}

	day := func(offset int) time.Time {
		return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, offset)
	}
	older, newer := day(-2), day(-1)
	for _, from := range []time.Time{older, newer} {
		name := "metrics_container_raw_" + from.Format("20060102")
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF metrics_container_raw FOR VALUES FROM ('%s') TO ('%s')`,
			quoteIdent(name), from.Format(time.RFC3339), from.AddDate(0, 0, 1).Format(time.RFC3339))); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw (ts, host_id, container, cpu_pct, state)
			VALUES ($1, $2, 'ret', 1, 'running') ON CONFLICT DO NOTHING`, from.Add(time.Hour), hostID); err != nil {
			t.Fatal(err)
		}
	}

	exists := func(name string) bool {
		t.Helper()
		var ok bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass('public.' || $1) IS NOT NULL", name).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	olderPart := "metrics_container_raw_" + older.Format("20060102")
	newerPart := "metrics_container_raw_" + newer.Format("20060102")

	var keepWas string
	if err := pool.QueryRow(ctx,
		"SELECT keep::text FROM partition_config WHERE relname = 'metrics_container_raw'").Scan(&keepWas); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx,
		"UPDATE partition_config SET keep = $1::interval WHERE relname = 'metrics_container_raw'", keepWas)

	setup := func(keep string, mark time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			"UPDATE partition_config SET keep = $1::interval WHERE relname = 'metrics_container_raw'", keep); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO rollup_state (name, last_bucket) VALUES ('container_1m', $1)
			ON CONFLICT (name) DO UPDATE SET last_bucket = excluded.last_bucket`, mark); err != nil {
			t.Fatal(err)
		}
	}

	setup("1 second", older)
	if _, err := s.retainOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !exists(olderPart) {
		t.Fatalf("%s was deleted, although the rollup never reached it", olderPart)
	}

	setup("1 second", newer)
	got, err := s.retainOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exists(olderPart) {
		t.Errorf("%s is still there, although it is rolled up and older than the retention period", olderPart)
	}
	if !exists(newerPart) {
		t.Errorf("%s was deleted, although the rollup does not cover it", newerPart)
	}
	if len(got) != 1 || got[0].part != olderPart || got[0].rows != 1 {
		t.Errorf("the report on what was thrown out: %+v, one partition %s with one row was expected", got, olderPart)
	}

	setup("30 days", day(1))
	got, err = s.retainOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		if d.table == "metrics_container_raw" {
			t.Errorf("%s was thrown out at a retention period of 30 days", d.part)
		}
	}

	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS "+quoteIdent(newerPart)); err != nil {
		t.Fatal(err)
	}
}
