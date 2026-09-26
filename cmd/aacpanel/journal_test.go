package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A deletion answers the entry of the map's journal that takes it back; the
// entry puts the project back once and refuses the second time; the journal
// answers who did what, the newest first.
func TestTheJournalTakesBackADeletionPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	_, body := call(http.MethodPost, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)
	profile := idOf(t, body, "profile")
	_, body = call(http.MethodPost, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"services"}`)
	group := idOf(t, body, "group")
	_, body = call(http.MethodPost, "/api/groups/"+strconv.Itoa(group)+"/projects",
		`{"name":"aacpanel","path":"`+root+`/aacpanel","launch":{"remoteControl":true}}`)
	project := idOf(t, body, "project")

	code, body := call(http.MethodDelete, "/api/projects/"+strconv.Itoa(project), "")
	entry, ok := body["undo"].(float64)
	if code != http.StatusOK || !ok || entry <= 0 {
		t.Fatalf("the deletion answered %d without the entry that takes it back: %v", code, body)
	}
	undo := "/api/profiles/journal/" + strconv.Itoa(int(entry)) + "/undo"
	code, body = call(http.MethodPost, undo, "")
	if code != http.StatusOK || idOf(t, body, "project") != project {
		t.Fatalf("the undo answered %d: %v", code, body)
	}
	if code, _ = call(http.MethodPost, undo, ""); code != http.StatusConflict {
		t.Errorf("a second undo answered %d", code)
	}

	code, body = call(http.MethodGet, "/api/profiles/journal?limit=2", "")
	list, _ := body["journal"].([]any)
	if code != http.StatusOK || len(list) != 2 {
		t.Fatalf("the journal answered %d: %v", code, body)
	}
	newest := list[0].(map[string]any)
	if newest["op"] != "create" || newest["undoes"] != entry {
		t.Errorf("the newest entry is %v — the project put back by the undo", newest)
	}
}
