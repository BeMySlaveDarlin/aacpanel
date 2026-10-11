package store

import (
	"bytes"
	"context"
	"io/fs"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"

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
	base := testdb.DSN(t)
	testdb.LockRoles(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// Roles belong to the cluster. A unique name leaves other packages' roles
	// alone while this database starts with neither schema nor application role.
	db := "aacpanel_test_bootstrap_" + testdb.RunTag()
	role := "aacpanel_app_onstart_" + testdb.RunTag()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoteIdent(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoteIdent(db)+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		if _, err := admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+quoteIdent(role)); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + db
	dsn := u.String()
	var found bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("the fresh application's role already exists")
	}
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	passwords := []string{"startup-'\\-secret", "startup-'\\-secret", "rotated-\u00a0-secret"}
	for i, password := range passwords {
		s, err := New(appDSNAs(t, dsn, role, password))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UseOwner(dsn); err != nil {
			t.Fatal(err)
		}
		// Logging every bound parameter is deliberately stricter than the
		// service logger: it catches a plaintext password sent to Postgres
		// even when the role SQL itself contains only placeholders.
		s.ownerCfg.ConnConfig.Tracer = &tracelog.TraceLog{
			LogLevel: tracelog.LogLevelTrace,
			Logger: tracelog.LoggerFunc(func(_ context.Context, _ tracelog.LogLevel, msg string, data map[string]any) {
				log.Printf("%s %v", msg, data)
			}),
		}
		if err := s.Open(ctx); err != nil {
			s.Close()
			t.Fatalf("start %d: %v", i, err)
		}
		defer s.Close()
		app := mustPool(t, s)
		var login, super, createDB, createRole bool
		if err := app.QueryRow(ctx, "SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = current_user").Scan(&login, &super, &createDB, &createRole); err != nil {
			t.Fatal(err)
		}
		if !login || super || createDB || createRole {
			t.Errorf("start %d: unexpected role attributes: login=%t super=%t createdb=%t createrole=%t", i, login, super, createDB, createRole)
		}
		var id int64
		if err := app.QueryRow(ctx, `INSERT INTO actions (kind,target,device_name) VALUES ('container.stop','startup-issued-privileges','test') RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := app.Exec(ctx, `UPDATE actions SET result='ok', duration_ms=1, finished_at=now() WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := app.Exec(ctx, "TRUNCATE actions"); err == nil {
			t.Error("the app can wipe its journal")
		}
		for _, query := range []string{
			"SELECT has_database_privilege(current_user,current_database(),'CONNECT')",
			"SELECT has_schema_privilege(current_user,'public','USAGE')",
			"SELECT has_sequence_privilege(current_user,'actions_id_seq','USAGE')",
			"SELECT has_function_privilege(current_user,'ensure_partitions(interval)','EXECUTE')",
		} {
			var can bool
			if err := app.QueryRow(ctx, query).Scan(&can); err != nil {
				t.Fatal(err)
			}
			if !can {
				t.Errorf("start %d: missing privilege: %s", i, query)
			}
		}
		owner, err := s.ownerPool()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, "REVOKE INSERT ON actions FROM "+quoteIdent(role)); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if _, err := owner.Exec(ctx, "ALTER ROLE "+quoteIdent(role)+" NOLOGIN"); err != nil {
				t.Fatal(err)
			}
		}
		s.Close()
	}
	for _, password := range passwords {
		if strings.Contains(logs.String(), password) {
			t.Error("the service log contains the application password")
		}
	}
	if !strings.Contains(logs.String(), "store: connected") {
		t.Fatal("no startup log was captured")
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
