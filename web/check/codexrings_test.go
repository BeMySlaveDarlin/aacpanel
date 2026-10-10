package check

import (
	"os"
	"strings"
	"testing"
)

// At a desk each contour's heading carries claude's two rings and, beside
// them, codex's week in codex's own hue, and the details under them name every
// window; a contour codex alone spent in has codex's ring alone. A codex
// thread with a title is listed by it on the desk, on the phone and in its
// sheet, while a press still opens it by its address.
func TestTheColumnShowsCodexsWeekAndATitledThread(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type ring struct {
		Tip   string `json:"tip"`
		Value string `json:"value"`
		Agent string `json:"agent"`
		Hue   string `json:"hue"`
	}
	var got struct {
		Heads []struct {
			Name  string `json:"name"`
			Rings []ring `json:"rings"`
		} `json:"heads"`
		Pop      []string `json:"pop"`
		DeskName struct {
			Text string `json:"text"`
			Tip  string `json:"tip"`
		} `json:"deskName"`
		PhoneName string   `json:"phoneName"`
		SheetName string   `json:"sheetName"`
		Opened    []string `json:"opened"`
	}
	runWideFixture(t, "codexrings.html", &got)

	if len(got.Heads) != 2 {
		t.Fatalf("the column has %d contour headings: %+v", len(got.Heads), got.Heads)
	}
	acme, lab := got.Heads[0], got.Heads[1]
	if len(acme.Rings) != 3 || acme.Rings[0].Value != "7" || acme.Rings[1].Value != "41" ||
		acme.Rings[2].Value != "63" || acme.Rings[2].Agent != "codex" || !strings.HasPrefix(acme.Rings[2].Tip, "Codex, seven days") {
		t.Errorf("the heading of Acme carries %+v, expected claude's two rings and codex's week last", acme.Rings)
	}
	if len(acme.Rings) == 3 && acme.Rings[2].Hue == acme.Rings[0].Hue {
		t.Errorf("codex's ring is painted %q like claude's", acme.Rings[2].Hue)
	}
	if len(lab.Rings) != 1 || lab.Rings[0].Value != "18" || lab.Rings[0].Agent != "codex" {
		t.Errorf("a contour codex alone spent in carries %+v, expected codex's week alone", lab.Rings)
	}
	if strings.Join(got.Pop, "|") != "Five hours|Seven days|Codex · seven days" {
		t.Errorf("the details of Acme's limits name %v", got.Pop)
	}
	if got.DeskName.Text != "Cart flakes" || got.DeskName.Tip != "codex-5afc361b" {
		t.Errorf("the desk lists the thread as %+v, expected its title with its address in the tip", got.DeskName)
	}
	if got.PhoneName != "Cart flakes" || got.SheetName != "Cart flakes" {
		t.Errorf("the phone lists the thread as %q and its sheet as %q", got.PhoneName, got.SheetName)
	}
	if strings.Join(got.Opened, ",") != "codex-5afc361b,codex-5afc361b" {
		t.Errorf("the presses opened %v, expected the thread by its address", got.Opened)
	}
}
