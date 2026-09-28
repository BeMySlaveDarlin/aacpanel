package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// What a model takes comes with the schema, as the host's sessions on the
// stream learned it; a host that has not learned it says so by an empty map,
// not by null.
func TestProfilesSchemaCarriesWhatModelsTake(t *testing.T) {
	var got struct {
		Traits map[string]struct {
			Efforts  []string `json:"efforts"`
			AutoMode bool     `json:"autoMode"`
		} `json:"traits"`
	}
	for _, snap := range []string{`{"at":1}`, `{"at":1,"models":{"traits":{"claude-sonnet-4-6":{"effort":true,"efforts":["low","medium","high"],"autoMode":false}}}}`} {
		srv := &Server{host: host.NewReader(snapshotWith(t, snap))}
		w := httptest.NewRecorder()
		srv.apiProfilesSchema(w, httptest.NewRequest(http.MethodGet, "/api/profiles/schema", nil))
		if !strings.Contains(w.Body.String(), `"traits":{`) {
			t.Errorf("the schema carries no map of traits: %s", w.Body.String())
		}
		got.Traits = nil
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
	}
	if s := got.Traits["claude-sonnet-4-6"]; len(s.Efforts) != 3 || s.AutoMode {
		t.Errorf("sonnet 4.6 reads %+v", s)
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

// Every project answers the command its next launch runs, each word marked
// with the layer its parameter came from.
func TestProfilesAnswerTheLaunchLinePG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	call := func(method, path, body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	profile := idOf(t, call(http.MethodPost, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`","launch":{"remoteControl":true}}`), "profile")
	group := idOf(t, call(http.MethodPost, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"s"}`), "group")
	body := call(http.MethodPost, "/api/groups/"+strconv.Itoa(group)+"/projects",
		`{"name":"aacpanel","path":"`+root+`/aacpanel","launch":{"effort":"high"}}`)
	line, _ := body["project"].(map[string]any)["line"].(map[string]any)
	words, _ := line["words"].([]any)
	got := []string{}
	for _, raw := range words {
		w := raw.(map[string]any)
		layer, _ := w["layer"].(string)
		got = append(got, w["text"].(string)+"/"+layer)
	}
	want := "claude/ --mcp-config/panel <the panel's tools>/panel --allowedTools/panel mcp__aacpanel__plan/panel " +
		"-n/ aacpanel/ --remote-control/contour aacpanel/contour --effort/project high/project"
	if strings.Join(got, " ") != want {
		t.Errorf("the line reads %q, meant %q", strings.Join(got, " "), want)
	}
}

// A draft of a project's edit is answered with what the launch would be if it
// were saved — the command by the launcher's own code, the effective values
// and what the launch would refuse — and nothing of it is written.
func TestProjectPreviewAnswersTheDraftAndWritesNothingPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	call := func(method, path, body string, want int) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	text := func(body map[string]any) string {
		line, _ := body["line"].(map[string]any)
		words, _ := line["words"].([]any)
		got := []string{}
		for _, raw := range words {
			w := raw.(map[string]any)
			layer, _ := w["layer"].(string)
			got = append(got, w["text"].(string)+"/"+layer)
		}
		return strings.Join(got, " ")
	}
	profile := idOf(t, call(http.MethodPost, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`","launch":{"remoteControl":true}}`, http.StatusOK), "profile")
	group := idOf(t, call(http.MethodPost, "/api/profiles/"+strconv.Itoa(profile)+"/groups", `{"name":"s"}`, http.StatusOK), "group")
	project := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/groups/"+strconv.Itoa(group)+"/projects",
		`{"name":"aacpanel","path":"`+root+`/aacpanel","launch":{"effort":"high"}}`, http.StatusOK), "project"))
	preview := "/api/projects/" + project + "/preview"

	got := call(http.MethodPost, preview, `{"session":"panel","launchUnset":["effort"],"launchSet":{"model":"sonnet"}}`, http.StatusOK)
	if want := "claude/ --mcp-config/panel <the panel's tools>/panel --allowedTools/panel mcp__aacpanel__plan/panel " +
		"-n/ panel/ --remote-control/contour panel/contour --model/project sonnet/project"; text(got) != want {
		t.Errorf("the draft's line reads %q, meant %q", text(got), want)
	}
	if problems, _ := got["problems"].([]any); problems == nil || len(problems) != 0 {
		t.Errorf("a draft the launch takes answers problems %v, meant an empty list", got["problems"])
	}
	layers := map[string]string{}
	for _, raw := range got["effective"].([]any) {
		v := raw.(map[string]any)
		layers[v["key"].(string)] = v["layer"].(string)
	}
	if layers["model"] != "project" || layers["effort"] != "claude" || layers["remoteControl"] != "contour" {
		t.Errorf("the draft's effective layers are %v", layers)
	}

	bad := call(http.MethodPost, preview, `{"launchSet":{"effort":"ultra","env":{"CLAUDE_CONFIG_DIR":"/x"}}}`, http.StatusOK)
	keys := []string{}
	for _, raw := range bad["problems"].([]any) {
		keys = append(keys, raw.(map[string]any)["key"].(string))
	}
	if strings.Join(keys, " ") != "effort env" {
		t.Errorf("a draft the launch would refuse names %v, meant effort and env", keys)
	}

	call(http.MethodPost, preview, `{"launchSet":{"effort":"low"},"launchUnset":["effort"]}`, http.StatusBadRequest)
	call(http.MethodPost, "/api/projects/999999/preview", `{}`, http.StatusNotFound)

	tree := call(http.MethodGet, "/api/profiles", "", http.StatusOK)
	stored := treeOf(t, tree)[0].(map[string]any)["groups"].([]any)[0].(map[string]any)["projects"].([]any)[0].(map[string]any)
	if launch, _ := json.Marshal(stored["launch"]); string(launch) != `{"effort":"high"}` || stored["session"] != "" {
		t.Errorf("the preview wrote the draft: launch %s, session %q", launch, stored["session"])
	}
}

