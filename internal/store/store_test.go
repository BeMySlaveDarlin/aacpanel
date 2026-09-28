package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

// The log of a fresh database: every file applied in its own transaction, in
// the order of the numbers, and a table that keeps the number, the name and the
// time — nothing of the file's text.
func TestFreshDatabaseLogsEveryFileInOrder(t *testing.T) {
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

	if got, want := logColumns(t, ctx, pool, "public"), []string{"version", "name", "applied_at"}; !slices.Equal(got, want) {
		t.Errorf("schema_version has the columns %v, %v was expected", got, want)
	}

	list, err := loadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, "SELECT version, name, applied_at FROM schema_version ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []logEntry
	var times []time.Time
	for rows.Next() {
		var l logEntry
		var at time.Time
		if err := rows.Scan(&l.version, &l.name, &at); err != nil {
			t.Fatal(err)
		}
		got = append(got, l)
		times = append(times, at)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var want []logEntry
	for _, m := range list {
		want = append(want, logEntry{version: m.version, name: m.name})
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the log of a fresh database is not the list of files:\n%v\nfiles:\n%v", got, want)
	}
	for i := 1; i < len(times); i++ {
		if !times[i].After(times[i-1]) {
			t.Errorf("%03d_%s was applied at %s, not after %03d_%s at %s: the files did not go one by one, each in a transaction of its own",
				got[i].version, got[i].name, times[i], got[i-1].version, got[i-1].name, times[i-1])
		}
	}
}

// An applied file whose text changed afterwards: the database remembers the
// earlier text, and the start goes on without running the file again.
func TestMigrateStartsOverAnEditedAppliedFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, schema := soloSchema(t, ctx)

	list, err := loadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	edited := list[0]
	rows := checksumLogOf(list)
	rows[0].sum = sumOf(edited.sql + "\n-- the text as it was applied")
	seedLogWithChecksums(t, ctx, pool, rows)

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("an applied file edited afterwards stopped the start: %v", err)
	}
	nothingButTheLog(t, ctx, pool, schema)
	if got := loggedIn(t, ctx, pool); !slices.Contains(got, logEntry{version: edited.version, name: edited.name}) {
		t.Errorf("%03d_%s left the log: %v", edited.version, edited.name, got)
	}
}

// The log as a machine in service holds it: every file logged with a checksum
// of its text in a column of its own, and numbers whose files are gone —
// removed from the tree, or not yet in this binary. A start drops the column,
// runs nothing and keeps every row, and so does the start after it.
func TestMigrateOnAProductionLog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, schema := soloSchema(t, ctx)

	list, err := loadMigrations(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	rows := checksumLogOf(list)
	last := list[len(list)-1].version
	var gone []int
	for v := 1; v <= last+1; v++ {
		if !slices.ContainsFunc(list, func(m migration) bool { return m.version == v }) {
			gone = append(gone, v)
			rows = append(rows, checksumRow{version: v, name: "gone", sum: sumOf("gone " + strconv.Itoa(v))})
		}
	}
	if len(gone) < 2 {
		t.Fatalf("the numbers without a file are %v: the log does not hold a removed file and a newer one", gone)
	}
	seedLogWithChecksums(t, ctx, pool, rows)

	var want []logEntry
	for _, r := range rows {
		want = append(want, logEntry{version: r.version, name: r.name})
	}
	slices.SortFunc(want, func(a, b logEntry) int { return a.version - b.version })

	for start := 1; start <= 2; start++ {
		if err := migrate(ctx, pool); err != nil {
			t.Fatalf("start %d on a log with checksums: %v", start, err)
		}
		if got := logColumns(t, ctx, pool, schema); !slices.Equal(got, []string{"version", "name", "applied_at"}) {
			t.Errorf("start %d: schema_version has the columns %v, the checksum is still kept", start, got)
		}
		nothingButTheLog(t, ctx, pool, schema)
		if got := loggedIn(t, ctx, pool); !slices.Equal(got, want) {
			t.Errorf("start %d changed the log:\n%v\nwas:\n%v", start, got, want)
		}
	}
}

// soloSchema gives a test a schema of its own and a pool that sees nothing
// else, so a log can be laid by hand: the package database is migrated by the
// other tests, and a log laid there would stand in their way.
func soloSchema(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := testdb.DSN(t)
	schema := strings.ToLower(t.Name())
	ident := pgx.Identifier{schema}.Sanitize()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+ident+" CASCADE; CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("a schema of its own: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+ident+" CASCADE"); err != nil {
			t.Errorf("dropping the schema %s: %v", schema, err)
		}
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, schema
}

type logEntry struct {
	version int
	name    string
}

type checksumRow struct {
	version int
	name    string
	sum     string
}

func checksumLogOf(list []migration) []checksumRow {
	out := make([]checksumRow, 0, len(list))
	for _, m := range list {
		out = append(out, checksumRow{version: m.version, name: m.name, sum: sumOf(m.sql)})
	}
	return out
}

func sumOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// seedLogWithChecksums lays the log with a checksum column filled in every row.
func seedLogWithChecksums(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows []checksumRow) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_version (
			version    integer     PRIMARY KEY,
			name       text        NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now(),
			checksum   text)`); err != nil {
		t.Fatalf("a log with checksums: %v", err)
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue("INSERT INTO schema_version (version, name, checksum) VALUES ($1, $2, $3)", r.version, r.name, r.sum)
	}
	if err := pool.SendBatch(ctx, batch).Close(); err != nil {
		t.Fatalf("filling the log: %v", err)
	}
}

func logColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'schema_version' ORDER BY ordinal_position`, schema)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func loggedIn(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []logEntry {
	t.Helper()
	rows, err := pool.Query(ctx, "SELECT version, name FROM schema_version ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []logEntry
	for rows.Next() {
		var l logEntry
		if err := rows.Scan(&l.version, &l.name); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// nothingButTheLog fails if a migration ran in the schema: the log is the one
// table a start may leave there when every file is logged.
func nothingButTheLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname NOT IN ('schema_version', 'schema_version_pkey')
		UNION ALL
		SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1`, schema)
	if err != nil {
		t.Fatal(err)
	}
	made, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(made) > 0 {
		t.Errorf("a logged file ran again, the schema now holds %v", made)
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
