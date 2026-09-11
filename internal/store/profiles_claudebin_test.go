package store

import (
	"strings"
	"testing"
)

func TestProfileKeepsClaudeBinPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	bin := "/srv/tools/claude-contours/bin/claude"
	e := edit("contour-with-a-wrapper", root)
	e.ClaudeBin = &bin
	p, err := s.CreateProfile(ctx, e)
	if err != nil {
		t.Fatalf("the profile was not created: %v", err)
	}
	if p.ClaudeBin != bin {
		t.Errorf("the creation reply carries %q instead of %q", p.ClaudeBin, bin)
	}

	list, err := s.Profiles(ctx)
	if err != nil {
		t.Fatalf("the map was not read: %v", err)
	}
	var found bool
	for _, it := range list {
		if it.ID != p.ID {
			continue
		}
		found = true
		if it.ClaudeBin != bin {
			t.Errorf("on the map the contour has %q instead of %q — the launch will go through the wrong wrapper", it.ClaudeBin, bin)
		}
	}
	if !found {
		t.Fatal("the contour that was created is not on the map")
	}

	empty := ""
	off, err := s.UpdateProfile(ctx, p.ID, ProfileEdit{ClaudeBin: &empty})
	if err != nil {
		t.Fatalf("the path was not removed: %v", err)
	}
	if off.ClaudeBin != "" {
		t.Errorf("after the removal the path is still %q", off.ClaudeBin)
	}

	if _, err := s.UpdateProfile(ctx, p.ID, ProfileEdit{ClaudeBin: &bin}); err != nil {
		t.Fatal(err)
	}
	name := "contour-renamed"
	renamed, err := s.UpdateProfile(ctx, p.ID, ProfileEdit{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ClaudeBin != bin {
		t.Errorf("the rename erased the launch path: %q", renamed.ClaudeBin)
	}
}

func TestProfileRefusesRelativeClaudeBinPG(t *testing.T) {
	s, root := profileStore(t)

	e := edit("contour-with-a-malformed-path", root)
	bin := "claude"
	e.ClaudeBin = &bin
	if _, err := s.CreateProfile(t.Context(), e); err == nil {
		t.Fatal("a relative path was accepted — the session will go wherever PATH points")
	} else if !strings.Contains(err.Error(), "is not absolute") {
		t.Errorf("the refusal does not explain the reason: %v", err)
	}
}
