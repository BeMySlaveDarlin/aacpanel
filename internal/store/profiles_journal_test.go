package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func changeOf(e JournalEntry, key string) (Change, bool) {
	for _, c := range e.Changes {
		if c.Key == key {
			return c, true
		}
	}
	return Change{}, false
}

// Every change of the map is written with who made it and, field by field,
// what it changed — a key of the launch by its own name — and a save that
// changed nothing writes nothing.
func TestTheMapKeepsAJournalPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := WithActor(t.Context(), "pixel")

	c, err := s.CreateProfile(ctx, edit("contour", root))
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGroup(ctx, c.ID, GroupEdit{Name: strp("services")})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, g.ID, ProjectEdit{Name: strp("aacpanel"), Path: strp(filepath.Join(root, "aacpanel")),
		Launch: json.RawMessage(`{"effort":"high"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject(ctx, p.ID, ProjectEdit{LaunchSet: map[string]any{"effort": "max", "remoteControl": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject(ctx, p.ID, ProjectEdit{Sort: intp(3)}); err != nil {
		t.Fatal(err)
	}

	list, err := s.Journal(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("%d entries for a contour, a group, a project and one real change: %+v", len(list), list)
	}
	last := list[0]
	if last.Entity != "project" || last.Op != OpUpdate || last.Actor != "pixel" || last.Name != "aacpanel" {
		t.Errorf("the latest entry is %+v", last)
	}
	if ch, ok := changeOf(last, "launch.effort"); !ok || ch.From != "high" || ch.To != "max" {
		t.Errorf("the effort's change reads %+v", last.Changes)
	}
	if ch, ok := changeOf(last, "launch.remoteControl"); !ok || ch.From != nil || ch.To != true {
		t.Errorf("remote control switched on reads %+v", last.Changes)
	}
	if created := list[1]; created.Op != OpCreate {
		t.Errorf("the project's creation reads %+v", created)
	} else if ch, ok := changeOf(created, "path"); !ok || ch.To != filepath.Join(root, "aacpanel") {
		t.Errorf("the creation does not say where: %+v", created.Changes)
	}
}

// A deleted project comes back from the journal as it was — the same
// identifier, group, place and launch — once; not where its directory went
// to another project since, and not once its group is gone.
func TestADeletedProjectComesBackFromTheJournalPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	c := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, c.ID, "services", 0)
	path := filepath.Join(root, "aacpanel")
	p, err := s.CreateProject(ctx, g.ID, ProjectEdit{Name: strp("aacpanel"), Path: &path, Session: strp("panel"),
		Launch: json.RawMessage(`{"transport":"stream","remoteControl":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.DeleteProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := s.Journal(ctx, 1)
	if len(list) != 1 || list[0].ID != entry || !list[0].Undoable {
		t.Fatalf("the deletion is not offered back: %+v", list)
	}

	back, err := s.UndoDelete(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != p.ID || back.GroupID != g.ID || back.Path != path || back.Session != "panel" ||
		!sameJSON(t, back.Launch, `{"transport":"stream","remoteControl":true}`) {
		t.Errorf("the project came back as %+v", back)
	}
	if _, err := s.UndoDelete(ctx, entry); !errors.Is(err, ErrNotUndoable) {
		t.Errorf("a second undo gave %v", err)
	}
	list, _ = s.Journal(ctx, 2)
	if list[0].Undoes == nil || *list[0].Undoes != entry || list[1].Undoable || list[1].UndoneAt == nil {
		t.Errorf("the journal does not say the deletion was taken back: %+v", list)
	}

	entry, _ = s.DeleteProject(ctx, p.ID)
	mustProject(t, s, g.ID, "squatter", path, 0)
	if _, err := s.UndoDelete(ctx, entry); !errors.Is(err, ErrConflict) {
		t.Errorf("an undo onto a taken directory gave %v", err)
	}

	other := mustProject(t, s, g.ID, "other", filepath.Join(root, "other"), 1)
	gone, _ := s.DeleteProject(ctx, other.ID)
	squatter := mustTree(t, s)[0].Groups[0].Projects[0]
	if _, err := s.DeleteProject(ctx, squatter.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, g.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UndoDelete(ctx, gone); !errors.Is(err, ErrNotUndoable) {
		t.Errorf("an undo into a deleted group gave %v", err)
	}
}

// Projects a cascade takes are journalled one by one, each with what brings
// it back.
func TestACascadeJournalsEveryProjectPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()
	c := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, c.ID, "services", 0)
	mustProject(t, s, g.ID, "a", filepath.Join(root, "a"), 0)
	mustProject(t, s, g.ID, "b", filepath.Join(root, "b"), 1)
	if err := s.DeleteGroup(ctx, g.ID, true); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Journal(ctx, 3)
	names := map[string]bool{}
	for _, e := range list {
		if e.Entity == "project" && e.Op == OpDelete {
			names[e.Name] = true
		}
	}
	if !names["a"] || !names["b"] || list[0].Entity != "group" {
		t.Errorf("the cascade is journalled as %+v", list)
	}
}
