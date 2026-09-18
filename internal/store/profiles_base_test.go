package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// The branch a review is measured against is a setting of the project, and an
// empty one is not a choice: it says nobody picked, and the screen offers its
// own default instead of repeating an answer that was never given.
func TestProjectBaseBranchIsKeptAndClearedPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, p.ID, "services", 0)

	fresh, err := s.CreateProject(ctx, g.ID, ProjectEdit{
		Name: strp("aacpanel"), Path: strp(filepath.Join(root, "aacpanel")), Sort: intp(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Base != "" {
		t.Errorf("a project nobody asked about came up with base %q — the map answered for the person", fresh.Base)
	}

	named, err := s.UpdateProject(ctx, fresh.ID, ProjectEdit{Base: strp("release/2026")})
	if err != nil {
		t.Fatal(err)
	}
	if named.Base != "release/2026" {
		t.Fatalf("the branch came back as %q", named.Base)
	}

	// A field left alone by an edit of something else keeps what it held.
	kept, err := s.UpdateProject(ctx, fresh.ID, ProjectEdit{Name: strp("panel")})
	if err != nil {
		t.Fatal(err)
	}
	if kept.Base != "release/2026" {
		t.Errorf("renaming the project dropped its base branch (%q)", kept.Base)
	}

	cleared, err := s.UpdateProject(ctx, fresh.ID, ProjectEdit{Base: strp("  ")})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Base != "" {
		t.Errorf("clearing the field left %q — there is then no way back to the default", cleared.Base)
	}

	back, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) == 0 || len(back[0].Groups) == 0 || len(back[0].Groups[0].Projects) == 0 {
		t.Fatal("the map came back without the project")
	}
	if got := back[0].Groups[0].Projects[0].Base; got != "" {
		t.Errorf("the map reads the base as %q where the project holds none", got)
	}
}

// git refuses these names itself, and a refusal that arrives from the database
// says nothing a person can act on. Each one is turned away with its reason.
func TestProjectBaseBranchRefusesWhatGitWouldPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, p.ID, "services", 0)
	project := mustProject(t, s, g.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)

	cases := []struct{ name, branch, says string }{
		{"a name that reads as a flag", "-delete", "flag"},
		{"a range of two names", "main..HEAD", "range"},
		{"a reflog address", "main@{yesterday}", "reflog"},
		{"the name of git's own lock file", "main.lock", ".lock"},
		{"a name with a space in it", "my branch", "does not take"},
		{"a name with a wildcard", "release/*", "does not take"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.UpdateProject(ctx, project.ID, ProjectEdit{Base: strp(c.branch)})
			if err == nil {
				t.Fatalf("%q was taken as a branch name — git will refuse it later, far from this screen", c.branch)
			}
			var bad ErrBadRequest
			if !errors.As(err, &bad) {
				t.Errorf("%q gave %v instead of a bad request", c.branch, err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("the refusal of %q says %q — it never mentions %q, so the person is left guessing",
					c.branch, err.Error(), c.says)
			}
		})
	}

	long := strings.Repeat("b", 201)
	if _, err := s.UpdateProject(ctx, project.ID, ProjectEdit{Base: strp(long)}); err == nil {
		t.Error("a branch name of 201 characters went into the map")
	}
}
