// Package testdb gives every test run its own database.
package testdb

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Env is the variable holding the base connection string.
const Env = "AACP_TEST_DSN"

const prefix = "aacpanel_test_"

const orphanAge = 2 * time.Hour

var run = fmt.Sprintf("%d_%d", time.Now().Unix(), os.Getpid())

const rolesLockKey int64 = 0x726f6c65

const rolesWait = 5 * time.Minute

var (
	mu             sync.Mutex
	ready          = map[string]string{}
	roles          *pgx.Conn
	rolesExclusive bool
	admin          string
	swept          bool
)

var unsafeName = regexp.MustCompile(`[^a-z0-9_]`)

func runName(pkg string) string { return prefix + pkg + "_" + run }

// RunTag is the mark of this run for names that are not scoped by the database.
func RunTag() string { return run }

// LockRoles takes the role lock for good: the caller is going to edit roles.
func LockRoles(t *testing.T) {
	t.Helper()
	base := envDSN(t)
	mu.Lock()
	defer mu.Unlock()
	lockRoles(t, base, true)
}

func lockRoles(t *testing.T, base string, exclusive bool) {
	t.Helper()

	if roles != nil && (rolesExclusive || !exclusive) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), rolesWait)
	defer cancel()

	if roles == nil {
		adminDSN, err := swapDatabase(base, "postgres")
		if err != nil {
			t.Fatalf("the lock on the roles: %v", err)
		}
		conn, err := pgx.Connect(ctx, adminDSN)
		if err != nil {
			t.Fatalf("the lock on the roles, connecting to postgres: %v", err)
		}
		roles = conn
	} else {
		if _, err := roles.Exec(ctx, "SELECT pg_advisory_unlock_shared($1)", rolesLockKey); err != nil {
			t.Fatalf("the lock on the roles: %v", err)
		}
	}

	try, take := "pg_try_advisory_lock_shared", "pg_advisory_lock_shared"
	if exclusive {
		try, take = "pg_try_advisory_lock", "pg_advisory_lock"
	}

	var got bool
	if err := roles.QueryRow(ctx, "SELECT "+try+"($1)", rolesLockKey).Scan(&got); err != nil {
		t.Fatalf("the lock on the roles: %v", err)
	}
	if !got {
		log.Printf("testdb: waiting, the Postgres roles are taken by another run")
		if _, err := roles.Exec(ctx, "SELECT "+take+"($1)", rolesLockKey); err != nil {
			t.Fatalf("the Postgres roles have been taken by another run for longer than %s: %v", rolesWait, err)
		}
	}
	rolesExclusive = exclusive
}

func unlockRoles() {
	if roles == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	roles.Close(ctx)
	roles = nil
	rolesExclusive = false
}

// DSN returns the connection string of this run, creating the database on first call.
func DSN(t *testing.T) string {
	t.Helper()

	base := envDSN(t)
	name := runName(pkgOf(t))

	mu.Lock()
	defer mu.Unlock()
	if dsn, ok := ready[name]; ok {
		return dsn
	}

	if !swept {
		swept = true
		sweep(base)
	}

	dsn, err := create(base, name)
	if err != nil {
		t.Fatalf("a database of its own for the tests: %v", err)
	}
	ready[name] = dsn

	lockRoles(t, base, false)
	return dsn
}

// Main runs the tests of a package and takes the created databases away with it.
func Main(m *testing.M) int {
	code := m.Run()
	dropAll()
	mu.Lock()
	unlockRoles()
	mu.Unlock()
	return code
}

func dropAll() {
	mu.Lock()
	defer mu.Unlock()
	if admin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		log.Printf("testdb: the databases of the run were not taken away, connecting to postgres: %v", err)
		return
	}
	defer conn.Close(ctx)

	for name := range ready {
		if err := dropDatabase(ctx, conn, name); err != nil {
			log.Printf("testdb: the database %s was not taken away: %v", name, err)
			continue
		}
		delete(ready, name)
	}
}

