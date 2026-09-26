package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
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