// Raised to the contour, a value is taken out of the projects that store the
// same one — each change journaled — and left in the ones that store another;
// a contour answers a draft of its own launch with what its projects would
// start with, writing nothing.
func TestContourUnpinsCopiesAndPreviewsItsDraftPG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	call := func(method, path, body string, want int) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	contour := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles",
		`{"name":"personal","configDir":"`+root+`","launch":{"remoteControl":true}}`, http.StatusOK), "profile"))
	group := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles/"+contour+"/groups", `{"name":"s"}`, http.StatusOK), "group"))
	for i, launch := range []string{`{"remoteControl":true,"effort":"high"}`, `{"remoteControl":false}`, `{}`} {
		dir := filepath.Join(root, "p"+strconv.Itoa(i))
		call(http.MethodPost, "/api/groups/"+group+"/projects",
			`{"name":"p`+strconv.Itoa(i)+`","path":"`+dir+`","launch":`+launch+`}`, http.StatusOK)
	}

	preview := call(http.MethodPost, "/api/profiles/"+contour+"/preview",
		`{"launchSet":{"transport":"stream","effort":"ultra"}}`, http.StatusOK)
	layers := map[string]string{}
	for _, raw := range preview["effective"].([]any) {
		v := raw.(map[string]any)
		layers[v["key"].(string)] = v["layer"].(string)
	}
	if layers["transport"] != "contour" || layers["remoteControl"] != "contour" {
		t.Errorf("the contour's draft lays %v", layers)
	}
	if problems, _ := preview["problems"].([]any); len(problems) != 1 || problems[0].(map[string]any)["key"] != "effort" {
		t.Errorf("the contour's draft names problems %v, meant the effort", preview["problems"])
	}

	body := call(http.MethodPost, "/api/profiles/"+contour+"/unpin", `{"key":"remoteControl"}`, http.StatusOK)
	if body["unpinned"] != float64(1) {
		t.Errorf("unpinned %v projects, meant the one storing the contour's value", body["unpinned"])
	}
	launches := []string{}
	for _, raw := range treeOf(t, body)[0].(map[string]any)["groups"].([]any)[0].(map[string]any)["projects"].([]any) {
		l, _ := json.Marshal(raw.(map[string]any)["launch"])
		launches = append(launches, string(l))
	}
	if strings.Join(launches, " ") != `{"effort":"high"} {"remoteControl":false} {}` {
		t.Errorf("after unpinning the projects store %v", launches)
	}
	if transport := preview["effective"]; transport == nil {
		t.Fatal("no effective")
	}
	tree := call(http.MethodGet, "/api/profiles", "", http.StatusOK)
	if launch, _ := json.Marshal(treeOf(t, tree)[0].(map[string]any)["launch"]); string(launch) != `{"remoteControl":true}` {
		t.Errorf("the preview wrote the contour's draft: %s", launch)
	}
	journal := call(http.MethodGet, "/api/profiles/journal", "", http.StatusOK)["journal"].([]any)
	last := journal[0].(map[string]any)
	change, _ := json.Marshal(last["changes"])
	if last["name"] != "p0" || last["op"] != "update" || !strings.Contains(string(change), `"launch.remoteControl"`) {
		t.Errorf("the unpin is journaled as %v %v %s", last["name"], last["op"], change)
	}

	call(http.MethodPost, "/api/profiles/"+contour+"/unpin", `{"key":"effort"}`, http.StatusBadRequest)
	call(http.MethodPost, "/api/profiles/"+contour+"/unpin", `{"key":"nonsense"}`, http.StatusBadRequest)
}

