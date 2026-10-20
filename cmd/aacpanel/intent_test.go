package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
	"aacpanel/internal/schema"
	"aacpanel/internal/sessionintent"
	"aacpanel/internal/store"
)

func TestSessionMessagesReachTheExecutorIndependently(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true})
	s := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	for _, values := range []sessionintent.Values{{"intent": "Queue"}, {"restartIntent": ""}} {
		raw, _ := json.Marshal(map[string]any{"kind": "session.intent", "target": "lead", "params": values})
		w := post(t, s, string(raw))
		if w.Code != http.StatusOK {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
		r := <-fake.got
		for k, v := range values {
			if r.Kind != action.SessionIntent || r.Target != "lead" || len(r.Intents) != 1 || r.Intents[k] != v {
				t.Fatalf("the executor got %+v", r)
			}
		}
	}
	for _, params := range []string{`{}`, `{"intent":null}`, `{"restartIntent":1}`, `{"model":"opus"}`,
		`{"intent":"bell\u0007"}`, `{"intent":"` + strings.Repeat("é", 501) + `"}`} {
		w := post(t, s, `{"kind":"session.intent","target":"lead","params":`+params+`}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("accepted %s: %d", params, w.Code)
		}
	}
}

func TestSessionMessageSheetShowsOwnAndInheritedLayersPG(t *testing.T) {
	s, root := profilesServer(t)
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.UseProjectRoots([]string{root}); err != nil {
		t.Fatal(err)
	}
	name, group, dir := "Work", "Services", filepath.Join(root, "aacpanel")
	p, err := s.db.CreateProfile(t.Context(), store.ProfileEdit{Name: &name, ConfigDir: &root,
		Launch: json.RawMessage(`{"intent":"Contour start","restartIntent":"Contour restart"}`)})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.db.CreateGroup(t.Context(), p.ID, store.GroupEdit{Name: &group})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{Name: &name, Path: &dir,
		Launch: json.RawMessage(`{"restartIntent":"Summary"}`)}); err != nil {
		t.Fatal(err)
	}
	s.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[{"session":"aacpanel-2","cwd":"`+dir+`"}]}`))
	client, fake := startFakeExec(t, action.Response{OK: true, Intents: sessionintent.Values{"restartIntent": "Continue"}})
	s.exec = client
	w := httptest.NewRecorder()
	s.apiSessionIntent(w, httptest.NewRequest(http.MethodGet, "/api/session/intent?name=aacpanel-2", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sheet: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Own       sessionintent.Values `json:"own"`
		Inherited []schema.Value       `json:"inherited"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Own["restartIntent"] != "Continue" || len(got.Inherited) != 2 {
		t.Fatalf("the sheet got %+v", got)
	}
	for _, v := range got.Inherited {
		want, layer := "Summary", schema.LayerProject
		if v.Key == "intent" {
			want, layer = "Contour start", schema.LayerContour
		}
		if v.Value != want || v.Layer != layer {
			t.Errorf("inherited %+v, want %q from %s", v, want, layer)
		}
	}
	if r := <-fake.got; r.Ask != action.AskIntent || r.Target != "aacpanel-2" {
		t.Errorf("the host was asked %+v", r)
	}
}

func TestSessionMessageSheetWithoutAMapHasNoInheritedMessage(t *testing.T) {
	client, _ := startFakeExec(t, action.Response{OK: true})
	s := &Server{exec: client, host: host.NewReader(snapshotWith(t,
		`{"at":1,"sessions":[{"session":"lead","cwd":"/srv/notes"}]}`))}
	w := httptest.NewRecorder()
	s.apiSessionIntent(w, httptest.NewRequest(http.MethodGet, "/api/session/intent?name=lead", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"value":null`) {
		t.Fatalf("sheet with no map: %d %s", w.Code, w.Body.String())
	}
}
