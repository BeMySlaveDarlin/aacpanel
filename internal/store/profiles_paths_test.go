package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectPathStaysInsideRoots(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Projects")
	sibling := filepath.Join(base, "Projects-2")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, sibling, outside, filepath.Join(root, "aacpanel")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "outwards")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "aacpanel"), filepath.Join(root, "inwards")); err != nil {
		t.Fatal(err)
	}
	roots := []string{root}

	ok := []struct {
		what string
		path string
		want string
	}{
		{"the root itself", root, root},
		{"a directory inside", filepath.Join(root, "aacpanel"), filepath.Join(root, "aacpanel")},
		{"a trailing slash is cut", filepath.Join(root, "aacpanel") + "/", filepath.Join(root, "aacpanel")},
		{"a symlink pointing inside the root", filepath.Join(root, "inwards"), filepath.Join(root, "inwards")},
		{"the directory does not exist yet", filepath.Join(root, "future"), filepath.Join(root, "future")},
	}
	for _, c := range ok {
		t.Run(c.what, func(t *testing.T) {
			got, err := CheckProjectPath(c.path, roots)
			if err != nil {
				t.Fatalf("%s was rejected: %v", c.path, err)
			}
			if got != c.want {
				t.Errorf("%s was normalised into %q, %q was expected", c.path, got, c.want)
			}
		})
	}

	bad := []struct {
		what string
		path string
	}{
		{"empty", ""},
		{"relative", "Projects/aacpanel"},
		{"the home tilde is not expanded", "~/Projects"},
		{"a neighbour sharing a prefix", filepath.Join(sibling, "aacpanel")},
		{"outside the roots altogether", outside},
		{"an escape through two dots", filepath.Join(root, "..", "outside")},
		{"a symlink pointing outwards", filepath.Join(root, "outwards")},
		{"a directory behind an outward symlink", filepath.Join(root, "outwards", "foreign")},
		{"a null byte", root + "/pro\x00ject"},
	}
	for _, c := range bad {
		t.Run(c.what, func(t *testing.T) {
			got, err := CheckProjectPath(c.path, roots)
			if err == nil {
				t.Fatalf("%q was accepted as %q — the path would travel into a session launch", c.path, got)
			}
			var badReq ErrBadRequest
			if !errors.As(err, &badReq) {
				t.Errorf("%q was rejected with a %T, ErrBadRequest was expected: HTTP will answer 500 instead of 400", c.path, err)
			}
		})
	}

	t.Run("without roots we do not write", func(t *testing.T) {
		_, err := CheckProjectPath(filepath.Join(root, "aacpanel"), nil)
		if err == nil {
			t.Fatal("a path was accepted without any roots set: the check is cancelled entirely")
		}
		var badReq ErrBadRequest
		if errors.As(err, &badReq) {
			t.Error("unset roots were passed off as a client error — the wrong thing will be fixed")
		}
	})
}

func TestProjectRootsRefuseEverything(t *testing.T) {
	s := &Store{}
	for _, root := range []string{"/", "Projects", "./Projects"} {
		if err := s.UseProjectRoots([]string{root}); err == nil {
			t.Errorf("the root %q was accepted: after that the path check means nothing", root)
		}
	}
	if err := s.UseProjectRoots([]string{"/srv/proj/", "  ", "/home/u"}); err != nil {
		t.Fatalf("normal roots were rejected: %v", err)
	}
	if got := s.ProjectRoots(); len(got) != 2 || got[0] != "/srv/proj" || got[1] != "/home/u" {
		t.Errorf("the roots were parsed into %q, [/srv/proj /home/u] was expected", got)
	}
}

func TestStoreRefusesForeignPathPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, p.ID, "group", 0)

	foreign := filepath.Dir(root) + "-foreign/project"
	name, path := "foreign", foreign
	_, err := s.CreateProject(ctx, g.ID, ProjectEdit{Name: &name, Path: &path})
	if err == nil {
		t.Fatal("a project with a foreign path was created")
	}
	var bad ErrBadRequest
	if !errors.As(err, &bad) {
		t.Fatalf("a foreign path gave a %T (%v), ErrBadRequest was expected", err, err)
	}

	good := mustProject(t, s, g.ID, "own", filepath.Join(root, "aacpanel"), 0)
	if _, err := s.UpdateProject(ctx, good.ID, ProjectEdit{Path: &foreign}); err == nil {
		t.Fatal("a foreign path got through an edit — the check stands only on creation")
	}
	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree[0].Groups[0].Projects[0].Path; got != filepath.Join(root, "aacpanel") {
		t.Errorf("after the refusal the path became %q — the refusal wrote something", got)
	}
}
