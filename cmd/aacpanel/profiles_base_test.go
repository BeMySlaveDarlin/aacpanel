package main

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The base branch is set on the same form as everything else about a project,
// and it travels the same way: the form hands the gate its fields, the gate
// sends them whole. A field that only exists on the screen is a setting nobody
// can save.
func TestProjectBaseBranchTravelsFromTheFormToTheMapPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)

	profile := idOf(t, profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`), "profile")
	group := idOf(t, profilePost(t, mux, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"Services"}`), "group")

	created := profilePost(t, mux, "/api/groups/"+strconv.Itoa(group)+"/projects",
		`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`","base":"release/2026"}`)
	project := idOf(t, created, "project")
	if got := created["project"].(map[string]any)["base"]; got != "release/2026" {
		t.Fatalf("the project came back with base %v — the branch was dropped on the way in", got)
	}

	path := "/api/projects/" + strconv.Itoa(project)

	t.Run("the map carries it to the screens", func(t *testing.T) {
		body := profilePatch(t, mux, path, `{"name":"panel"}`)
		if got := body["project"].(map[string]any)["base"]; got != "release/2026" {
			t.Errorf("renaming the project left base %v — an edit of one field cleared another", got)
		}
	})

	t.Run("an empty value is the way back to the default", func(t *testing.T) {
		body := profilePatch(t, mux, path, `{"base":""}`)
		if got := body["project"].(map[string]any)["base"]; got != "" {
			t.Errorf("clearing the branch left %v", got)
		}
	})

	t.Run("a name git refuses is refused here, with the reason", func(t *testing.T) {
		code, body := profileCode(t, mux, http.MethodPatch, path, `{"base":"main..HEAD"}`)
		if code != http.StatusBadRequest {
			t.Fatalf("a range of two names gave %d, expected 400", code)
		}
		if !strings.Contains(strings.ToLower(body), "range") {
			t.Errorf("the refusal reads %q — it never says what is wrong with the name", body)
		}
	})
}
