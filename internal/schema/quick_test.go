package schema

import (
	"strings"
	"testing"

	"aacpanel/internal/sessionintent"
)

func TestQuickPhrasesAreCheckedOnBothMapLevels(t *testing.T) {
	for _, level := range []Level{LevelContour, LevelProject} {
		for _, text := range []string{"", " \n\t ", sessionintent.DefaultQuick, " go \n\nstatus ", strings.Repeat(strings.Repeat("🙂", 200)+"\n", 5)} {
			if got := Check(level, map[string]any{"quick": text}); len(got) != 0 {
				t.Errorf("%s refuses %q: %v", level, text, got)
			}
		}
		for _, value := range []any{nil, true, []any{"go"}, strings.Repeat("go\n", 6), strings.Repeat("🙂", 201), "go\a"} {
			if got := Check(level, map[string]any{"quick": value}); len(got) != 1 || got[0].Key != "quick" {
				t.Errorf("%s accepts %v: %v", level, value, got)
			}
		}
	}
}

func TestQuickPhrasesInheritAndDefaultToThePanel(t *testing.T) {
	for _, c := range []struct {
		name, contour, project, want, layer string
	}{
		{"default", "", "", sessionintent.DefaultQuick, LayerPanel},
		{"contour", "Go on\nStatus", "", "Go on\nStatus", LayerContour},
		{"project", "Go on\nStatus", " AskUserQuestion \n\n Continue ", "AskUserQuestion\nContinue", LayerProject},
		{"blank project", "Go on\nStatus", " \n\t ", "Go on\nStatus", LayerContour},
		{"both blank", " \n ", "\n\t", sessionintent.DefaultQuick, LayerPanel},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := valueOf(t, Effective(nil, map[string]any{"quick": c.contour}, map[string]any{"quick": c.project}), "quick")
			if got.Value != c.want || got.Layer != c.layer {
				t.Fatalf("got %+v; want %q from %s", got, c.want, c.layer)
			}
		})
	}
	if got := valueOf(t, Effective(nil, nil, nil), "quick"); got.Value != sessionintent.DefaultQuick || got.Layer != LayerPanel {
		t.Fatalf("an absent quick list: %+v", got)
	}
	if got := Launch(map[string]any{"quick": " Go on\n\nStatus "}, nil); got["quick"] != "Go on\nStatus" {
		t.Fatalf("the launch carries %v", got)
	}
}
