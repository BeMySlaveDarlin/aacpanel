package check

import (
	"strings"
	"testing"
)

func TestThePhoneContourPageShowsCodexsWeekBesideClaudesLimits(t *testing.T) {
	type ring struct {
		Agent, Word, Window, Percent, Label, Title, Tone string
		Old                                              bool
	}
	var got struct {
		Pages []struct {
			Name    string
			Rings   []ring
			Notes   []string
			Vendors []string
		}
	}
	runFixture(t, "codexplates.html", &got)
	if len(got.Pages) != 6 {
		t.Fatalf("contours: %+v", got.Pages)
	}
	for _, page := range got.Pages {
		for _, r := range page.Rings {
			if r.Word != r.Agent || (r.Word != "claude" && r.Word != "codex") || r.Label == "" || r.Title != r.Label {
				t.Errorf("window vendor/details in %s: %+v", page.Name, r)
			}
		}
	}
	acme := got.Pages[0]
	if strings.Join(acme.Vendors, "|") != "claude|codex" {
		t.Errorf("vendor frames: %v", acme.Vendors)
	}
	if len(acme.Rings) != 3 || acme.Rings[0].Window != "3h/5h" || acme.Rings[1].Window != "4d/7d" || acme.Rings[2].Window != "2d/7d" || acme.Rings[2].Agent != "codex" || !strings.Contains(acme.Rings[2].Label, "38%") {
		t.Fatalf("Acme rings: %+v", acme)
	}
	personal := got.Pages[1]
	if strings.Join(personal.Vendors, "|") != "claude" {
		t.Errorf("Claude-only frame: %v", personal.Vendors)
	}
	if len(personal.Rings) != 2 || personal.Rings[0].Window != "2h/5h" || personal.Rings[1].Word != "claude" {
		t.Errorf("Claude-only contour: %+v", personal)
	}
	lab := got.Pages[2]
	if len(lab.Rings) != 0 || strings.Join(lab.Notes, "|") != "codex has told no weekly window yet|codex says the limit is reached" {
		t.Errorf("untold Codex window: %+v", lab)
	}
	globex := got.Pages[3]
	if len(globex.Rings) != 3 || globex.Rings[0].Old || !globex.Rings[2].Old || !strings.Contains(strings.Join(globex.Notes, "|"), "codex's numbers are from 1 h ago") {
		t.Errorf("independent staleness: %+v", globex)
	}
	initech := got.Pages[4]
	if len(initech.Rings) != 2 || initech.Rings[0].Window != "0m/5h" || initech.Rings[1].Window != "30m/7d" || !strings.Contains(initech.Rings[0].Label, "resets any moment") || !strings.Contains(initech.Rings[1].Tone, "warn") {
		t.Errorf("expired/minute resets: %+v", initech)
	}
	hooli := got.Pages[5]
	if len(hooli.Rings) != 2 || hooli.Rings[0].Window != "7d" || hooli.Rings[1].Agent != "codex" {
		t.Errorf("unknown reset or missing five-hour window: %+v", hooli)
	}
}
