package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectPathIsUniquePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	p := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, p.ID, "services", 0)
	path := filepath.Join(root, "aacpanel")
	mustProject(t, s, g.ID, "aacpanel", path, 0)

	other := mustGroup(t, s, p.ID, "pets", 1)
	_, err := s.CreateProject(ctx, other.ID, ProjectEdit{Name: strp("copy"), Path: strp(path), Sort: intp(0)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a repeated path gave %v, ErrConflict was expected", err)
	}
	want := `this directory is already in the map — profile "contour", group "services"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal's text %q does not say where the duplicate is hiding (%q was expected)", err.Error(), want)
	}

	if _, err := s.CreateProject(ctx, other.ID, ProjectEdit{
		Name: strp("aacpanel"), Path: strp(filepath.Join(root, "gatekeeper")), Sort: intp(0),
	}); err != nil {
		t.Fatalf("a second \"aacpanel\" label under a different path was refused: %v", err)
	}

	third := mustProject(t, s, other.ID, "third", filepath.Join(root, "Labs"), 1)
	if _, err := s.UpdateProject(ctx, third.ID, ProjectEdit{Path: strp(path)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changing the path to a taken one gave %v, ErrConflict was expected", err)
	}
}

func TestProjectMovesOnlyInsideItsProfilePG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	own := mustProfile(t, s, "personal", root, 0)
	from := mustGroup(t, s, own.ID, "Services", 0)
	to := mustGroup(t, s, own.ID, "Personal", 1)
	other := mustProfile(t, s, "work", filepath.Join(root, "Labs"), 1)
	foreign := mustGroup(t, s, other.ID, "CLIENT", 0)

	project := mustProject(t, s, from.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)
	mustProject(t, s, to.ID, "gatekeeper", filepath.Join(root, "gatekeeper"), 0)

	moved, err := s.UpdateProject(ctx, project.ID, ProjectEdit{GroupID: &to.ID})
	if err != nil {
		t.Fatalf("the move into the neighbouring group: %v", err)
	}
	if moved.GroupID != to.ID {
		t.Errorf("the project stayed in group %d, %d was expected", moved.GroupID, to.ID)
	}
	if moved.Sort != 1 {
		t.Errorf("the moved project took place %d, the end of the list (1) was expected — "+
			"otherwise it lands in the middle of somebody else's order", moved.Sort)
	}
	if moved.Name != "aacpanel" || moved.Path != filepath.Join(root, "aacpanel") {
		t.Errorf("the move caught the other fields: %+v", moved)
	}

	if _, err := s.UpdateProject(ctx, project.ID, ProjectEdit{GroupID: &foreign.ID}); err == nil {
		t.Error("the project moved into a group of somebody else's contour — that is a silent change of token and subscription")
	} else {
		var bad ErrBadRequest
		if !errors.As(err, &bad) {
			t.Errorf("somebody else's group gave %v, a refusal of ErrBadRequest was expected", err)
		}
	}

	after := mustTree(t, s)
	if got := projectGroup(after, project.ID); got != to.ID {
		t.Errorf("after the refusal the project lies in group %d, and it lay in %d", got, to.ID)
	}

	if _, err := s.UpdateProject(ctx, project.ID, ProjectEdit{GroupID: intp(to.ID + 1000)}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a move into a group that does not exist gave %v, ErrNotFound was expected", err)
	}

	renamed := "aacpanel-2"
	got, err := s.UpdateProject(ctx, project.ID, ProjectEdit{Name: &renamed})
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupID != to.ID {
		t.Errorf("editing the name carried the project off into group %d", got.GroupID)
	}
	if got.Sort != 1 {
		t.Errorf("editing the name moved the project to place %d", got.Sort)
	}
}

func projectGroup(tree []Profile, id int) int {
	for _, p := range tree {
		for _, g := range p.Groups {
			for _, r := range g.Projects {
				if r.ID == id {
					return g.ID
				}
			}
		}
	}
	return 0
}

func TestProjectNeverMovesToAnotherContourPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	own := mustProfile(t, s, "personal", root, 0)
	from := mustGroup(t, s, own.ID, "Services", 0)
	other := mustProfile(t, s, "work", filepath.Join(root, "Labs"), 1)
	foreign := mustGroup(t, s, other.ID, "Backend", 0)

	project := mustProject(t, s, from.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)
	mustProject(t, s, foreign.ID, "acme", filepath.Join(root, "acme"), 0)

	for _, tc := range []struct {
		what string
		edit ProjectEdit
	}{
		{"without consent", ProjectEdit{GroupID: &foreign.ID}},
		{"with moveProfile consent", ProjectEdit{GroupID: &foreign.ID, MoveProfile: true}},
		{"with moveContour consent", ProjectEdit{GroupID: &foreign.ID, MoveContour: true}},
	} {
		_, err := s.UpdateProject(ctx, project.ID, tc.edit)
		if err == nil {
			t.Fatalf("%s: the project moved into somebody else's contour — the sessions would go under somebody else's token, "+
				"while the transcripts would stay in the previous one", tc.what)
		}
		var bad ErrBadRequest
		if !errors.As(err, &bad) {
			t.Errorf("%s: the refusal came as %v, ErrBadRequest was expected — the group exists, "+
				"and a 404 would send the person hunting for a typo in the id", tc.what, err)
		}
		for _, consent := range []string{"moveProfile", "moveContour"} {
			if strings.Contains(err.Error(), consent) {
				t.Errorf("%s: the refusal offers a consent that no longer exists (%q): %v", tc.what, consent, err)
			}
		}
		if !strings.Contains(err.Error(), "belongs to another profile") {
			t.Errorf("%s: the refusal does not name the contour: %v", tc.what, err)
		}
	}

	if got := projectGroup(mustTree(t, s), project.ID); got != from.ID {
		t.Errorf("after the refusals the project lies in group %d, and it lay in %d", got, from.ID)
	}
}
