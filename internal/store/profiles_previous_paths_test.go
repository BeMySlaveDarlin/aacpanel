package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectRemembersPreviousPathsPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()
	c := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, c.ID, "services", 0)
	old, current := filepath.Join(root, "old"), filepath.Join(root, "current")
	p := mustProject(t, s, g.ID, "service", old, 0)
	check := func(edit ProjectEdit, want ...string) {
		t.Helper()
		want = append([]string{}, want...)
		var err error
		p, err = s.UpdateProject(ctx, p.ID, edit)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p.PreviousPaths, want) {
			t.Fatalf("previous paths = %v, want %v", p.PreviousPaths, want)
		}
		tree := mustTree(t, s)
		if got := tree[0].Groups[0].Projects[0].PreviousPaths; !reflect.DeepEqual(got, want) {
			t.Fatalf("the map returned previous paths %v, want %v", got, want)
		}
	}
	check(ProjectEdit{Path: &current}, old)
	entries, err := s.Journal(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if change, ok := changeOf(entries[0], "previousPaths"); !ok || !reflect.DeepEqual(change.To, []any{old}) {
		t.Fatalf("the move did not journal its former path: %+v", entries[0])
	}
	check(ProjectEdit{Path: &current}, old)
	check(ProjectEdit{Path: &old}, current)
	manual := []string{current, "/elsewhere/former/", "/elsewhere/former", old}
	check(ProjectEdit{PreviousPaths: &manual}, current, "/elsewhere/former")
	manual = []string{"/elsewhere/former"}
	check(ProjectEdit{PreviousPaths: &manual}, "/elsewhere/former")
	entry, err := s.DeleteProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	back, err := s.UndoDelete(ctx, entry)
	if err != nil || !reflect.DeepEqual(back.PreviousPaths, manual) {
		t.Fatalf("undo restored previous paths %v: %v", back.PreviousPaths, err)
	}
	manual = []string{}
	check(ProjectEdit{PreviousPaths: &manual})
	for _, invalid := range []string{"relative/path", "", "/bad\x00path", "/bad\npath"} {
		manual = []string{invalid}
		if _, err := s.UpdateProject(ctx, p.ID, ProjectEdit{PreviousPaths: &manual}); err == nil {
			t.Fatalf("invalid former path %q was accepted", invalid)
		}
	}
}
