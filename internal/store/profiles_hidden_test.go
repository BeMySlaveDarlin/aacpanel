package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func TestHiddenDirsHideAndReturnPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()
	if _, err := mustPool(t, s).Exec(ctx, "DELETE FROM disk_hidden"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "Labs")
	if err := s.HideDir(ctx, path); err != nil {
		t.Fatalf("hiding the directory: %v", err)
	}
	if hidden := mustHidden(t, s); !hidden[path] {
		t.Errorf("the hidden directory is not on the list: %v", hidden)
	}

	if err := s.HideDir(ctx, path); err != nil {
		t.Errorf("hiding it again gave an error: %v", err)
	}

	if err := s.ShowDir(ctx, path); err != nil {
		t.Fatalf("returning the directory: %v", err)
	}
	if hidden := mustHidden(t, s); hidden[path] {
		t.Errorf("the returned directory is still hidden: %v", hidden)
	}
	if err := s.ShowDir(ctx, path); err != nil {
		t.Errorf("returning something that does not exist gave an error: %v", err)
	}
}

func TestHideChecksPathButShowDoesNotPG(t *testing.T) {
	s, _ := profileStore(t)
	ctx := t.Context()

	if err := s.HideDir(ctx, "/etc"); err == nil {
		t.Error("a path outside the allowed roots got hidden — the table will become a dump of foreign paths")
	}

	root := filepath.Join(t.TempDir(), "Projects")
	if err := os.MkdirAll(filepath.Join(root, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.UseProjectRoots([]string{root}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "old")
	if err := s.HideDir(ctx, path); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(t.TempDir(), "Elsewhere")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.UseProjectRoots([]string{moved}); err != nil {
		t.Fatal(err)
	}
	if err := s.ShowDir(ctx, path); err != nil {
		t.Errorf("returning a hidden path after the roots changed gave %v — the row is locked in forever", err)
	}
	if hidden := mustHidden(t, s); hidden[path] {
		t.Error("after the return the directory is still hidden")
	}
}

func mustHidden(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	hidden, err := s.HiddenDirs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return hidden
}

func TestHiddenGrantsComeFromGrantStepPG(t *testing.T) {
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

	if _, err := owner.Exec(ctx, "REVOKE ALL ON disk_hidden FROM monitor_app"); err != nil {
		t.Fatal(err)
	}
	var can bool
	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege('monitor_app', 'disk_hidden', 'INSERT')").Scan(&can); err != nil {
		t.Fatal(err)
	}
	if can {
		t.Fatal("the privileges on disk_hidden were not stripped — there is nothing more to check")
	}

	if err := grantAppRole(ctx, owner, "monitor_app"); err != nil {
		t.Fatalf("the grant step: %v", err)
	}

	for _, verb := range []string{"SELECT", "INSERT", "DELETE"} {
		if err := owner.QueryRow(ctx,
			"SELECT has_table_privilege('monitor_app', 'disk_hidden', $1)", verb).Scan(&can); err != nil {
			t.Fatal(err)
		}
		if !can {
			t.Errorf("the step did not issue %s on disk_hidden: without it the parsing queue "+
				"cannot be cleaned up, and that would come to light in production", verb)
		}
	}
	for _, verb := range []string{"UPDATE", "TRUNCATE"} {
		if err := owner.QueryRow(ctx,
			"SELECT has_table_privilege('monitor_app', 'disk_hidden', $1)", verb).Scan(&can); err != nil {
			t.Fatal(err)
		}
		if can {
			t.Errorf("the step issued %s on disk_hidden — more privileges than an \"exists or does not\" "+
				"row needs", verb)
		}
	}
}

func TestHiddenUpdateIsRevokedByGrantStepPG(t *testing.T) {
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

	if _, err := owner.Exec(ctx, "GRANT UPDATE ON disk_hidden TO monitor_app"); err != nil {
		t.Fatal(err)
	}
	var can bool
	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege('monitor_app', 'disk_hidden', 'UPDATE')").Scan(&can); err != nil {
		t.Fatal(err)
	}
	if !can {
		t.Fatal("the UPDATE was not issued — the production picture could not be reproduced, there is nothing to check")
	}

	if err := grantAppRole(ctx, owner, "monitor_app"); err != nil {
		t.Fatalf("the grant step: %v", err)
	}

	if err := owner.QueryRow(ctx,
		"SELECT has_table_privilege('monitor_app', 'disk_hidden', 'UPDATE')").Scan(&can); err != nil {
		t.Fatal(err)
	}
	if can {
		t.Error("034 did not strip the UPDATE: in production the privilege arrives through the database's default, and " +
			"\"we never issued it\" means nothing here")
	}

	for _, verb := range []string{"SELECT", "INSERT", "DELETE"} {
		if err := owner.QueryRow(ctx,
			"SELECT has_table_privilege('monitor_app', 'disk_hidden', $1)", verb).Scan(&can); err != nil {
			t.Fatal(err)
		}
		if !can {
			t.Errorf("the step stripped %s along the way — the panel will not be able to hide directories at all", verb)
		}
	}

	if err := grantAppRole(ctx, owner, "monitor_app"); err != nil {
		t.Errorf("the step did not survive being applied again: %v", err)
	}
}
