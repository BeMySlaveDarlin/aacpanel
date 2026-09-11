package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/testdb"
)

func TestProfileTablesAppRolePG(t *testing.T) {
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

	for _, table := range []string{"profile_projects", "profile_groups", "profiles"} {
		if _, err := owner.Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}

	app, err := pgx.Connect(ctx, appDSN(t, dsn, password))
	if err != nil {
		t.Fatalf("connecting as monitor_app: %v", err)
	}
	defer app.Close(ctx)

	var profileID, groupID, projectID int
	if err := app.QueryRow(ctx, `INSERT INTO profiles (name, config_dir)
		VALUES ('privileges', '/home/u/.claude') RETURNING id`).Scan(&profileID); err != nil {
		t.Fatalf("the application could not create a profile: %v", err)
	}
	if err := app.QueryRow(ctx, `INSERT INTO profile_groups (profile_id, name)
		VALUES ($1, 'group') RETURNING id`, profileID).Scan(&groupID); err != nil {
		t.Fatalf("the application could not create a group: %v", err)
	}
	if err := app.QueryRow(ctx, `INSERT INTO profile_projects (group_id, name, path)
		VALUES ($1, 'project', '/srv/proj/x') RETURNING id`, groupID).Scan(&projectID); err != nil {
		t.Fatalf("the application could not create a project: %v", err)
	}
	if _, err := app.Exec(ctx, `UPDATE profiles SET sort = 3 WHERE id = $1`, profileID); err != nil {
		t.Fatalf("the application could not edit the profile: %v", err)
	}
	if _, err := app.Exec(ctx, `DELETE FROM profile_projects WHERE id = $1`, projectID); err != nil {
		t.Fatalf("the application could not delete the project: %v", err)
	}

	forbidden := []struct {
		what string
		sql  string
	}{
		{"drop the table", `DROP TABLE profile_projects`},
		{"rebuild the table", `ALTER TABLE profiles ADD COLUMN loophole text`},
		{"wipe the whole map", `TRUNCATE profile_groups CASCADE`},
	}
	for _, c := range forbidden {
		t.Run(c.what, func(t *testing.T) {
			if _, err := app.Exec(ctx, c.sql); err == nil {
				t.Fatalf("the application managed to %s — that is not closed off by privileges", c.what)
			} else if !strings.Contains(err.Error(), "permission denied") &&
				!strings.Contains(err.Error(), "must be owner") {
				t.Errorf("%s was refused by something other than privileges: %v", c.what, err)
			}
		})
	}
}

func TestProfileGrantsComeBackFromGrantStepPG(t *testing.T) {
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
	ensureAppRole(t, ctx, owner)

	tables := []string{"profiles", "profile_groups", "profile_projects"}
	for _, table := range tables {
		if _, err := owner.Exec(ctx, "REVOKE ALL ON "+table+" FROM monitor_app"); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx,
			"REVOKE ALL ON SEQUENCE "+table+"_id_seq FROM monitor_app"); err != nil {
			t.Fatal(err)
		}
	}

	for _, table := range tables {
		var can bool
		if err := owner.QueryRow(ctx,
			"SELECT has_table_privilege('monitor_app', $1, 'INSERT')", table).Scan(&can); err != nil {
			t.Fatal(err)
		}
		if can {
			t.Fatalf("the privileges on %s were not stripped — there is nothing more to check", table)
		}
	}

	if err := grantAppRole(ctx, owner, "monitor_app"); err != nil {
		t.Fatalf("the grant step: %v", err)
	}

	for _, table := range tables {
		for _, verb := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var can bool
			if err := owner.QueryRow(ctx,
				"SELECT has_table_privilege('monitor_app', $1, $2)", table, verb).Scan(&can); err != nil {
				t.Fatal(err)
			}
			if !can {
				t.Errorf("the step did not issue %s on %s: without it the profiles screen would be "+
					"read-only, and that would come to light in production", verb, table)
			}
		}
		var truncate bool
		if err := owner.QueryRow(ctx,
			"SELECT has_table_privilege('monitor_app', $1, 'TRUNCATE')", table).Scan(&truncate); err != nil {
			t.Fatal(err)
		}
		if truncate {
			t.Errorf("the step issued TRUNCATE on %s — the map can be wiped with a single command", table)
		}

		var seq bool
		if err := owner.QueryRow(ctx,
			"SELECT has_sequence_privilege('monitor_app', $1, 'USAGE')", table+"_id_seq").Scan(&seq); err != nil {
			t.Fatal(err)
		}
		if !seq {
			t.Errorf("the step did not issue USAGE on the %s_id_seq sequence", table)
		}
	}
}
