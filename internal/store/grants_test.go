package store

import (
	"context"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/deploy/migrations"
	"aacpanel/internal/testdb"
)

// A migration cannot name the application role. It runs once in the life of a
// database, and the name it would have to write is not known to it: the role is
// called whatever AACP_APP_ROLE says, and on a fresh install it is created after
// the first start, when every migration has already been applied. A GRANT
// written into one therefore lands on a name nobody connects under — it issues
// nothing, and nothing goes wrong, because the rights come from grantAppRole at
// every startup. That silence is the danger: the migration reads as the place
// the rights are decided while the decision is made elsewhere, and the first
// person to trim a privilege there trims nothing.
func TestMigrationsIssueNoPrivileges(t *testing.T) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("not a single migration is embedded — the check checks nothing")
	}
	sort.Strings(names)

	verb := regexp.MustCompile(`(?i)\b(grant|revoke)\b`)
	for _, name := range names {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if !verb.MatchString(line) {
				continue
			}
			t.Errorf("%s:%d hands out privileges: %s\n\t"+
				"a migration does not know the name of the application role; the rights belong in "+
				"appPrivs and appTableGrants in grants.go, which reissues them at every startup",
				name, i+1, strings.TrimSpace(line))
		}
	}
}

func TestAppGrantsFollowRoleNamePG(t *testing.T) {
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
	owner := mustPool(t, s)

	role := "aacpanel_app_renamed_" + testdb.RunTag()
	password := ensureRoleWithGrants(t, ctx, owner, role)
	defer dropRole(t, ctx, owner, role)

	app, err := pgx.Connect(ctx, appDSNAs(t, dsn, role, password))
	if err != nil {
		t.Fatalf("connecting as %s: %v", role, err)
	}
	defer app.Close(ctx)

	var id int64
	if err := app.QueryRow(ctx, `INSERT INTO actions (kind, target, device_name)
		VALUES ('container.stop', 'renamed-role', 'test') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("the role with a configured name could not write an action attempt: %v", err)
	}
	if _, err := app.Exec(ctx, `UPDATE actions SET result = 'ok', duration_ms = 7,
		finished_at = now(), detail = 'check' WHERE id = $1`, id); err != nil {
		t.Fatalf("the role with a configured name could not append the outcome: %v", err)
	}
	if _, err := app.Exec(ctx, `TRUNCATE actions`); err == nil {
		t.Fatal("the role with a configured name wiped the whole log — it has more privileges than it needs")
	} else if !strings.Contains(err.Error(), "permission denied") &&
		!strings.Contains(err.Error(), "must be owner") {
		t.Errorf("TRUNCATE was refused by something other than privileges: %v", err)
	}
	var can bool
	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege($1, 'disk_hidden', 'UPDATE')", role).Scan(&can); err != nil {
		t.Fatal(err)
	}
	if can {
		t.Error("the renamed role has UPDATE on disk_hidden — the exceptions passed it by")
	}
}

func TestStoreGrantsAppRoleOnStartPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	setup, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer setup.Close()
	if err := setup.Open(ctx); err != nil {
		t.Fatal(err)
	}
	owner := mustPool(t, setup)

	role := "aacpanel_app_onstart_" + testdb.RunTag()
	password := makeRole(t, ctx, owner, role)
	defer func() {
		dropRole(t, ctx, owner, role)
		ensureAppRole(t, ctx, owner)
	}()
	stripRole(t, ctx, owner, role)

	var can bool
	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege($1, 'actions', 'INSERT')", role).Scan(&can); err != nil {
		t.Fatal(err)
	}
	if can {
		t.Fatal("the new role already has privileges — there is nothing to check")
	}

	s, err := New(appDSNAs(t, dsn, role, password))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UseOwner(dsn); err != nil {
		t.Fatal(err)
	}
	if err := s.Open(ctx); err != nil {
		t.Fatalf("the service did not come up under a role without privileges: %v", err)
	}

	app, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := app.QueryRow(ctx, `INSERT INTO actions (kind, target, device_name)
		VALUES ('container.stop', 'startup-issued-privileges', 'test') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("after startup the role is still without privileges: %v", err)
	}
}

