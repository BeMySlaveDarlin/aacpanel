package check

import (
	"os"
	"strings"
	"testing"
)

// Each provider supplied by the contour gets its own group and window
// details. Thread titles remain separate from the address used to open them.
func TestTheColumnShowsCodexsWeekAndATitledThread(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type ring struct {
		Tip   string `json:"tip"`
		Value string `json:"value"`
		Agent string `json:"agent"`
	}
	var got struct {
		Heads []struct {
			Name   string   `json:"name"`
			Words  []string `json:"words"`
			Rings  []ring   `json:"rings"`
			Groups []struct {
				Agent, Label string
				Windows      int
			}
		} `json:"heads"`
		Pop                                        []string `json:"pop"`
		ToggledClosed, EscapeClosed, OutsideClosed bool
		DeskName                                   struct {
			Text string `json:"text"`
			Tip  string `json:"tip"`
		} `json:"deskName"`
		PhoneName string   `json:"phoneName"`
		Opened    []string `json:"opened"`
	}
	runWideFixture(t, "codexrings.html", &got)

	if len(got.Heads) != 3 {
		t.Fatalf("the column has %d contour headings: %+v", len(got.Heads), got.Heads)
	}
	acme, lab, home := got.Heads[0], got.Heads[1], got.Heads[2]
	if len(acme.Rings) != 5 || acme.Rings[0].Value != "7" || acme.Rings[1].Value != "41" ||
		acme.Rings[2].Value != "63" || acme.Rings[2].Agent != "codex" || !strings.HasPrefix(acme.Rings[2].Tip, "Codex, 7 days") {
		t.Errorf("the heading of Acme carries %+v, expected Claude and Codex followed by Gemini from the data", acme.Rings)
	}
	if len(lab.Rings) != 1 || lab.Rings[0].Value != "18" || lab.Rings[0].Agent != "codex" {
		t.Errorf("a contour codex alone spent in carries %+v, expected codex's week alone", lab.Rings)
	}
	if len(home.Rings) != 1 || home.Rings[0].Value != "9" || !strings.HasPrefix(home.Rings[0].Tip, "Claude, 7 days") {
		t.Errorf("an account with no five-hour window carries %+v, expected claude's week alone", home.Rings)
	}
	for i, want := range []string{"claude:claude,codex:codex,gemini:gemini", "codex:codex", "claude:claude"} {
		if head := got.Heads[i]; strings.Join(head.Words, ",") != want {
			t.Errorf("the rings of %s stand after the words %v, expected %s — each agent's rings after its word, in its hue", head.Name, head.Words, want)
		}
	}
	if strings.Join(got.Pop, "|") != "Claude, 5 hours|Claude, 7 days|Codex, 7 days|Gemini, 5 hours|Gemini, 7 days" {
		t.Errorf("the details of Acme's limits name %v", got.Pop)
	}
	if len(acme.Groups) != 3 || acme.Groups[2].Agent != "gemini" || acme.Groups[2].Label != "gemini limits" || acme.Groups[2].Windows != 2 ||
		len(acme.Rings) < 5 || acme.Rings[3].Value != "27" || acme.Rings[4].Value != "55" ||
		acme.Rings[3].Agent != "gemini" || acme.Rings[4].Agent != "gemini" {
		t.Errorf("the third provider must come from contour data: groups %+v, readings %+v", acme.Groups, acme.Rings)
	}
	if !got.ToggledClosed || !got.EscapeClosed || !got.OutsideClosed {
		t.Errorf("details close: toggle=%v, Escape=%v, outside=%v", got.ToggledClosed, got.EscapeClosed, got.OutsideClosed)
	}
	if got.DeskName.Text != "Cart flakes" || got.DeskName.Tip != "codex-5afc361b" {
		t.Errorf("the desk lists the thread as %+v, expected its title with its address in the tip", got.DeskName)
	}
	if got.PhoneName != "Cart flakes" {
		t.Errorf("the phone lists the thread as %q", got.PhoneName)
	}
	if strings.Join(got.Opened, ",") != "codex-5afc361b,codex-5afc361b" {
		t.Errorf("the presses opened %v, expected the thread by its address", got.Opened)
	}
}
