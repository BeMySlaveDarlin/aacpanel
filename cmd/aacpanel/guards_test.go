package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/store"
)

// Every project's directory gets what it starts with, and every contour's
// directories its defaults: a session no project lies closer to is still
// capped by its contour. Where nobody says anything the panel's own cap
// holds and nothing restarts.
func TestGuardsOfTheMap(t *testing.T) {
	list := []store.Profile{
		{Prefix: "/srv/proj/Algo", Launch: json.RawMessage(`{"contextCap":70,"autoRestart":true}`),
			Groups: []store.ProfileGroup{{Projects: []store.ProfileProject{
				{Path: "/srv/proj/Algo/lms", Launch: json.RawMessage(`{"autoRestart":false}`)},
				{Path: "/srv/proj/Algo/ai-platform"},
			}}}},
		{Prefix: "", Launch: json.RawMessage(`{}`),
			Groups: []store.ProfileGroup{{Projects: []store.ProfileProject{
				{Path: "/srv/proj/Pets/service/aacpanel", Launch: json.RawMessage(`{"contextCap":90}`)},
			}}}},
	}
	got := map[string]action.Guard{}
	for _, g := range guardsOf(list) {
		got[g.Path] = g
	}
	want := map[string]action.Guard{
		"/srv/proj/Algo":                  {Path: "/srv/proj/Algo", Cap: 70, Restart: true},
		"/srv/proj/Algo/lms":              {Path: "/srv/proj/Algo/lms", Cap: 70, Restart: false},
		"/srv/proj/Algo/ai-platform":      {Path: "/srv/proj/Algo/ai-platform", Cap: 70, Restart: true},
		"/srv/proj/Pets/service/aacpanel": {Path: "/srv/proj/Pets/service/aacpanel", Cap: 90, Restart: false},
	}
	if len(got) != len(want) {
		t.Errorf("guards %v, meant %v", got, want)
	}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s is guarded as %+v, meant %+v", path, got[path], w)
		}
	}
}

// A change of the map reaches the host without waiting for the tick: the
// save pokes the publisher, and what it hands on is the map after the save.
func TestAChangeOfTheMapReachesTheHostPG(t *testing.T) {
	srv, root := profilesServer(t)
	client, fake := startFakeExec(t, action.Response{OK: true})
	srv.exec = client
	srv.guards = make(chan struct{}, 1)
	mux := profilesMux(srv)
	call := func(method, path, body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: status %d, %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	profile := idOf(t, call(http.MethodPost, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`), "profile")
	group := idOf(t, call(http.MethodPost, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"services"}`), "group")
	project := idOf(t, call(http.MethodPost, "/api/groups/"+strconv.Itoa(group)+"/projects",
		`{"name":"aacpanel","path":"`+root+`/aacpanel"}`), "project")
	<-srv.guards
	call(http.MethodGet, "/api/profiles", "")
	select {
	case <-srv.guards:
		t.Error("a look at the map poked the publisher")
	default:
	}

	call(http.MethodPatch, "/api/projects/"+strconv.Itoa(project), `{"launchSet":{"autoRestart":true,"contextCap":70}}`)
	select {
	case <-srv.guards:
	default:
		t.Fatal("a save of the map did not poke the publisher")
	}
	if failed := srv.handGuards(t.Context(), ""); failed != "" {
		t.Fatalf("the guards were not handed: %s", failed)
	}
	var req action.Request
	for req = range fake.got {
		if req.Ask == action.AskGuards {
			break
		}
	}
	want := action.Guard{Path: root + "/aacpanel", Cap: 70, Restart: true}
	if len(req.Guards) != 1 || req.Guards[0] != want {
		t.Errorf("the host was handed %+v, meant %+v", req.Guards, want)
	}
}
