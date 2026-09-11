package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/store"
)

func faultsOf(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	srv.apiFaults(w, httptest.NewRequest(http.MethodGet, "/api/faults", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/faults answered %d", w.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response does not parse: %v (%s)", err, w.Body.String())
	}
	return out
}

func TestFaultsWithoutDB(t *testing.T) {
	got := faultsOf(t, &Server{hostName: "STAND-01"})
	if got["recording"] != false {
		t.Errorf("without a database recording = %v, expected false", got["recording"])
	}
	if got["reason"] == "" || got["reason"] == nil {
		t.Error("a refusal without a reason: the panel has nothing to show")
	}
}

func TestFaultsShowSnapshotFaults(t *testing.T) {
	w := store.NewWriter(nil, "STAND-01")
	srv := &Server{hostName: "STAND-01", writer: w}

	if faults := faultsOf(t, srv)["faults"]; faults == nil {
		t.Fatal("faults arrived as null — an empty list and a forgotten field are indistinguishable")
	} else if len(faults.([]any)) != 0 {
		t.Fatalf("a clean collection already carries faults: %v", faults)
	}

	w.AgentSnapshot([]byte(`{"at": 1700000000,
		"host": {"cpuPct": 1, "mem": {"total": 2, "used": 1},
		         "disks": [{"mount": "/", "total": 10, "used": 3.5}]}}`))

	got := faultsOf(t, srv)
	if got["recording"] != true {
		t.Errorf("recording = %v, expected true", got["recording"])
	}
	faults, ok := got["faults"].([]any)
	if !ok || len(faults) != 1 {
		t.Fatalf("expected one fault, got: %v", got["faults"])
	}
	f := faults[0].(map[string]any)
	if f["block"] != "disks" {
		t.Errorf("the block that failed is %v, expected disks", f["block"])
	}
	for _, field := range []string{"error", "since", "last", "count"} {
		if f[field] == nil || f[field] == "" {
			t.Errorf("the fault has no %q field: %v", field, f)
		}
	}
}
