package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/deploy/migrations"
	"aacpanel/internal/testdb"
)

func TestAppRolePermissionsPG(t *testing.T) {
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
	owner, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	password := ensureAppRole(t, ctx, owner)

	app, err := pgx.Connect(ctx, appDSN(t, dsn, password))
	if err != nil {
		t.Fatalf("connecting as monitor_app: %v", err)
	}
	defer app.Close(ctx)

	var id int64
	err = app.QueryRow(ctx, `INSERT INTO actions (kind, target, device_name)
		VALUES ('container.stop', 'privilege-check', 'test') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("the application could not write an action attempt: %v", err)
	}

	if _, err := app.Exec(ctx, `UPDATE actions SET result = 'ok', duration_ms = 12,
		finished_at = now(), detail = 'session aacpanel-2 was raised' WHERE id = $1`, id); err != nil {
		t.Fatalf("the application could not append the outcome: %v", err)
	}

	forbidden := []struct {
		what string
		sql  string
	}{
		{"rewrite the past", `UPDATE actions SET kind = 'container.start' WHERE id = $1`},
		{"delete a row", `DELETE FROM actions WHERE id = $1`},
		{"wipe the whole log", `TRUNCATE actions`},
		{"disable the trigger", `ALTER TABLE actions DISABLE TRIGGER actions_append_only`},
		{"drop the trigger", `DROP TRIGGER actions_append_only ON actions`},
		{"rebuild the table", `ALTER TABLE actions ADD COLUMN loophole text`},
		{"substitute the partitioning settings", `INSERT INTO partition_config (relname, step_unit) VALUES ('not_ours', 'day')`},
	}
	defer func() {
		if _, err := owner.Exec(context.WithoutCancel(ctx),
			"DELETE FROM partition_config WHERE relname = 'not_ours'"); err != nil {
			t.Errorf("cleaning up the substituted partitioning setting: %v", err)
		}
	}()

	for _, c := range forbidden {
		t.Run(c.what, func(t *testing.T) {
			var err error
			if strings.Contains(c.sql, "$1") {
				_, err = app.Exec(ctx, c.sql, id)
			} else {
				_, err = app.Exec(ctx, c.sql)
			}
			if err == nil {
				t.Fatalf("the application managed to %s — that is not closed off by privileges", c.what)
			}
			if !strings.Contains(err.Error(), "permission denied") &&
				!strings.Contains(err.Error(), "must be owner") {
				t.Errorf("%s was refused by something other than privileges: %v", c.what, err)
			}
		})
	}

	t.Run("cutting partitions is available to the application", func(t *testing.T) {
		var made int
		if err := app.QueryRow(ctx, "SELECT ensure_partitions()").Scan(&made); err != nil {
			t.Fatalf("the application cannot cut partitions: %v", err)
		}
	})

	t.Run("metrics are written and read", func(t *testing.T) {
		var hostID int
		if err := app.QueryRow(ctx,
			`INSERT INTO hosts (name, kind) VALUES ('ROLE-TEST', 'host')
			 ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`).
			Scan(&hostID); err != nil {
			t.Fatalf("the application cannot create a host: %v", err)
		}
		if _, err := app.Exec(ctx,
			`INSERT INTO metrics_host_raw (ts, host_id, cpu_pct, load1, mem_used, mem_total, swap_used)
			 VALUES (now(), $1, 1, 0.5, 100, 200, 0)`, hostID); err != nil {
			t.Fatalf("the application cannot write metrics: %v", err)
		}
	})
}

func appDSN(t *testing.T, dsn, password string) string {
	t.Helper()
	return appDSNAs(t, dsn, "monitor_app", password)
}

func appDSNAs(t *testing.T, dsn, role, password string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, password)
	return u.String()
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func grantFiles(t *testing.T) []string {
	t.Helper()
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)

	var out []string
	for _, name := range entries {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "monitor_app") {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		t.Fatal("not a single migration with monitor_app privileges — the check checks nothing")
	}
	return out
}

func TestActionOutcomeColumnsMatchGrantsPG(t *testing.T) {
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

	frozen := map[string]bool{
		"id": true, "ts": true, "device_id": true, "device_name": true,
		"kind": true, "target": true, "params": true,
	}

	rows, err := pool.Query(ctx,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'actions'`)
	if err != nil {
		t.Fatal(err)
	}
	var outcome []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !frozen[name] {
			outcome = append(outcome, name)
		}
	}
	rows.Close()
	if len(outcome) == 0 {
		t.Fatal("no outcome columns are left in actions — the check has gone stale along with the schema")
	}
	sort.Strings(outcome)

	granted := map[string]bool{}
	for _, g := range appTableGrants {
		if g.table != "actions" {
			continue
		}
		for _, col := range g.updateColumns {
			granted[col] = true
		}
	}
	if len(granted) == 0 {
		t.Fatal("there is no column-level UPDATE on actions in appTableGrants — the check checks nothing")
	}
	for _, col := range outcome {
		if !granted[col] {
			t.Errorf("the outcome column %q is not granted to the application role for UPDATE: "+
				"once the roles are separated the service will not be able to append it", col)
		}
	}

	ensureAppRole(t, ctx, pool)
	for _, col := range outcome {
		var can bool
		if err := pool.QueryRow(ctx,
			"SELECT has_column_privilege('monitor_app', 'actions', $1, 'UPDATE')", col).Scan(&can); err != nil {
			t.Fatal(err)
		}
		if !can {
			t.Errorf("the application role cannot append the outcome column %q, although the privilege is declared", col)
		}
	}
}

func TestRetentionRunsAsOwnerPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	owner, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := owner.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := owner.Pool()
	if err != nil {
		t.Fatal(err)
	}
	password := ensureAppRole(t, ctx, pool)

	s, err := New(appDSN(t, dsn, password))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UseOwner(dsn); err != nil {
		t.Fatal(err)
	}
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}

	var hostID int
	if err := pool.QueryRow(ctx, `INSERT INTO hosts (name, kind) VALUES ('ROLE-RETENTION', 'host')
		ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`).Scan(&hostID); err != nil {
		t.Fatal(err)
	}

	from := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -3)
	part := "metrics_container_raw_" + from.Format("20060102")
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF metrics_container_raw FOR VALUES FROM ('%s') TO ('%s')`,
		quoteIdent(part), from.Format(time.RFC3339), from.AddDate(0, 0, 1).Format(time.RFC3339))); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO metrics_container_raw (ts, host_id, container, cpu_pct, state)
		VALUES ($1, $2, 'role', 1, 'running') ON CONFLICT DO NOTHING`, from.Add(time.Hour), hostID); err != nil {
		t.Fatal(err)
	}

	var keepWas string
	if err := pool.QueryRow(ctx,
		"SELECT keep::text FROM partition_config WHERE relname = 'metrics_container_raw'").Scan(&keepWas); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx,
		"UPDATE partition_config SET keep = $1::interval WHERE relname = 'metrics_container_raw'", keepWas)
	if _, err := pool.Exec(ctx,
		"UPDATE partition_config SET keep = '1 second'::interval WHERE relname = 'metrics_container_raw'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO rollup_state (name, last_bucket) VALUES ('container_1m', $1)
		ON CONFLICT (name) DO UPDATE SET last_bucket = excluded.last_bucket`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.retainOnce(ctx); err != nil {
		t.Fatalf("retention under the application role did not work: %v", err)
	}

	var alive bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.' || $1) IS NOT NULL", part).Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Errorf("partition %s is still there: retention could not throw it out under separated roles", part)
	}
}

func ensureAppRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	return ensureRoleWithGrants(t, ctx, pool, "monitor_app")
}

func ensureRoleWithGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string) string {
	t.Helper()

	password := makeRole(t, ctx, pool, role)
	if err := grantAppRole(ctx, pool, role); err != nil {
		t.Fatalf("issuing privileges to role %s: %v", role, err)
	}
	return password
}

func makeRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string) string {
	t.Helper()
	testdb.LockRoles(t)

	secret := make([]byte, 16)
	rand.Read(secret)
	password := hex.EncodeToString(secret)
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %[1]s) THEN
				ALTER ROLE %[2]s LOGIN PASSWORD %[3]s;
			ELSE
				CREATE ROLE %[2]s LOGIN PASSWORD %[3]s;
			END IF;
		END $$`, quoteLiteral(role), quoteIdent(role), quoteLiteral(password))); err != nil {
		t.Fatalf("role %s: %v", role, err)
	}
	return password
}
