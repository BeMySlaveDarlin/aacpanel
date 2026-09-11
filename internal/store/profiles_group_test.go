package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupNameIsUniqueWithinProfilePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	a := mustProfile(t, s, "first", root, 0)
	b := mustProfile(t, s, "second", root, 10)
	mustGroup(t, s, a.ID, "Services", 0)

	_, err := s.CreateGroup(ctx, a.ID, GroupEdit{Name: strp("Services"), Sort: intp(0)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a second \"Infra\" in the same profile gave %v, ErrConflict was expected", err)
	}
	if !strings.Contains(err.Error(), "this profile already has a group with that name") {
		t.Errorf("the refusal's text %q does not name the reason to a person", err.Error())
	}

	g2 := mustGroup(t, s, a.ID, "Personal", 0)
	if _, err := s.UpdateGroup(ctx, g2.ID, GroupEdit{Name: strp("Services")}); !errors.Is(err, ErrConflict) {
		t.Fatalf("renaming to a taken name gave %v, ErrConflict was expected", err)
	}

	if _, err := s.CreateGroup(ctx, b.ID, GroupEdit{Name: strp("Services"), Sort: intp(0)}); err != nil {
		t.Fatalf("\"Infra\" in another profile was refused: %v", err)
	}
}

func TestDeleteGroupCascadeTakesOnlyItsProjectsPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "contour", root, 0)
	doomed := mustGroup(t, s, p.ID, "leaving", 0)
	spared := mustGroup(t, s, p.ID, "staying", 1)
	mustProject(t, s, doomed.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)
	mustProject(t, s, doomed.ID, "gatekeeper", filepath.Join(root, "gatekeeper"), 1)
	mustProject(t, s, spared.ID, "work", filepath.Join(root, "Labs"), 0)

	if err := s.DeleteGroup(ctx, doomed.ID, false); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("a group with projects was deleted without consent, or refused with the wrong error: %v", err)
	}
	if err := s.DeleteGroup(ctx, doomed.ID, true); err != nil {
		t.Fatalf("the cascading deletion of the group: %v", err)
	}

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || len(tree[0].Groups) != 1 {
		t.Fatalf("after the cascade the map holds more than one group: %+v", tree)
	}
	left := tree[0].Groups[0]
	if left.ID != spared.ID {
		t.Fatalf("the wrong group went: %d was left, %d was expected", left.ID, spared.ID)
	}
	if len(left.Projects) != 1 || left.Projects[0].Name != "work" {
		t.Errorf("the cascade caught the neighbouring group: %+v", left.Projects)
	}
}

func TestGroupMovesBetweenProfilesPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	own := mustProfile(t, s, "personal", root, 0)
	other := mustProfile(t, s, "work", filepath.Join(root, "Labs"), 1)
	mustGroup(t, s, other.ID, "First", 0)

	group := mustGroup(t, s, own.ID, "VENDOR", 0)
	project := mustProject(t, s, group.ID, "acme", filepath.Join(root, "acme"), 0)

	if _, err := s.UpdateGroup(ctx, group.ID, GroupEdit{ProfileID: &other.ID}); err == nil {
		t.Fatal("the group moved into somebody else's contour without consent — together with all of its projects")
	} else {
		var bad ErrBadRequest
		if !errors.As(err, &bad) {
			t.Errorf("the refusal came as %v, ErrBadRequest was expected", err)
		}
	}

	moved, err := s.UpdateGroup(ctx, group.ID, GroupEdit{ProfileID: &other.ID, MoveProfile: true})
	if err != nil {
		t.Fatalf("moving the group with consent: %v", err)
	}
	if moved.ProfileID != other.ID {
		t.Errorf("the group stayed in profile %d, %d was expected", moved.ProfileID, other.ID)
	}
	if moved.Sort != 1 {
		t.Errorf("the moved group took place %d, the end of the list (1) was expected", moved.Sort)
	}
	if moved.Name != "VENDOR" {
		t.Errorf("the move caught the name: %q", moved.Name)
	}

	tree := mustTree(t, s)
	if got := projectGroup(tree, project.ID); got != group.ID {
		t.Errorf("the project came loose from the group: it lies in %d", got)
	}
	for _, p := range tree {
		if p.ID != other.ID {
			continue
		}
		found := false
		for _, g := range p.Groups {
			if g.ID == group.ID && len(g.Projects) == 1 {
				found = true
			}
		}
		if !found {
			t.Errorf("the target profile holds no group with its project: %+v", p.Groups)
		}
	}
}

func TestGroupMoveRefusesTakenNamePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	own := mustProfile(t, s, "personal", root, 0)
	other := mustProfile(t, s, "work", filepath.Join(root, "Labs"), 1)
	mustGroup(t, s, other.ID, "Backend", 0)
	group := mustGroup(t, s, own.ID, "Backend", 0)

	_, err := s.UpdateGroup(ctx, group.ID, GroupEdit{ProfileID: &other.ID, MoveProfile: true})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("a move under a taken name gave %v, ErrConflict was expected — "+
			"two \"Backend\" groups in one contour cannot be told apart on the map", err)
	}
}

func TestGroupMoveToMissingContourIsNotFoundPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	own := mustProfile(t, s, "personal", root, 0)
	group := mustGroup(t, s, own.ID, "Services", 0)
	gone := own.ID + 1000

	for _, tc := range []struct {
		what   string
		agreed bool
	}{
		{"with consent", true},
		{"without consent", false},
	} {
		_, err := s.UpdateGroup(ctx, group.ID, GroupEdit{ProfileID: &gone, MoveProfile: tc.agreed})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: a move into a contour that does not exist gave %v, ErrNotFound was expected", tc.what, err)
		}
		if err != nil && strings.Contains(err.Error(), "moveProfile") {
			t.Errorf("%s: the person is asked to consent to a move into a contour that does not exist: %v", tc.what, err)
		}
	}

	tree := mustTree(t, s)
	if len(tree) != 1 || len(tree[0].Groups) != 1 {
		t.Fatalf("the map after the refusals: %+v", tree)
	}
	if g := tree[0].Groups[0]; g.ID != group.ID || g.ProfileID != own.ID || g.Name != "Services" {
		t.Errorf("the group after the refusal: %+v, its own in contour %d was expected", g, own.ID)
	}
}
