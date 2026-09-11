package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestProfileNameIsUniquePG(t *testing.T) {
	s, root := profileStore(t)
	mustProfile(t, s, "namesake", root, 0)

	_, err := s.CreateProfile(t.Context(), edit("namesake", root))
	if err == nil {
		t.Fatal("a second profile with the same name was created")
	}
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a taken name gave %v, ErrConflict was expected: HTTP will answer 500 instead of 409", err)
	}
}

func TestDeleteRefusesWhileProjectsInsidePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "with projects", root, 0)
	g := mustGroup(t, s, p.ID, "group", 0)
	pr := mustProject(t, s, g.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)

	if err := s.DeleteProfile(ctx, p.ID, false); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("a profile with a project was deleted, or refused with the wrong error: %v", err)
	}
	if err := s.DeleteGroup(ctx, g.ID, false); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("a group with a project was deleted, or refused with the wrong error: %v", err)
	}
	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || len(tree[0].Groups) != 1 || len(tree[0].Groups[0].Projects) != 1 {
		t.Fatalf("the map slipped after the refusal: %+v", tree)
	}

	if err := s.DeleteProject(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, g.ID, false); err != nil {
		t.Fatalf("the empty group was not deleted: %v", err)
	}
	if err := s.DeleteProfile(ctx, p.ID, false); err != nil {
		t.Fatalf("the empty profile was not deleted: %v", err)
	}
	if err := s.DeleteProfile(ctx, p.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a repeated deletion gave %v, ErrNotFound was expected: HTTP will answer 500 instead of 404", err)
	}
}

func TestDeleteProfileTakesEmptyGroupsPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "empty", root, 0)
	g := mustGroup(t, s, p.ID, "empty", 0)

	if err := s.DeleteProfile(ctx, p.ID, false); err != nil {
		t.Fatalf("a profile with an empty group was not deleted: %v", err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM profile_groups WHERE id = $1`, g.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Error("the group was left without a profile — the map would show it as nobody's")
	}
}

func TestDeleteProfileCascadeTakesOnlyItsOwnPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	doomed := mustProfile(t, s, "leaving", root, 0)
	first := mustGroup(t, s, doomed.ID, "first", 0)
	second := mustGroup(t, s, doomed.ID, "second", 1)
	mustProject(t, s, first.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)
	mustProject(t, s, second.ID, "gatekeeper", filepath.Join(root, "gatekeeper"), 0)

	spared := mustProfile(t, s, "staying", root, 1)
	kept := mustGroup(t, s, spared.ID, "own", 0)
	mustProject(t, s, kept.ID, "work", filepath.Join(root, "Labs"), 0)

	if err := s.DeleteProfile(ctx, doomed.ID, false); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("a contour with projects was deleted without consent, or refused with the wrong error: %v", err)
	}
	if err := s.DeleteProfile(ctx, doomed.ID, true); err != nil {
		t.Fatalf("the cascading deletion of the contour: %v", err)
	}

	tree, err := s.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].ID != spared.ID {
		t.Fatalf("the wrong contour was left on the map: %+v", tree)
	}
	if len(tree[0].Groups) != 1 || len(tree[0].Groups[0].Projects) != 1 {
		t.Fatalf("the cascade caught the neighbouring contour: %+v", tree[0].Groups)
	}

	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	var groups, projects int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM profile_groups),
		       (SELECT count(*) FROM profile_projects)`).Scan(&groups, &projects); err != nil {
		t.Fatal(err)
	}
	if groups != 1 || projects != 1 {
		t.Errorf("after the cascade the database holds %d groups and %d projects, one of each was expected", groups, projects)
	}
}

func TestProfileUpdateTouchesOnlyNamedPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	e := edit("contour", root)
	prefix := filepath.Join(root, "Labs")
	e.Prefix = &prefix
	e.Launch = json.RawMessage(`{"model":"opus","effort":"high"}`)
	p, err := s.CreateProfile(ctx, e)
	if err != nil {
		t.Fatal(err)
	}

	name := "renamed"
	got, err := s.UpdateProfile(ctx, p.ID, ProfileEdit{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name {
		t.Errorf("the name did not change: %q", got.Name)
	}
	if got.Prefix != prefix {
		t.Errorf("the prefix became %q, and nobody touched it", got.Prefix)
	}
	if !sameJSON(t, got.Launch, `{"model":"opus","effort":"high"}`) {
		t.Errorf("the launch parameters became %s, and nobody touched them", got.Launch)
	}

	if _, err := s.UpdateProfile(ctx, p.ID+1000, ProfileEdit{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Errorf("editing a profile that does not exist gave %v, ErrNotFound was expected", err)
	}
}
