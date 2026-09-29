package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/notify"
)

func pushCall(t *testing.T, h http.HandlerFunc, method, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(method, "/api/push/x", strings.NewReader(body)))
	out := map[string]any{}
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %v", w.Body.String(), err)
		}
	}
	return w.Code, out
}

// The choice of pushes is stored cleaned, read back with what it can name,
// and the button of a push turns its source off in the same choice — which
// the watch then reads before every push.
func TestTheChoiceOfPushesIsKeptAndTheButtonOfAPushQuietsItsSourcePG(t *testing.T) {
	srv, _ := profilesServer(t)
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM push_prefs"); err != nil {
		t.Fatal(err)
	}
	if p := srv.pushPrefs(t.Context()); len(p.Off)+len(p.Stacks)+len(p.Sessions)+len(p.Rules)+len(p.Limits) != 0 {
		t.Fatalf("a panel nobody tuned holds back %+v", p)
	}

	code, got := pushCall(t, srv.apiPushPrefsSave, http.MethodPut,
		`{"off":["done","call","done","nonsense"],"stacks":["shop"," "],"rules":[14,-1]}`)
	if code != http.StatusOK {
		t.Fatalf("saving gave %d", code)
	}
	raw, _ := json.Marshal(got["prefs"])
	var saved notify.Prefs
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	want := notify.Prefs{Off: []string{"done"}, Rules: []int64{14}, Sessions: []string{}, Stacks: []string{"shop"}, Limits: []string{}}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("the choice was stored as %+v, want %+v", saved, want)
	}

	code, got = pushCall(t, srv.apiPushPrefs, http.MethodGet, "")
	if code != http.StatusOK {
		t.Fatalf("reading gave %d", code)
	}
	src, _ := got["sources"].(map[string]any)
	rules, _ := src["rules"].([]any)
	if len(rules) == 0 {
		t.Errorf("the choice names no rules: %v", src)
	}
	for _, key := range []string{"contours", "stacks", "rules", "probes"} {
		if _, ok := src[key].([]any); !ok {
			t.Errorf("the sources have no list %q: %v", key, src)
		}
	}

	code, _ = pushCall(t, srv.apiPushQuiet, http.MethodPost, `{"what":"sessions","key":"/srv/proj/Acme/infra","label":"Quiet: infra"}`)
	if code != http.StatusOK {
		t.Fatalf("the button gave %d", code)
	}
	p := srv.pushPrefs(t.Context())
	if len(p.Sessions) != 1 || p.Sessions[0] != "/srv/proj/Acme/infra" || len(p.Stacks) != 1 {
		t.Errorf("after the button the choice is %+v", p)
	}
	turn := notify.Message{Title: "Turn finished · infra", Body: "b", Tag: "done:x:1", Kind: notify.KindDone,
		Source: notify.Source{Place: "/srv/proj/Acme/infra"}}
	if p.Allows(turn) {
		t.Error("the source a button quieted still pushes")
	}

	if code, _ := pushCall(t, srv.apiPushQuiet, http.MethodPost, `{"what":"off","key":"call"}`); code != http.StatusBadRequest {
		t.Errorf("a button that turns off the calls gave %d", code)
	}
}
