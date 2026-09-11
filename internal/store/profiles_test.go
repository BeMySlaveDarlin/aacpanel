package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

func TestProfilesTreePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	second := mustProfile(t, s, "second", root, 20)
	first := mustProfile(t, s, "first", root, 10)

	group := mustGroup(t, s, first.ID, "services", 0)
	other := mustGroup(t, s, first.ID, "pets", 5)
	mustProject(t, s, group.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)
	mustProject(t, s, group.ID, "gatekeeper", filepath.Join(root, "gatekeeper"), 1)

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 {
		t.Fatalf("%d profiles in the map, 2 were expected", len(tree))
	}
	if tree[0].ID != first.ID || tree[1].ID != second.ID {
		t.Errorf("the profile order is %d,%d — sort was not taken into account", tree[0].ID, tree[1].ID)
	}
	if len(tree[0].Groups) != 2 || tree[0].Groups[0].ID != group.ID || tree[0].Groups[1].ID != other.ID {
		t.Fatalf("the first profile's groups are laid out wrongly: %+v", tree[0].Groups)
	}
	if len(tree[0].Groups[0].Projects) != 2 {
		t.Fatalf("%d projects in the group, 2 were expected", len(tree[0].Groups[0].Projects))
	}
	if tree[0].Groups[0].Projects[0].Name != "aacpanel" {
		t.Errorf("the first project is %q, aacpanel was expected", tree[0].Groups[0].Projects[0].Name)
	}

	if tree[1].Groups == nil {
		t.Error("a profile without groups served null instead of []")
	}
	if tree[0].Groups[1].Projects == nil {
		t.Error("a group without projects served null instead of []")
	}
	if string(tree[0].Launch) != "{}" {
		t.Errorf("the default launch parameters are %q, an empty object was expected", tree[0].Launch)
	}
	if tree[0].CreatedAt <= 0 || time.Since(time.Unix(tree[0].CreatedAt, 0)) > time.Hour {
		t.Errorf("the creation time %d looks implausible", tree[0].CreatedAt)
	}
}

func strp(v string) *string { return &v }
func intp(v int) *int       { return &v }

func TestLaunchParamsOfProjectOverrideProfilePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	e := edit("personal", root)
	e.Launch = json.RawMessage(`{"permissionMode":"plan"}`)
	p, err := s.CreateProfile(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	g := mustGroup(t, s, p.ID, "work", 0)

	name, path := "career", filepath.Join(root, "Task")
	work, err := s.CreateProject(ctx, g.ID, ProjectEdit{
		Name: &name, Path: &path, Launch: json.RawMessage(`{"permissionMode":"bypassPermissions"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	quiet := mustProject(t, s, g.ID, "aacpanel", filepath.Join(root, "aacpanel"), 1)

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, tree[0].Launch, `{"permissionMode":"plan"}`) {
		t.Errorf("the profile's default became %s", tree[0].Launch)
	}
	got := tree[0].Groups[0].Projects
	if len(got) != 2 {
		t.Fatalf("%d projects, 2 were expected", len(got))
	}
	if got[0].ID != work.ID || !sameJSON(t, got[0].Launch, `{"permissionMode":"bypassPermissions"}`) {
		t.Errorf("the career project did not override the mode: %s", got[0].Launch)
	}
	if got[1].ID != quiet.ID || !sameJSON(t, got[1].Launch, `{}`) {
		t.Errorf("a project without parameters of its own got %s, an empty object was expected", got[1].Launch)
	}
}

func profileStore(t *testing.T) (*Store, string) {
	t.Helper()
	dsn := testdb.DSN(t)

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "Projects")
	for _, dir := range []string{"aacpanel", "gatekeeper", "Labs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UseProjectRoots([]string{root}); err != nil {
		t.Fatal(err)
	}

	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"profile_projects", "profile_groups", "profiles"} {
		if _, err := pool.Exec(t.Context(), "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	return s, root
}

func sameJSON(t *testing.T, got json.RawMessage, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("the answer is not json: %s", got)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(a, b)
}

func edit(name, configDir string) ProfileEdit {
	return ProfileEdit{Name: &name, ConfigDir: &configDir}
}

func mustProfile(t *testing.T, s *Store, name, configDir string, sort int) Profile {
	t.Helper()
	e := edit(name, configDir)
	e.Sort = &sort
	p, err := s.CreateProfile(t.Context(), e)
	if err != nil {
		t.Fatalf("profile %q: %v", name, err)
	}
	return p
}

func mustGroup(t *testing.T, s *Store, profileID int, name string, sort int) ProfileGroup {
	t.Helper()
	g, err := s.CreateGroup(t.Context(), profileID, GroupEdit{Name: &name, Sort: &sort})
	if err != nil {
		t.Fatalf("group %q: %v", name, err)
	}
	return g
}

func mustProject(t *testing.T, s *Store, groupID int, name, path string, sort int) ProfileProject {
	t.Helper()
	p, err := s.CreateProject(t.Context(), groupID, ProjectEdit{Name: &name, Path: &path, Sort: &sort})
	if err != nil {
		t.Fatalf("project %q: %v", name, err)
	}
	return p
}

func mustTree(t *testing.T, s *Store) []Profile {
	t.Helper()
	tree, err := s.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func mustPool(t *testing.T, s *Store) *pgxpool.Pool {
	t.Helper()
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
