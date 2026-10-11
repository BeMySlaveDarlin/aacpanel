package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/historymove"
)

func TestTransferProjectCommitsOnlyWithItsFilesPG(t *testing.T) {
	for _, failure := range []string{"", "clash", "commit"} {
		t.Run(map[string]string{"": "success", "clash": "file conflict", "commit": "map commit refused"}[failure], func(t *testing.T) {
			s, root := profileStore(t)
			from, to := filepath.Join(root, "old"), filepath.Join(root, "new")
			source := mustProfile(t, s, "source", from, 0)
			dest := mustProfile(t, s, "destination", to, 1)
			old := mustGroup(t, s, source.ID, "old", 0)
			new := mustGroup(t, s, dest.ID, "new", 0)
			project := mustProject(t, s, old.ID, "shop", filepath.Join(root, "shop"), 0)
			rel := filepath.Join("projects", historymove.Slug(project.Path), "12345678-1234-4321-8765-123456789abc.jsonl")
			write := func(path, body string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(from, rel), "conversation")
			if err := os.MkdirAll(to, 0o700); err != nil {
				t.Fatal(err)
			}
			if failure == "clash" {
				write(filepath.Join(to, rel), "different")
			}
			pool, _ := s.Pool()
			if failure == "commit" {
				_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_transfer() RETURNS trigger LANGUAGE plpgsql AS $$
    BEGIN RAISE EXCEPTION 'map commit refused'; END $$;
    CREATE CONSTRAINT TRIGGER refuse_transfer AFTER UPDATE ON profile_projects DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION reject_transfer()`)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), `DROP TRIGGER refuse_transfer ON profile_projects; DROP FUNCTION reject_transfer()`); err != nil {
						t.Error(err)
					}
				})
			}
			moved, err := s.TransferProject(t.Context(), project.ID, new.ID, func(p ProfileProject, a, b string) (func() error, error) {
				if a != from || b != to || p.Path != project.Path {
					t.Fatalf("map paths changed: %s %s %+v", a, b, p)
				}
				m, err := historymove.Prepare("move", p.Path, a, b)
				if err != nil {
					return nil, err
				}
				if err = m.Apply(t.Context(), func() error { return nil }); err != nil {
					return nil, err
				}
				return m.Rollback, nil
			})
			var actual int
			if err := pool.QueryRow(t.Context(), `SELECT group_id FROM profile_projects WHERE id=$1`, project.ID).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if failure == "" {
				if err != nil || moved.GroupID != new.ID || actual != new.ID {
					t.Fatalf("move=%+v group=%d error=%v", moved, actual, err)
				}
				if raw, err := os.ReadFile(filepath.Join(to, rel)); err != nil || string(raw) != "conversation" {
					t.Fatalf("destination: %q %v", raw, err)
				}
				if _, err := os.Stat(filepath.Join(from, rel)); !os.IsNotExist(err) {
					t.Fatal("source remains", err)
				}
				if err := historymove.Finish("move", project.Path, from, to, true); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || actual != old.ID {
					t.Fatalf("failure changed the map: group %d, error %v", actual, err)
				}
				if raw, err := os.ReadFile(filepath.Join(from, rel)); err != nil || string(raw) != "conversation" {
					t.Fatalf("source lost: %q %v", raw, err)
				}
				if failure == "commit" {
					if _, err := os.Stat(filepath.Join(to, rel)); !os.IsNotExist(err) {
						t.Fatal("rollback left a new destination file", err)
					}
				}
				if !strings.Contains(err.Error(), map[string]string{"clash": "clash", "commit": "map commit refused"}[failure]) {
					t.Fatal(err)
				}
			}
			journal, err := s.Journal(t.Context(), 20)
			if err != nil {
				t.Fatal(err)
			}
			updates := 0
			for _, e := range journal {
				if e.Entity == journalProject && e.EntityID == project.ID && e.Op == OpUpdate {
					updates++
				}
			}
			if (updates == 1) != (failure == "") {
				t.Fatalf("map journal has %d updates for failure %q", updates, failure)
			}
		})
	}
}
