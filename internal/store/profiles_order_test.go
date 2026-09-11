package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestReorderProfilesPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	a := mustProfile(t, s, "a", root, 0)
	b := mustProfile(t, s, "b", root, 0)
	c := mustProfile(t, s, "c", root, 0)

	if err := s.ReorderProfiles(ctx, []int{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("the reordering was refused: %v", err)
	}

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 3 || tree[0].ID != c.ID || tree[1].ID != a.ID || tree[2].ID != b.ID {
		t.Fatalf("the order after the reordering is %v, c,a,b was expected", profileIDs(tree))
	}
	if tree[0].Sort != 0 || tree[1].Sort != 1 || tree[2].Sort != 2 {
		t.Errorf("sort was not normalised: %d,%d,%d", tree[0].Sort, tree[1].Sort, tree[2].Sort)
	}
}

func TestReorderRejectsMismatchedSetPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	a := mustProfile(t, s, "a", root, 0)
	mustProfile(t, s, "b", root, 0)

	var bad ErrBadRequest
	if err := s.ReorderProfiles(ctx, []int{a.ID}); !errors.As(err, &bad) {
		t.Fatalf("an incomplete list gave a %T (%v), ErrBadRequest was expected", err, err)
	}
	if err := s.ReorderProfiles(ctx, []int{a.ID, a.ID + 1000}); !errors.As(err, &bad) {
		t.Fatalf("a foreign id gave a %T (%v), ErrBadRequest was expected", err, err)
	}
}

func TestCreateAppendsToEndAfterReorderPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	a := mustProfile(t, s, "a", root, 0)
	b := mustProfile(t, s, "b", root, 0)
	if err := s.ReorderProfiles(ctx, []int{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}

	c, err := s.CreateProfile(ctx, edit("c", root))
	if err != nil {
		t.Fatalf("profile c: %v", err)
	}

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 3 || tree[2].ID != c.ID {
		t.Fatalf("the new profile did not go to the end: %v", profileIDs(tree))
	}
}

func TestReorderGroupsAndProjectsPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "p", root, 0)
	g1 := mustGroup(t, s, p.ID, "g1", 0)
	g2 := mustGroup(t, s, p.ID, "g2", 0)
	if err := s.ReorderGroups(ctx, p.ID, []int{g2.ID, g1.ID}); err != nil {
		t.Fatalf("the reordering of the groups was refused: %v", err)
	}

	pr1 := mustProject(t, s, g1.ID, "pr1", filepath.Join(root, "aacpanel"), 0)
	pr2 := mustProject(t, s, g1.ID, "pr2", filepath.Join(root, "gatekeeper"), 0)
	if err := s.ReorderProjects(ctx, g1.ID, []int{pr2.ID, pr1.ID}); err != nil {
		t.Fatalf("the reordering of the projects was refused: %v", err)
	}

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tree[0].Groups[0].ID != g2.ID || tree[0].Groups[1].ID != g1.ID {
		t.Fatalf("the groups' order was not applied: %+v", tree[0].Groups)
	}
	projects := tree[0].Groups[1].Projects
	if len(projects) != 2 || projects[0].ID != pr2.ID || projects[1].ID != pr1.ID {
		t.Fatalf("the projects' order was not applied: %+v", projects)
	}

	other := mustProfile(t, s, "other", root, 10)
	og := mustGroup(t, s, other.ID, "og", 0)
	var bad ErrBadRequest
	if err := s.ReorderGroups(ctx, p.ID, []int{g2.ID, og.ID}); !errors.As(err, &bad) {
		t.Fatalf("a group belonging to another profile passed the check: %T (%v)", err, err)
	}
}

func profileIDs(tree []Profile) []int {
	out := make([]int, len(tree))
	for i, p := range tree {
		out[i] = p.ID
	}
	return out
}
