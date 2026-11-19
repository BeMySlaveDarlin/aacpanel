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
	for _, values := range []sessionintent.Values{{"intent": "Queue"}, {"restartIntent": ""}, {"quick": "Go on\nStatus"},
		{"quick": strings.Repeat(strings.Repeat("🙂", 200)+"\n", 5)}, {"quick": ""}} {
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
		`{"intent":"bell\u0007"}`, `{"intent":"` + strings.Repeat("é", 501) + `"}`,
		`{"quick":null}`, `{"quick":"` + strings.Repeat(`go\n`, 6) + `"}`, `{"quick":"` + strings.Repeat("🙂", 201) + `"}`} {
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
	project, err := s.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{Name: &name, Path: &dir,
		Launch: json.RawMessage(`{"restartIntent":"Summary"}`)})
	if err != nil {
		t.Fatal(err)
	}
	s.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[{"session":"aacpanel-2","cwd":"`+dir+`"}]}`))
	client, fake := startFakeExec(t, action.Response{OK: true, Intents: sessionintent.Values{"restartIntent": "Continue", "quick": "Session phrase"}})
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
	if got.Own["restartIntent"] != "Continue" || len(got.Inherited) != 3 {
		t.Fatalf("the sheet got %+v", got)
	}
	for _, v := range got.Inherited {
		want, layer := "Summary", schema.LayerProject
		if v.Key == "intent" {
			want, layer = "Contour start", schema.LayerContour
		}
		if v.Key == "quick" {
			want, layer = sessionintent.DefaultQuick, schema.LayerPanel
		}
		if v.Value != want || v.Layer != layer {
			t.Errorf("inherited %+v, want %q from %s", v, want, layer)
		}
	}
	if r := <-fake.got; r.Ask != action.AskIntent || r.Target != "aacpanel-2" {
		t.Errorf("the host was asked %+v", r)
	}
	for _, c := range []struct{ contour, project, want, layer string }{
		{" Go on \n\n Status ", "", "Go on\nStatus", schema.LayerContour},
		{"Go on\nStatus", " Plan \n\n Continue ", "Plan\nContinue", schema.LayerProject},
		{"Go on\nStatus", " \n\t ", "Go on\nStatus", schema.LayerContour},
		{" \n ", " \n ", sessionintent.DefaultQuick, schema.LayerPanel},
	} {
		if _, err := s.db.UpdateProfile(t.Context(), p.ID, store.ProfileEdit{LaunchSet: map[string]any{"quick": c.contour}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.UpdateProject(t.Context(), project.ID, store.ProjectEdit{LaunchSet: map[string]any{"quick": c.project}}); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.apiSessionIntent(w, httptest.NewRequest(http.MethodGet, "/api/session/intent?name=aacpanel-2", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("quick phrases: %d %s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Own["quick"] != "Session phrase" {
			t.Fatalf("the sheet lost its quick override: %+v", got)
		}
		found := false
		for _, v := range got.Inherited {
			if v.Key == "quick" {
				found = true
				if v.Value != c.want || v.Layer != c.layer {
					t.Errorf("quick phrases: %+v; want %q from %s", v, c.want, c.layer)
				}
			}
		}
		if !found {
			t.Fatal("the sheet lost its inherited quick phrases")
		}
		<-fake.got
	}
}

func TestSessionMessageSheetWithoutAMapHasNoInheritedMessage(t *testing.T) {
	client, _ := startFakeExec(t, action.Response{OK: true})
	s := &Server{exec: client, host: host.NewReader(snapshotWith(t,
		`{"at":1,"sessions":[{"session":"lead","cwd":"/srv/notes"}]}`))}
	w := httptest.NewRecorder()
	s.apiSessionIntent(w, httptest.NewRequest(http.MethodGet, "/api/session/intent?name=lead", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"value":null`) ||
		!strings.Contains(w.Body.String(), `"key":"quick","value":"AskUserQuestion\nGo on\nTidy up the checklist\nStatus\nThat's all for today","layer":"panel"`) {
		t.Fatalf("sheet with no map: %d %s", w.Code, w.Body.String())
	}
}
