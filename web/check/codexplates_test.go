package check

import (
	"strings"
	"testing"
)

func TestThePhoneContourPageShowsCodexsWeekBesideClaudesLimits(t *testing.T) {
	type column struct {
		Agent, Word, Window, Percent, Label, Title, Tone string
		Old                                              bool
	}
	var got struct {
		Pages []struct {
			Name    string
			Columns []column
			Notes   []string
			Vendors []string
			Details []limitDetailState
		}
	}
	runFixture(t, "codexplates.html", &got)
	if len(got.Pages) != 6 {
		t.Fatalf("contours: %+v", got.Pages)
	}
	for _, page := range got.Pages {
		for _, r := range page.Columns {
			if r.Word != r.Agent || (r.Word != "claude" && r.Word != "codex") || r.Label == "" || r.Title != r.Label {
				t.Errorf("window vendor/details in %s: %+v", page.Name, r)
			}
		}
	}
	acme := got.Pages[0]
	if strings.Join(acme.Vendors, "|") != "claude|codex" {
		t.Errorf("vendor frames: %v", acme.Vendors)
	}
	if len(acme.Columns) != 2 || acme.Columns[0].Window != "7d" || acme.Columns[1].Window != "7d" || acme.Columns[1].Agent != "codex" || !strings.Contains(acme.Columns[1].Label, "38%") {
		t.Fatalf("Acme columns: %+v", acme)
	}
	if len(acme.Details) != 3 || !strings.Contains(acme.Details[0].Words, "7% used") || !strings.Contains(acme.Details[0].Words, "resets in 3 h") || !strings.Contains(acme.Details[1].Words, "41% used") || !strings.Contains(acme.Details[2].Words, "38% used") {
		t.Errorf("both Claude windows and Codex details: %+v", acme.Details)
	}
	personal := got.Pages[1]
	if strings.Join(personal.Vendors, "|") != "claude" {
		t.Errorf("Claude-only frame: %v", personal.Vendors)
	}
	if len(personal.Columns) != 1 || personal.Columns[0].Window != "7d" || personal.Columns[0].Word != "claude" {
		t.Errorf("Claude-only contour: %+v", personal)
	}
	lab := got.Pages[2]
	if len(lab.Columns) != 0 || strings.Join(lab.Notes, "|") != "codex has told no weekly window yet|codex says the limit is reached" {
		t.Errorf("untold Codex window: %+v", lab)
	}
	globex := got.Pages[3]
	if len(globex.Columns) != 2 || globex.Columns[0].Old || !globex.Columns[1].Old || !strings.Contains(strings.Join(globex.Notes, "|"), "codex's numbers are from 1 h ago") {
		t.Errorf("independent staleness: %+v", globex)
	}
	initech := got.Pages[4]
	if len(initech.Columns) != 1 || initech.Columns[0].Window != "5h" || !strings.Contains(initech.Columns[0].Label, "resets any moment") || !strings.Contains(initech.Columns[0].Tone, "crit") {
		t.Errorf("expired/minute resets: %+v", initech)
	}
	if len(initech.Details) != 2 || !strings.Contains(initech.Details[0].Words, "resets any moment") || !strings.Contains(initech.Details[1].Words, "resets in 30 min") || !strings.Contains(initech.Details[1].Tone, "warn") {
		t.Errorf("expired and minute resets in details: %+v", initech.Details)
	}
	hooli := got.Pages[5]
	if len(hooli.Columns) != 2 || hooli.Columns[0].Window != "7d" || hooli.Columns[1].Agent != "codex" {
		t.Errorf("unknown reset or missing five-hour window: %+v", hooli)
	}
}
