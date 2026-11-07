package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskCLIUsesThePanelAndPreservesFilters(t *testing.T) {
	var requests []*http.Request
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[],"total":0}`))
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	for _, args := range [][]string{{"list", "-project", "11", "-contour", "3", "-status", "open,deferred", "-tag", "a b", "-q", "source", "-external", "LMS-42", "-deleted", "-limit", "5", "-offset", "2"}, {"show", "-project", "11", "-number", "4"}, {"import", "-project", "11"}} {
		var out, stderr bytes.Buffer
		if code := runTask(args, &out, &stderr); code != 0 || out.Len() == 0 {
			t.Fatalf("%v: code %d, %s", args, code, stderr.String())
		}
	}
	if len(requests) != 3 {
		t.Fatalf("requests: %v", requests)
	}
	q := requests[0].URL.Query()
	for key, want := range map[string]string{"project": "11", "contour": "3", "status": "open,deferred", "tag": "a b", "q": "source", "external": "LMS-42", "deleted": "true", "limit": "5", "offset": "2"} {
		if q.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, q.Get(key), want)
		}
	}
	if requests[1].URL.Path != "/api/projects/11/tasks/4" || requests[2].URL.Path != "/api/tasks/import" || requests[2].Method != http.MethodPost {
		t.Fatalf("wrong task routes: %v", requests)
	}
}

func TestTaskCLIRejectsInvalidArgumentsAndServiceFailures(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(panel.Close)
	t.Setenv("AACP_PANEL_URL", panel.URL)
	for _, args := range [][]string{nil, {"write"}, {"show"}, {"show", "-project", "1"}, {"list", "-offset", "-1"}, {"list", "extra"}, {"list"}} {
		var out, stderr bytes.Buffer
		if code := runTask(args, &out, &stderr); code == 0 || stderr.Len() == 0 {
			t.Fatalf("%v accepted or silent: %d", args, code)
		}
	}
}
