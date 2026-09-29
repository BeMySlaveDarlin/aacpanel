package check

import (
	"strings"
	"testing"
)

// The settings of the map on a wide screen are three columns: the tree, the
// group and a project picked on it, each page keeping its own draft; a pick in
// the tree over one with changes asks; Escape reaches only the top page; the
// model is picked from a popover at its row, not a sheet; a project dropped on
// a group of the tree moves there through its sheet, and the shelf keeps its
// order.
func TestTheDesktopSettingsAreThreeColumns(t *testing.T) {
	var got struct {
		Tree               []string       `json:"tree"`
		Opened             []string       `json:"opened"`
		WithProject        []string       `json:"withProject"`
		Ask                string         `json:"ask"`
		AfterStay          []string       `json:"afterStay"`
		EscapeAsks         string         `json:"escapeAsks"`
		EscapeOpen         []string       `json:"escapeOpen"`
		AfterDiscard       []string       `json:"afterDiscard"`
		Popover            bool           `json:"popover"`
		PopBar             string         `json:"popBar"`
		PopClosedPageStays bool           `json:"popClosedPageStays"`
		MoveSheet          string         `json:"moveSheet"`
		Move               map[string]any `json:"move"`
		OrderKept          bool           `json:"orderKept"`
		Close              *struct {
			Right  int  `json:"right"`
			Top    int  `json:"top"`
			InTree bool `json:"inTree"`
			Clear  bool `json:"clear"`
		} `json:"close"`
	}
	runFixtureServing(t, "deskcolumns.html", deskScreen, deskPointer, schemaAnswer(map[string]any{}), &got)

	if strings.Join(got.Tree, "|") != "personal2|home2|side0" {
		t.Errorf("the tree reads %v", got.Tree)
	}
	if strings.Join(got.Opened, "|") != "home|" || strings.Join(got.WithProject, "|") != "home|alpha" {
		t.Errorf("the columns read %v, then %v", got.Opened, got.WithProject)
	}
	if got.Ask != "1 change not saved" || strings.Join(got.AfterStay, "|") != "home|alpha" {
		t.Errorf("a pick over a draft: asked %q, after Stay %v", got.Ask, got.AfterStay)
	}
	if got.EscapeAsks != "1 change not saved" || strings.Join(got.EscapeOpen, "|") != "home|alpha" ||
		strings.Join(got.AfterDiscard, "|") != "home|" {
		t.Errorf("Escape: asked %q with %v open, after Discard %v — only the top page goes", got.EscapeAsks, got.EscapeOpen, got.AfterDiscard)
	}
	if !got.Popover || got.PopBar != "1 change" || !got.PopClosedPageStays {
		t.Errorf("the model popover: shown %v, its bar %q, the page stays after Escape %v", got.Popover, got.PopBar, got.PopClosedPageStays)
	}
	if c := got.Close; c == nil || c.InTree || c.Right > 16 || c.Top > 16 || !c.Clear {
		t.Errorf("the close of the settings is not in the corner of the window clear of the last heading: %+v", got.Close)
	}
	body, _ := got.Move["body"].(map[string]any)
	if !strings.Contains(got.MoveSheet, `moves to group "side"`) || got.Move["url"] != "/api/projects/12" || body["groupId"] != float64(5) || !got.OrderKept {
		t.Errorf("a drop on the tree: sheet %q, sent %v, order kept %v", got.MoveSheet, got.Move, got.OrderKept)
	}
}
