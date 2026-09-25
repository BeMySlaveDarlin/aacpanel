package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/host"
)

const procsSnapshot = `{"at":100,"host":{"cpuPct":3},"procs":{"at":95,"total":835,"items":[
	{"pid":1201,"cmd":"claude -n panel","user":"u","cpuPct":42.5,"rss":459276288,"memPct":0.7},
	{"pid":774,"cmd":"[kworker/3:1]","user":"root","cpuPct":0,"rss":0,"memPct":0}
]}}`

func procsSrv(t *testing.T, body string) *Server {
	t.Helper()
	return &Server{hostName: "STAND", host: host.NewReader(snapshotWith(t, body))}
}

func TestProcsReportKeepsEveryFieldAgentSends(t *testing.T) {
	got := procsReport([]byte(procsSnapshot))

	if got.State != procsOK || got.At != 95 || got.Total != 835 {
		t.Fatalf("the header of the block was lost: %+v", got)
	}
	if len(got.Items) != 2 {
		t.Fatalf("there must be two rows: %+v", got.Items)
	}
	want := procRow{PID: 1201, Cmd: "claude -n panel", User: "u", CPUPct: 42.5, RSS: 459276288, MemPct: 0.7}
	if got.Items[0] != want {
		t.Errorf("the row did not arrive whole:\n got %+v\n expected %+v", got.Items[0], want)
	}
}

func TestProcsWithoutBlockAreUnknown(t *testing.T) {
	for _, body := range []string{
		`{"at":100,"host":{"cpuPct":3}}`,
		`{"at":100,"procs":null}`,
		`not json at all`,
	} {
		got := procsReport([]byte(body))
		if got.State != procsUnknown || len(got.Items) != 0 {
			t.Errorf("the snapshot %q has to read as unknown: %+v", body, got)
		}
	}
}

func TestProcsDropRowsNothingCanBeSaidAbout(t *testing.T) {
	got := procsReport([]byte(`{"procs":{"at":1,"total":9,"items":[
		{"pid":0,"cmd":"from nowhere","cpuPct":99},
		{"pid":5,"cmd":"","cpuPct":98},
		{"pid":7,"cmd":"a live row","cpuPct":1.5}
	]}}`))
	if got.State != procsOK {
		t.Fatalf("the block parsed, and the state is not ok: %+v", got)
	}
	if len(got.Items) != 1 || got.Items[0].PID != 7 {
		t.Errorf("one row has to be left, the seventh: %+v", got.Items)
	}
	if got.Total != 9 {
		t.Errorf("the total number of processes was replaced by the length of the list: %+v", got)
	}
}

func TestProcsRideTheirOwnRouteNotTheSnapshot(t *testing.T) {
	mux := procsSrv(t, procsSnapshot).routes(gate{
		page:   func(h http.HandlerFunc) http.Handler { return h },
		stream: func(h http.HandlerFunc) http.Handler { return h },
	})

	snapshot := httptest.NewRecorder()
	mux.ServeHTTP(snapshot, httptest.NewRequest(http.MethodGet, "/api/host", nil))
	if snapshot.Code != http.StatusOK {
		t.Fatalf("the host snapshot was not served: %d", snapshot.Code)
	}
	if strings.Contains(snapshot.Body.String(), "claude -n panel") {
		t.Error("the processes went into /api/host: every subscriber reads the snapshot, and this block is for the panel alone")
	}

	procs := httptest.NewRecorder()
	mux.ServeHTTP(procs, httptest.NewRequest(http.MethodGet, "/api/procs", nil))
	if procs.Code != http.StatusOK {
		t.Fatalf("the processes handler did not answer: %d", procs.Code)
	}
	var got procsReply
	if err := json.NewDecoder(procs.Body).Decode(&got); err != nil {
		t.Fatalf("the answer of the handler does not parse: %v", err)
	}
	if got.State != procsOK || len(got.Items) != 2 || got.Items[0].Cmd != "claude -n panel" {
		t.Errorf("the handler served something other than what the agent collected: %+v", got)
	}
}

func TestProcsSayWhenAgentIsSilent(t *testing.T) {
	srv := &Server{hostName: "STAND", host: host.NewReader(t.TempDir() + "/no-such-file.json")}
	rec := httptest.NewRecorder()
	srv.apiProcs(rec, httptest.NewRequest(http.MethodGet, "/api/procs", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a silent agent has to be a refusal of the source, not an empty list: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "aacpanel-agent") {
		t.Errorf("there is nothing for a person to read in the refusal: %q", rec.Body.String())
	}
}

// A process in a container carries the name of its container: the container
// stands in the same list, and two names would read as two consumers.
func TestAProcessOfAContainerIsNamedByItsContainer(t *testing.T) {
	const id = "4426af4f45ae09720a1e50f220d19db5b71cfe897480d860f0bc761d7f4be69d"
	got := procsReport([]byte(`{"procs":{"at":1,"total":3,"items":[
		{"pid":6345,"cmd":"python3 -m homeassistant","cpuPct":2,"container":"` + id + `"},
		{"pid":7001,"cmd":"sleep 9","cpuPct":1,"container":"` + strings.Repeat("f", 64) + `"},
		{"pid":1201,"cmd":"claude -n panel","cpuPct":1}
	]}}`))
	if !anyContainer(got.Items) {
		t.Fatalf("the container of a process was lost on the way: %+v", got.Items)
	}
	nameContainers(got.Items, map[string]string{id: "homeassistant"})
	if got.Items[0].Container != "homeassistant" {
		t.Errorf("the process of homeassistant is named %q", got.Items[0].Container)
	}
	if got.Items[1].Container != strings.Repeat("f", 12) {
		t.Errorf("a container the listing does not know is named %q: the short form of its id", got.Items[1].Container)
	}
	if got.Items[2].Container != "" {
		t.Errorf("a process of the host was given a container: %q", got.Items[2].Container)
	}
}