func TestAppGrantsReachTableMadeLaterPG(t *testing.T) {
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
	owner := mustPool(t, s)
	ensureAppRole(t, ctx, owner)

	const table = "grant_step_probe"
	if _, err := owner.Exec(ctx,
		"CREATE TABLE IF NOT EXISTS "+table+" (id int PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	defer owner.Exec(context.Background(), "DROP TABLE IF EXISTS "+table)

	if _, err := owner.Exec(ctx, "REVOKE ALL ON "+table+" FROM monitor_app"); err != nil {
		t.Fatal(err)
	}
	var can bool
	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege('monitor_app', $1, 'SELECT')", table).Scan(&can); err != nil {
		t.Fatal(err)
	}
	if can {
		t.Fatal("the privileges on the new table were not stripped — there is nothing more to check")
	}

	if err := grantAppRole(ctx, owner, "monitor_app"); err != nil {
		t.Fatalf("the grant step: %v", err)
	}
	for _, verb := range appPrivs {
		if err := owner.QueryRow(ctx,
			"SELECT has_table_privilege('monitor_app', $1, $2)", table, verb).Scan(&can); err != nil {
			t.Fatal(err)
		}
		if !can {
			t.Errorf("a table created after the privileges were handed out has no %s: "+
				"the service learns of this at its first query to it, far from the cause", verb)
		}
	}
	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege('monitor_app', $1, 'TRUNCATE')", table).Scan(&can); err != nil {
		t.Fatal(err)
	}
	if can {
		t.Error("the step issued TRUNCATE on the new table")
	}
}

func TestAppGrantExceptionTablesExistPG(t *testing.T) {
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
	owner := mustPool(t, s)

	if len(appTableGrants) == 0 {
		t.Fatal("the list of tables with trimmed privileges is empty — the check checks nothing")
	}
	for _, g := range appTableGrants {
		var exists bool
		if err := owner.QueryRow(ctx,
			"SELECT to_regclass('public.' || $1) IS NOT NULL", g.table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("there is no table %s in the schema: the exception will not fire, and the role "+
				"will receive the blanket set of privileges on it", g.table)
		}
	}
}

func TestAppGrantSQLQuotesNamesAndOrdersRevokes(t *testing.T) {
	const evil = `evil"role`
	stmts := appGrantSQL("database", "owner", evil)

	quoted := `"evil""role"`
	for _, stmt := range stmts {
		if strings.Contains(strings.ReplaceAll(stmt, quoted, ""), evil) {
			t.Errorf("the role's name went into the SQL unescaped: %s", stmt)
		}
	}

	grantAll, revoke := -1, -1
	for i, stmt := range stmts {
		if grantAll < 0 && strings.Contains(stmt, "ON ALL TABLES IN SCHEMA public") {
			grantAll = i
		}
		if revoke < 0 && strings.HasPrefix(stmt, "REVOKE ALL ON ") {
			revoke = i
		}
	}
	if grantAll < 0 {
		t.Fatal("there is no blanket grant on all tables at all")
	}
	if revoke < 0 {
		t.Fatal("not a single REVOKE — the trimmed privileges rest on nothing")
	}
	if revoke < grantAll {
		t.Errorf("the REVOKE (step %d) comes before the blanket grant (step %d): "+
			"the blanket grant will give back what was stripped, and the surplus privilege appears silently", revoke, grantAll)
	}
}

func stripRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string) {
	t.Helper()
	if _, err := pool.Exec(ctx, "DROP OWNED BY "+quoteIdent(role)); err != nil {
		t.Fatalf("stripping the privileges from role %s: %v", role, err)
	}
}

func dropRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "DROP OWNED BY "+quoteIdent(role)); err != nil {
		t.Errorf("stripping the privileges from role %s: %v", role, err)
		return
	}
	if _, err := pool.Exec(ctx, "DROP ROLE IF EXISTS "+quoteIdent(role)); err != nil {
		t.Errorf("removing role %s: %v", role, err)
	}
}
