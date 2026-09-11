package store

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/deploy/migrations"
	"aacpanel/internal/testdb"
)

func TestClosedIsQuiet(t *testing.T) {
	s, err := New("postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := s.Pool(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Pool after Close returned %v, ErrClosed was expected", err)
	}
	if err := s.ensurePartitions(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("ensurePartitions after Close returned %v, ErrClosed was expected", err)
	}

	done := make(chan struct{})
	go func() {
		s.keepPartitions(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keepPartitions did not exit on a closed store")
	}

	w := NewWriter(s, "stand")
	w.startedAt = time.Now().Add(-time.Hour)
	if err := w.write(context.Background(), &containerBatch{ts: time.Now()}); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after Close returned %v, ErrClosed was expected", err)
	}
}

func TestMigrationsHaveNoPlaceholders(t *testing.T) {
	list, err := loadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no migrations were found")
	}
	for _, m := range list {
		for n := 1; n <= 9; n++ {
			ph := "$" + strconv.Itoa(n)
			if strings.Contains(m.sql, ph) {
				t.Errorf("migration %03d_%s contains the placeholder %s", m.version, m.name, ph)
			}
		}
	}
}

func TestMigrationsHaveUniqueNumbers(t *testing.T) {
	entries, err := os.ReadDir("../../deploy/migrations")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".wip")
		m := fileName.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if prev, dup := seen[n]; dup {
			t.Errorf("number %03d is taken twice: %s and %s", n, prev, e.Name())
		}
		seen[n] = e.Name()
	}
	if len(seen) == 0 {
		t.Fatal("no migrations were found — the test is meaningless")
	}
}

func TestMigrationChecksumCatchesEditedFile(t *testing.T) {
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

	list, err := loadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	last := list[len(list)-1]

	fake := strings.Repeat("f", 64)
	if _, err := pool.Exec(ctx,
		"UPDATE schema_version SET checksum = $2 WHERE version = $1", last.version, fake); err != nil {
		t.Fatalf("substituting the checksum: %v", err)
	}
	defer pool.Exec(ctx, "UPDATE schema_version SET checksum = $2 WHERE version = $1", last.version, last.sum)

	err = migrate(ctx, pool)
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("an edit to an applied migration passed silently: %v", err)
	}
	if !strings.Contains(err.Error(), last.name) {
		t.Errorf("the error does not name the file: %v", err)
	}

	if _, err := pool.Exec(ctx,
		"UPDATE schema_version SET checksum = $2 WHERE version = $1", last.version, last.sum); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, pool); err != nil {
		t.Errorf("after the checksum was restored the migrations do not pass: %v", err)
	}
}

func TestMigrateUpgradesOldJournal(t *testing.T) {
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

	saved, err := pool.Query(ctx, "SELECT version, name, checksum FROM schema_version ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		version int
		name    string
		sum     *string
	}
	var rows []row
	for saved.Next() {
		var r row
		if err := saved.Scan(&r.version, &r.name, &r.sum); err != nil {
			saved.Close()
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	saved.Close()

	if _, err := pool.Exec(ctx, "ALTER TABLE schema_version DROP COLUMN checksum"); err != nil {
		t.Fatalf("dropping the column: %v", err)
	}
	defer func() {
		pool.Exec(ctx, "ALTER TABLE schema_version ADD COLUMN IF NOT EXISTS checksum text")
		for _, r := range rows {
			pool.Exec(ctx, "UPDATE schema_version SET checksum = $2 WHERE version = $1", r.version, r.sum)
		}
	}()

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("starting on a log without the column: %v", err)
	}

	var empty int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM schema_version WHERE checksum IS NULL").Scan(&empty); err != nil {
		t.Fatal(err)
	}
	if empty > 0 {
		t.Errorf("%d rows without a checksum were left after the startup", empty)
	}
}

func partitionsBack(t *testing.T, ctx context.Context, s *Store, table string, back time.Duration) {
	t.Helper()
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	testdb.PartitionsBack(t, ctx, pool, table, back)
}