func sweep(base string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminDSN, err := swapDatabase(base, "postgres")
	if err != nil {
		log.Printf("testdb: the cleanup was skipped: %v", err)
		return
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		log.Printf("testdb: the cleanup was skipped, connecting to postgres: %v", err)
		return
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx,
		"SELECT datname FROM pg_database WHERE datname LIKE $1", prefix+"%")
	if err != nil {
		log.Printf("testdb: the cleanup was skipped: %v", err)
		return
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		log.Printf("testdb: the cleanup was skipped: %v", err)
		return
	}

	now := time.Now()
	for _, name := range names {
		if !orphan(name, now) {
			continue
		}
		if err := dropDatabase(ctx, conn, name); err != nil {
			log.Printf("testdb: the abandoned database %s was not taken away: %v", name, err)
			continue
		}
		log.Printf("testdb: the abandoned database %s was taken away", name)
	}
}

func orphan(name string, now time.Time) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || rest == "" {
		return false
	}
	if strings.HasSuffix(name, "_"+run) {
		return false
	}
	i := strings.LastIndex(rest, "_")
	if i < 0 {
		return true
	}
	j := strings.LastIndex(rest[:i], "_")
	if j < 0 {
		return true
	}
	sec, err := strconv.ParseInt(rest[j+1:i], 10, 64)
	if err != nil {
		return true
	}
	if _, err := strconv.Atoi(rest[i+1:]); err != nil {
		return true
	}
	return now.Sub(time.Unix(sec, 0)) > orphanAge
}

func dropDatabase(ctx context.Context, conn *pgx.Conn, name string) error {
	_, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}

func envDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(Env)
	if dsn == "" {
		t.Skip(Env + " is not set")
	}
	return dsn
}

func pkgOf(t *testing.T) string {
	t.Helper()
	for skip := 2; skip < 12; skip++ {
		_, file, _, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		if strings.HasSuffix(file, "/testdb/testdb.go") {
			continue
		}
		name := strings.ToLower(filepath.Base(filepath.Dir(file)))
		return unsafeName.ReplaceAllString(name, "_")
	}
	return "unknown"
}

func create(base, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminDSN, err := swapDatabase(base, "postgres")
	if err != nil {
		return "", err
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return "", fmt.Errorf("connecting to postgres: %w", err)
	}
	defer conn.Close(ctx)

	if err := dropDatabase(ctx, conn, name); err != nil {
		return "", fmt.Errorf("dropping the previous database %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", fmt.Errorf("creating the database %s: %w", name, err)
	}
	admin = adminDSN
	return swapDatabase(base, name)
}

func swapDatabase(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		u.Path = "/" + name
		return u.String(), nil
	}
	fields := strings.Fields(dsn)
	found := false
	for i, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			fields[i] = "dbname=" + name
			found = true
		}
	}
	if !found {
		fields = append(fields, "dbname="+name)
	}
	return strings.Join(fields, " "), nil
}

// PartitionsBack cuts partitions of a table for time that has already passed.
func PartitionsBack(t *testing.T, ctx context.Context, exec Execer, table string, back time.Duration) {
	t.Helper()

	var step string
	if err := exec.QueryRow(ctx,
		"SELECT step_unit FROM partition_config WHERE relname = $1", table).Scan(&step); err != nil {
		t.Fatalf("the step of the partitions for %s: %v", table, err)
	}

	rows, err := exec.Query(ctx, `
		SELECT timezone('UTC', gs), timezone('UTC', gs + ('1 ' || $1)::interval)
		FROM generate_series(
		         date_trunc($1, timezone('UTC', now() - $2::interval)),
		         timezone('UTC', now()),
		         ('1 ' || $1)::interval) AS gs`, step, back.String())
	if err != nil {
		t.Fatalf("the ranges of the partitions of %s: %v", table, err)
	}
	defer rows.Close()

	type span struct{ from, to time.Time }
	var spans []span
	for rows.Next() {
		var s span
		if err := rows.Scan(&s.from, &s.to); err != nil {
			t.Fatal(err)
		}
		spans = append(spans, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for _, s := range spans {
		name := table + "_" + s.from.UTC().Format("20060102")
		if _, err := exec.Exec(ctx, fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
			pgx.Identifier{name}.Sanitize(), pgx.Identifier{table}.Sanitize(),
			s.from.UTC().Format(time.RFC3339), s.to.UTC().Format(time.RFC3339))); err != nil {
			t.Fatalf("the partition %s: %v", name, err)
		}
	}
}

// Execer is the little that PartitionsBack needs from a connection.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