// What is said for a whole group is written into each of its projects — a
// project already storing it left alone, each change journaled — and taken out
// of all of them the same way; a group's projects move onto another group of
// the contour after the ones there, and not onto a foreign contour's.
func TestAShelfSetsForAllAndMovesWholePG(t *testing.T) {
	srv, root := profilesServer(t)
	mux := profilesMux(srv)
	call := func(method, path, body string, want int) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	contour := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`, http.StatusOK), "profile"))
	other := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles", `{"name":"work","configDir":"`+root+`/w"}`, http.StatusOK), "profile"))
	shelf := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles/"+contour+"/groups", `{"name":"a"}`, http.StatusOK), "group"))
	next := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles/"+contour+"/groups", `{"name":"b"}`, http.StatusOK), "group"))
	foreign := strconv.Itoa(idOf(t, call(http.MethodPost, "/api/profiles/"+other+"/groups", `{"name":"c"}`, http.StatusOK), "group"))
	call(http.MethodPost, "/api/groups/"+next+"/projects", `{"name":"z","path":"`+root+`/z"}`, http.StatusOK)
	for i, launch := range []string{`{"effort":"high"}`, `{}`, `{"effort":"low"}`} {
		n := strconv.Itoa(i)
		call(http.MethodPost, "/api/groups/"+shelf+"/projects", `{"name":"p`+n+`","path":"`+root+`/p`+n+`","launch":`+launch+`}`, http.StatusOK)
	}
	launches := func(body map[string]any, group string) []string {
		out := []string{}
		for _, g := range treeOf(t, body)[0].(map[string]any)["groups"].([]any) {
			gm := g.(map[string]any)
			if strconv.Itoa(int(gm["id"].(float64))) != group {
				continue
			}
			for _, raw := range gm["projects"].([]any) {
				p := raw.(map[string]any)
				l, _ := json.Marshal(p["launch"])
				out = append(out, p["name"].(string)+string(l))
			}
		}
		return out
	}

	body := call(http.MethodPost, "/api/groups/"+shelf+"/set", `{"key":"effort","value":"high"}`, http.StatusOK)
	if body["changed"] != float64(2) || strings.Join(launches(body, shelf), " ") != `p0{"effort":"high"} p1{"effort":"high"} p2{"effort":"high"}` {
		t.Errorf("set for all changed %v: %v", body["changed"], launches(body, shelf))
	}
	body = call(http.MethodPost, "/api/groups/"+shelf+"/set", `{"key":"effort","value":null}`, http.StatusOK)
	if body["changed"] != float64(3) || strings.Join(launches(body, shelf), " ") != `p0{} p1{} p2{}` {
		t.Errorf("clear for all changed %v: %v", body["changed"], launches(body, shelf))
	}
	call(http.MethodPost, "/api/groups/"+shelf+"/set", `{"key":"effort","value":"ultra"}`, http.StatusBadRequest)
	call(http.MethodPost, "/api/groups/"+shelf+"/set", `{"key":"room","value":"x"}`, http.StatusBadRequest)
	journal := call(http.MethodGet, "/api/profiles/journal", "", http.StatusOK)["journal"].([]any)
	if len(journal) < 5 || journal[0].(map[string]any)["op"] != "update" {
		t.Errorf("the shelf's changes are not journaled a project each: %d entries", len(journal))
	}

	ids := []string{}
	for _, g := range treeOf(t, body)[0].(map[string]any)["groups"].([]any) {
		if gm := g.(map[string]any); strconv.Itoa(int(gm["id"].(float64))) == shelf {
			for _, raw := range gm["projects"].([]any) {
				ids = append(ids, strconv.Itoa(int(raw.(map[string]any)["id"].(float64))))
			}
		}
	}
	call(http.MethodPut, "/api/groups/"+shelf+"/projects/order", `{"ids":[`+ids[2]+`,`+ids[0]+`,`+ids[1]+`]}`, http.StatusOK)
	call(http.MethodPost, "/api/groups/"+shelf+"/move", `{"to":`+foreign+`}`, http.StatusBadRequest)
	call(http.MethodPost, "/api/groups/"+shelf+"/move", `{"to":`+shelf+`}`, http.StatusBadRequest)
	body = call(http.MethodPost, "/api/groups/"+shelf+"/move", `{"to":`+next+`}`, http.StatusOK)
	if body["moved"] != float64(3) || len(launches(body, shelf)) != 0 ||
		strings.Join(launches(body, next), " ") != `z{} p2{} p0{} p1{}` {
		t.Errorf("the move left %v and made %v", launches(body, shelf), launches(body, next))
	}
	call(http.MethodDelete, "/api/groups/"+shelf, "", http.StatusOK)
}
