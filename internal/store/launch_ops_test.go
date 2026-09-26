package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// A change of some keys leaves the rest as stored: a phone saving the effort
// a minute after the desktop saved the model keeps the model. A change the
// launch would not take is refused whole and changes nothing.
func TestLaunchChangesKeyByKeyPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	c := mustProfile(t, s, "contour", root, 0)
	g := mustGroup(t, s, c.ID, "services", 0)
	p := mustProject(t, s, g.ID, "aacpanel", filepath.Join(root, "aacpanel"), 0)
	if _, err := s.UpdateProject(ctx, p.ID, ProjectEdit{
		Launch: json.RawMessage(`{"model":"opus","remoteControl":true,"intent":"read the queue"}`),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.UpdateProject(ctx, p.ID, ProjectEdit{
		LaunchSet:   map[string]any{"effort": "high", "remoteControl": false},
		LaunchUnset: []string{"intent"},
	})
	if err != nil {
		t.Fatalf("a change of some keys was refused: %v", err)
	}
	if !sameJSON(t, got.Launch, `{"model":"opus","effort":"high","remoteControl":false}`) {
		t.Errorf("after the change the launch is %s", got.Launch)
	}

	for name, e := range map[string]ProjectEdit{
		"a value the launch would not take": {LaunchSet: map[string]any{"effort": "ultracode"}},
		"whole and key by key at once": {Launch: json.RawMessage(`{}`),
			LaunchSet: map[string]any{"effort": "low"}},
		"a key set and removed": {LaunchSet: map[string]any{"effort": "low"}, LaunchUnset: []string{"effort"}},
	} {
		if _, err := s.UpdateProject(ctx, p.ID, e); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if tree := mustTree(t, s); !sameJSON(t, tree[0].Groups[0].Projects[0].Launch,
		`{"model":"opus","effort":"high","remoteControl":false}`) {
		t.Errorf("a refused change left a trace: %s", tree[0].Groups[0].Projects[0].Launch)
	}

	if _, err := s.UpdateProject(ctx, p.ID+1000, ProjectEdit{LaunchSet: map[string]any{"effort": "low"}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a change of a project that is not there gave %v", err)
	}

	contour, err := s.UpdateProfile(ctx, c.ID, ProfileEdit{LaunchSet: map[string]any{"remoteControl": true}})
	if err != nil || !sameJSON(t, contour.Launch, `{"remoteControl":true}`) {
		t.Errorf("the contour's change gave %s, %v", contour.Launch, err)
	}
	if _, err := s.UpdateProfile(ctx, c.ID, ProfileEdit{LaunchSet: map[string]any{"room": "Work"}}); err == nil {
		t.Error("a retired key was stored on the contour")
	}
}
