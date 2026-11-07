package check

import (
	"regexp"
	"strings"
	"testing"
)

type heavySeen struct {
	Text     string   `json:"text"`
	Running  []string `json:"running"`
	Waiting  []string `json:"waiting"`
	Head     string   `json:"head"`
	Controls int      `json:"controls"`
}

func checkHeavyRuns(t *testing.T, full, empty heavySeen) {
	t.Helper()
	if !strings.Contains(full.Text, "Heavy runs") || !strings.Contains(full.Text, "2 of 3 slots occupied") {
		t.Errorf("the machine loses the queue's slots: %+v", full)
	}
	if len(full.Running) != 2 || len(full.Waiting) != 2 {
		t.Fatalf("the machine loses the runs: %+v", full)
	}
	for i, wants := range [][]string{
		{"full check", "lab", "proj/service", "asked 2.0 GB", "holds 512 MB", "running"},
		{"aacp-heavy-2.scope", "session unknown", "asked no ceiling", "holds 0 B", "time unknown"},
	} {
		for _, want := range wants {
			if !strings.Contains(full.Running[i], want) {
				t.Errorf("running row %q omits %q", full.Running[i], want)
			}
		}
	}
	for i, wants := range [][]string{
		{"1. build", "other", "proj/client", "asked 1.0 GB", "waiting"},
		{"2. terminal check", "srv/tools", "asked 256 MB", "waiting"},
	} {
		for _, want := range wants {
			if !strings.Contains(full.Waiting[i], want) {
				t.Errorf("waiting row %q omits %q", full.Waiting[i], want)
			}
		}
	}
	for _, row := range []string{full.Running[0], full.Waiting[0], full.Waiting[1]} {
		if !regexp.MustCompile(`(?:running|waiting) \d+ min`).MatchString(row) {
			t.Errorf("a run has no elapsed time: %q", row)
		}
	}
	if full.Head != "First run waits for memory pressure (full avg10 12, below 10 needed)" {
		t.Errorf("the head's reason is lost: %q", full.Head)
	}
	if full.Controls != 0 || empty.Controls != 0 {
		t.Error("the queue has controls")
	}
	if !strings.Contains(empty.Text, "0 of 3 slots occupied · no heavy runs") || len(empty.Running) != 0 || len(empty.Waiting) != 0 || empty.Head != "" {
		t.Errorf("an empty queue keeps a run or its head: %+v", empty)
	}
}
