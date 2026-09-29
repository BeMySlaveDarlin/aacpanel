package check

import (
	"net/http"
	"strings"
	"testing"
)

// The page of a group: each project with what it sets otherwise than the
// contour, the shelf summed up, the directories next to it on disk — claude's
// first; another contour held where the router picks the account by
// directory, with the way back; projects dragged and the name changed, saved
// without a sheet; a value set for all through its sheet; a directory added
// and one hidden; the projects moved onto another shelf; a project's page
// laid over the shelf and gone back from.
func TestTheGroupPageIsAShelf(t *testing.T) {
	var got struct {
		Rows        []string       `json:"rows"`
		Summary     string         `json:"summary"`
		Nearby      []string       `json:"nearby"`
		Danger      string         `json:"danger"`
		Blocked     string         `json:"blocked"`
		BackClears  bool           `json:"backClears"`
		Order       []string       `json:"order"`
		Saved       []string       `json:"saved"`
		SheetOnSave bool           `json:"sheetOnSave"`
		SetSheet    string         `json:"setSheet"`
		Set         map[string]any `json:"set"`
		AddSheet    string         `json:"addSheet"`
		Add         map[string]any `json:"add"`
		Hide        map[string]any `json:"hide"`
		MoveSheet   string         `json:"moveSheet"`
		Move        map[string]any `json:"move"`
		ProjectOver string         `json:"projectOver"`
		BackToShelf bool           `json:"backToShelf"`
		Closed      int            `json:"closed"`
	}
	runFixtureServing(t, "groupsettings.html", phoneScreen, phonePointer, schemaAnswer(map[string]any{}), &got)

	if strings.Join(got.Rows, " ") != "alpha:stream,RC beta: gamma:high" {
		t.Errorf("the rows read %v", got.Rows)
	}
	if got.Summary != "set otherwise than the contour: stream 1 of 3 · RC 1 of 3 · high 1 of 3" {
		t.Errorf("the shelf is summed up as %q", got.Summary)
	}
	if strings.Join(got.Nearby, " ") != "/srv/proj/delta /srv/proj/eps" {
		t.Errorf("found next to the shelf: %v — meant the two in its folder, claude's first", got.Nearby)
	}
	if !strings.Contains(got.Danger, "only an empty shelf is deleted — move its 3 projects to:") || !strings.HasSuffix(got.Danger, "pets") {
		t.Errorf("a full shelf's deletion reads %q", got.Danger)
	}
	if got.Blocked != "the directory decides the account here: these 3 projects in /srv/proj stay personal" || !got.BackClears {
		t.Errorf("another contour on a routed machine reads %q, the way back clears it: %v", got.Blocked, got.BackClears)
	}
	want := []string{`PATCH /api/groups/3 {"name":"house"}`, `PUT /api/groups/3/projects/order {"ids":[13,11,12]}`}
	if strings.Join(got.Order, " ") != "gamma alpha beta" || strings.Join(got.Saved, "|") != strings.Join(want, "|") || got.SheetOnSave {
		t.Errorf("after the drag %v Save sent %v (sheet %v), meant %v", got.Order, got.Saved, got.SheetOnSave, want)
	}
	if !strings.Contains(got.SetSheet, "Set Effort Max for all projects") || got.Set["url"] != "/api/groups/3/set" {
		t.Errorf("set for all: sheet %q, sent %v", got.SetSheet, got.Set)
	}
	if body, _ := got.Set["body"].(map[string]any); body["key"] != "effort" || body["value"] != "max" {
		t.Errorf("set for all sent %v", got.Set)
	}
	if body, _ := got.Add["body"].(map[string]any); got.Add["url"] != "/api/groups/3/projects" || body["path"] != "/srv/proj/delta" {
		t.Errorf("adding a directory next to the shelf sent %v", got.Add)
	}
	if body, _ := got.Hide["body"].(map[string]any); got.Hide["url"] != "/api/disk/hidden" || body["path"] != "/srv/proj/eps" {
		t.Errorf("hiding a directory sent %v", got.Hide)
	}
	if body, _ := got.Move["body"].(map[string]any); got.Move["url"] != "/api/groups/3/move" || body["to"] != float64(5) ||
		!strings.Contains(got.MoveSheet, `onto group "pets"`) {
		t.Errorf("moving the shelf: sheet %q, sent %v", got.MoveSheet, got.Move)
	}
	if got.ProjectOver != "alpha" || !got.BackToShelf || got.Closed != 0 {
		t.Errorf("a project's page over the shelf read %q, back to the shelf %v, closed %d", got.ProjectOver, got.BackToShelf, got.Closed)
	}
}

// Where the router does not pick the account by directory, another contour is
// a move the page says the price of and sends through its sheet with consent.
func TestAGroupMovesToAnotherContourWhereNothingRoutes(t *testing.T) {
	var got struct {
		Said  string         `json:"said"`
		Bar   string         `json:"bar"`
		Sheet string         `json:"sheet"`
		Sent  map[string]any `json:"sent"`
	}
	serve := schemaAnswer(map[string]any{})
	serve["/mode"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("free")) })
	runFixtureServing(t, "groupsettings.html", phoneScreen, phonePointer, serve, &got)
	if got.Said != "3 projects start under work: its token, limits, archive. 1 of them have Remote Control on — work has no bridge" || got.Bar != "1 change" {
		t.Errorf("another contour reads %q, the bar %q", got.Said, got.Bar)
	}
	body, _ := got.Sent["body"].(map[string]any)
	if !strings.Contains(got.Sheet, `moves to profile "work"`) || body["profileId"] != float64(9) || body["moveProfile"] != true {
		t.Errorf("the move's sheet %q sent %v", got.Sheet, got.Sent)
	}
}
