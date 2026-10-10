package check

import (
	"os"
	"strings"
	"testing"
)

// The usage page opens with the limits of every contour: claude's five hours
// and seven days, and beside them codex's week, a window as wide as one of
// claude's, each line led by the word of whose it is in that agent's hue. A
// contour codex alone has spent in has codex's line alone, and an account
// that told no week, or ran into its limit, says so rather than drawing an
// empty bar.
func TestTheUsagePageShowsCodexsWeekBesideClaudesLimits(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type line struct {
		Agent   string   `json:"agent"`
		Windows []string `json:"windows"`
		Width   int      `json:"width"`
		Colour  string   `json:"colour"`
		Note    string   `json:"note"`
	}
	var got struct {
		After    string `json:"after"`
		Contours []struct {
			Name  string `json:"name"`
			Lines []line `json:"lines"`
		} `json:"contours"`
	}
	runFixture(t, "codexlimits.html", &got)

	if !strings.Contains(got.After, "ufilters") {
		t.Errorf("the limits stand after %q, expected right under the filters", got.After)
	}
	if len(got.Contours) != 3 {
		t.Fatalf("the limits show %d contours: %+v", len(got.Contours), got.Contours)
	}
	acme := got.Contours[0]
	if acme.Name != "acme" || len(acme.Lines) != 2 || acme.Lines[0].Agent != "Claude" || acme.Lines[1].Agent != "Codex" {
		t.Fatalf("the first contour reads %+v, expected acme with a line of Claude and a line of Codex", acme)
	}
	if len(acme.Lines[0].Windows) != 2 || !strings.HasPrefix(acme.Lines[0].Windows[0], "5 hours 7%") ||
		!strings.HasPrefix(acme.Lines[0].Windows[1], "7 days 41%") {
		t.Errorf("claude's line of acme reads %v", acme.Lines[0].Windows)
	}
	if len(acme.Lines[1].Windows) != 1 || !strings.HasPrefix(acme.Lines[1].Windows[0], "7 days 63% resets in") {
		t.Errorf("codex's line of acme reads %v, expected its week alone", acme.Lines[1].Windows)
	}
	if w, c := acme.Lines[1].Width, acme.Lines[0].Width; w < c-4 || w > c+4 {
		t.Errorf("codex's week is %dpx wide and a window of claude's %dpx: one window, as wide as one of claude's", w, c)
	}
	if acme.Lines[0].Colour == acme.Lines[1].Colour {
		t.Errorf("Claude and Codex are painted alike (%s)", acme.Lines[0].Colour)
	}
	personal := got.Contours[1]
	if personal.Name != "personal" || len(personal.Lines) != 1 || personal.Lines[0].Agent != "Claude" {
		t.Errorf("a contour without codex reads %+v, expected claude's line alone", personal)
	}
	lab := got.Contours[2]
	if lab.Name != "lab" || len(lab.Lines) != 1 || lab.Lines[0].Agent != "Codex" ||
		strings.Join(lab.Lines[0].Windows, "") != "codex has told no weekly window yet" ||
		lab.Lines[0].Note != "codex says the limit is reached" {
		t.Errorf("a contour codex alone spent in reads %+v", lab)
	}
}
