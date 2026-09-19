package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"
	"aacpanel/web"
)

// The version the panel serves has to be the version of the worker it hands
// out, or the screen that compares the two is comparing two different things.
// The worker carries it minified into a name of one letter, so the file beside
// it is what anyone asking can read.
func TestTheServedVersionIsTheVersionOfTheWorker(t *testing.T) {
	raw, err := web.FS.ReadFile("dist/" + webbuild.VersionFile)
	if err != nil {
		t.Skipf("the frontend is not built into this binary: %v", err)
	}
	version := strings.TrimSpace(string(raw))
	if version == "" {
		t.Fatal("the build wrote an empty version")
	}

	worker, err := web.FS.ReadFile("dist/sw.js")
	if err != nil {
		t.Fatalf("the worker: %v", err)
	}
	if !strings.Contains(string(worker), version) {
		t.Errorf("the worker does not carry the version %q the panel serves: a device would be told "+
			"it is behind — or up to date — by comparing two unrelated numbers", version)
	}

	rec := httptest.NewRecorder()
	versionFile().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /version answered %d", rec.Code)
	}
	if store := rec.Header().Get("Cache-Control"); store != "no-store" {
		t.Errorf("GET /version is served with Cache-Control %q: a cached answer tells a stuck device "+
			"its own version back, which is the one thing it must not do", store)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the answer does not parse: %v: %s", err, rec.Body.String())
	}
	if body.Version != version {
		t.Errorf("GET /version says %q against the built %q", body.Version, version)
	}
}
