package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/host"
)

// The schema is answered without a database: it is built into the service,
// and a screen drawn from it must not go blank because the map is down.
func TestProfilesSchemaNeedsNoDatabase(t *testing.T) {
	srv := &Server{hostName: "STAND-01"}
	w := httptest.NewRecorder()
	srv.apiProfilesSchema(w, httptest.NewRequest(http.MethodGet, "/api/profiles/schema", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var got struct {
		Params []struct {
			Key     string `json:"key"`
			Options []struct {
				Value string `json:"value"`
			} `json:"options"`
		} `json:"params"`
		Retired []struct {
			Key string `json:"key"`
		} `json:"retired"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, p := range got.Params {
		keys = append(keys, p.Key)
		if p.Key == "effort" && strings.Contains(w.Body.String(), `"value":"ultracode"`) {
			t.Error("the schema offers ultracode at launch")
		}
	}
	if !strings.Contains(strings.Join(keys, " "), "transport model effort") || len(got.Retired) == 0 {
		t.Errorf("the schema answers keys %v and retired %v", keys, got.Retired)
	}
}

// The map answers what every contour and project starts with, and from which
// layer; a project that turns off what its contour turns on says off, from
// itself. A change of one key through the API leaves the others as they were.
func TestProfilesAnswerEffectiveValuesPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	call := func(method, path, body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: status %d, %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	profile := idOf(t, call(http.MethodPost, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`","launch":{"remoteControl":true,"effort":"high"}}`), "profile")
	group := idOf(t, call(http.MethodPost, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"services"}`), "group")
	project := idOf(t, call(http.MethodPost, "/api/groups/"+strconv.Itoa(group)+"/projects",
		`{"name":"aacpanel","path":"`+root+`/aacpanel","launch":{"intent":"read the queue"}}`), "project")

	body := call(http.MethodPatch, "/api/projects/"+strconv.Itoa(project), `{"launchSet":{"remoteControl":false}}`)
	own := body["project"].(map[string]any)
	if launch, _ := json.Marshal(own["launch"]); !strings.Contains(string(launch), `"intent":"read the queue"`) {
		t.Errorf("a change of one key lost the others: %s", launch)
	}

	values := func(entry map[string]any) map[string]string {
		out := map[string]string{}
		list, _ := entry["effective"].([]any)
		for _, raw := range list {
			v := raw.(map[string]any)
			out[v["key"].(string)] = jsonText(v["value"]) + " · " + v["layer"].(string)
		}
		return out
	}
	got := values(own)
	for key, want := range map[string]string{
		"remoteControl": "false · project",
		"effort":        `"high" · contour`,
		"intent":        `"read the queue" · project`,
		"transport":     "null · claude",
	} {
		if got[key] != want {
			t.Errorf("the project's %s reads %q, meant %q", key, got[key], want)
		}
	}
	tree := treeOf(t, body)
	if got := values(tree[0].(map[string]any))["remoteControl"]; got != "true · contour" {
		t.Errorf("the contour's defaults read remote control %q", got)
	}
}

func jsonText(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// Where the map says nothing, the account's own settings are what a session
// starts with, and the map answers them from the account layer — the model a
// console runs on is not "claude decides" when the account names it.
func TestProfilesAnswerTheAccountLayerPG(t *testing.T) {
	srv, root := profilesServer(t)
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"profiles":[{"name":"personal","configDir":"`+root+
		`","auth":"builtin","account":{"model":"opus[1m]","effort":"xhigh"},"contextGuard":true}]}`))
	mux := profilesMux(srv)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/profiles",
		strings.NewReader(`{"name":"personal","configDir":"`+root+`","launch":{"effort":"high"}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, %s", w.Code, w.Body.String())
	}
	var body struct {
		Profile struct {
			ContextGuard *bool `json:"contextGuard"`
			Effective    []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
				Layer string `json:"layer"`
			} `json:"effective"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Profile.ContextGuard == nil || !*body.Profile.ContextGuard {
		t.Error("the contour does not say its account runs the context guard")
	}
	got := map[string]string{}
	for _, v := range body.Profile.Effective {
		got[v.Key] = jsonText(v.Value) + " · " + v.Layer
	}
	if got["model"] != `"opus[1m]" · account` || got["effort"] != `"high" · contour` {
		t.Errorf("the contour's defaults read model %q, effort %q", got["model"], got["effort"])
	}
}
