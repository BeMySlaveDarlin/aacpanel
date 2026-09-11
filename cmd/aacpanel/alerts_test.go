package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestAlertsProbesWithoutDB(t *testing.T) {
	srv := &Server{hostName: "STAND-01"}
	for name, h := range map[string]http.HandlerFunc{
		"/api/alerts": srv.apiAlerts,
		"/api/probes": srv.apiProbes,
	} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, name, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without a database gave %d, expected 503", name, w.Code)
		}
	}
}

func TestAlertsProbesPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := &Server{db: db, hostName: "STAND-01"}

	t.Run("alerts", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.apiAlerts(w, httptest.NewRequest(http.MethodGet, "/api/alerts?limit=5", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var body struct {
			Alerts []store.Alert `json:"alerts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Alerts == nil {
			t.Error("an empty list came as null, not as []")
		}
	})

	t.Run("probes", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.apiProbes(w, httptest.NewRequest(http.MethodGet, "/api/probes", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", w.Code, w.Body.String())
		}
		var body struct {
			Probes []store.ProbeState `json:"probes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Probes == nil {
			t.Error("an empty list came as null, not as []")
		}
	})
}
