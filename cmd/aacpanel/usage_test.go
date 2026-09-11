package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/store"
)

func TestUsageFilterTakesExplicitBounds(t *testing.T) {
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	f, err := usageFilter(request(t, "from=%d&to=%d", from.Unix(), to.Unix()))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if !f.From.Equal(from) || !f.To.Equal(to) {
		t.Errorf("the period is %s–%s, expected exactly the bounds the client named", f.From, f.To)
	}
}

func TestUsageFilterAllMeansAll(t *testing.T) {
	f, err := usageFilter(request(t, "period=all"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if !f.From.Equal(usageAll) {
		t.Errorf("the period starts at %s, expected the start of the epoch", f.From)
	}
}

func TestUsageFilterRefusesAnEmptyPeriod(t *testing.T) {
	now := time.Now().Unix()
	if _, err := usageFilter(request(t, "from=%d&to=%d", now, now-10)); err == nil {
		t.Fatal("a period whose end is before its start has to be a refusal")
	}
}

func TestUsageFilterReadsContoursBothWays(t *testing.T) {
	f, err := usageFilter(request(t, "contour=home&contour=work,client"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(f.Contours) != 3 {
		t.Fatalf("the contours are %v, expected three", f.Contours)
	}
}

func TestUsageFilterKeepsOutsideApartFromProject(t *testing.T) {
	f, err := usageFilter(request(t, "outside=1"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if !f.Outside || f.Project != "" {
		t.Errorf("filter %+v: outside the map has to be a flag, not a project", f)
	}
}

func TestUsageScanSaysWhenThereIsNoAgent(t *testing.T) {
	srv := &Server{}

	w := httptest.NewRecorder()
	srv.apiUsageScan(w, httptest.NewRequest(http.MethodGet, "/api/usage/scan", nil))
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("the response: %v", err)
	}
	if body["available"] != false {
		t.Errorf("the response is %v, expected available: false", body)
	}
	if body["reason"] == nil {
		t.Error("the reason is not named: nothing to do it with, and no explanation, sends a person guessing")
	}

	w = httptest.NewRecorder()
	srv.apiUsageScanStart(w, httptest.NewRequest(http.MethodPost, "/api/usage/scan", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("starting a collection without an agent answered %d, expected 503", w.Code)
	}
}

func request(t *testing.T, query string, args ...any) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet,
		"/api/usage/summary?"+fmt.Sprintf(query, args...), nil)
}

func TestUsageSummaryBodyMarshalsToNumbers(t *testing.T) {
	sum := store.UsageSummary{
		UsageTotals: store.UsageTotals{Input: 10, CacheRead: 700, CacheCreation: 290, Inbound: 1000},
		Sub:         store.UsageTotals{Inbound: 440, Output: 570},
	}
	raw, err := json.Marshal(usageSummaryBody(sum, store.UsageFilter{From: time.Unix(1, 0), To: time.Unix(2, 0)}))
	if err != nil {
		t.Fatalf("the summary body did not assemble: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"inbound", "hit", "subShareIn", "subShareOut"} {
		if _, ok := body[key].(float64); !ok {
			t.Errorf("%s arrived as %T, expected a number", key, body[key])
		}
	}
	if body["inbound"] != float64(1000) {
		t.Errorf("the full inbound is %v, expected 1000", body["inbound"])
	}
	if body["subShareIn"] != 0.44 {
		t.Errorf("the subagent share of the inbound is %v, expected 0.44 — of the full inbound", body["subShareIn"])
	}
}
