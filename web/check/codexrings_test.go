package check

import (
	"os"
	"strings"
	"testing"
)

type deskLimitGroup struct {
	Agent, Label, Role string
	Windows            []string
	Icon               bool
}

func checkDeskLimitGroup(t *testing.T, got deskLimitGroup, agent string, windows int) {
	t.Helper()
	if got.Agent != agent || got.Label != agent+" limits" || got.Role != "group" || !got.Icon {
		t.Errorf("subscription group %+v does not identify %s with a nonempty icon", got, agent)
	}
	if len(got.Windows) != windows {
		t.Errorf("%s holds %d windows, expected %d: %v", agent, len(got.Windows), windows, got.Windows)
	}
	for _, label := range got.Windows {
		if !strings.HasPrefix(strings.ToLower(label), agent+", ") {
			t.Errorf("%s holds another provider's window %q", agent, label)
		}
	}
}

// Each provider supplied by the contour gets its own group and window
// details. Thread titles remain separate from the address used to open them.
func TestTheColumnShowsCodexsWeekAndATitledThread(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type chip struct {
		Tip    string `json:"tip"`
		Value  string `json:"value"`
		Agent  string `json:"agent"`
		Vendor string `json:"vendor"`
		Left   string `json:"left"`
	}
	var got struct {
		Heads []struct {
			Name   string           `json:"name"`
			Agents []string         `json:"agents"`
			Chips  []chip           `json:"chips"`
			Groups []deskLimitGroup `json:"groups"`
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
	if len(acme.Chips) != 5 || acme.Chips[0].Value != "7" || acme.Chips[1].Value != "41" ||
		acme.Chips[2].Value != "63" || acme.Chips[2].Agent != "codex" || !strings.HasPrefix(acme.Chips[2].Tip, "Codex, 7 days") {
		t.Errorf("the heading of Acme carries %+v, expected Claude and Codex followed by Gemini from the data", acme.Chips)
	}
	if len(lab.Chips) != 1 || lab.Chips[0].Value != "18" || lab.Chips[0].Agent != "codex" {
		t.Errorf("a contour codex alone spent in carries %+v, expected codex's week alone", lab.Chips)
	}
	if len(home.Chips) != 1 || home.Chips[0].Value != "9" || !strings.HasPrefix(home.Chips[0].Tip, "Claude, 7 days") {
		t.Errorf("an account with no five-hour window carries %+v, expected claude's week alone", home.Chips)
	}
	for i, want := range []string{"claude,codex,gemini", "codex", "claude"} {
		if head := got.Heads[i]; strings.Join(head.Agents, ",") != want {
			t.Errorf("the chips of %s identify agents %v, expected %s", head.Name, head.Agents, want)
		}
		agents := strings.Split(want, ",")
		if len(got.Heads[i].Groups) != len(agents) {
			t.Fatalf("%s has subscription groups %+v, expected %v", got.Heads[i].Name, got.Heads[i].Groups, agents)
		}
		for j, agent := range agents {
			windows := 1
			if i == 0 && agent != "codex" {
				windows = 2
			}
			checkDeskLimitGroup(t, got.Heads[i].Groups[j], agent, windows)
		}
	}
	for _, head := range got.Heads {
		for _, reading := range head.Chips {
			if reading.Vendor != reading.Agent {
				t.Errorf("subscription mark %q does not identify agent %q", reading.Vendor, reading.Agent)
			}
		}
	}
	if strings.Join(got.Pop, "|") != "Claude, 5 hours|Claude, 7 days|Codex, 7 days|Gemini, 5 hours|Gemini, 7 days" {
		t.Errorf("the details of Acme's limits name %v", got.Pop)
	}
	if len(acme.Agents) != 3 || acme.Agents[2] != "gemini" ||
		len(acme.Chips) < 5 || acme.Chips[3].Value != "27" || acme.Chips[4].Value != "55" ||
		acme.Chips[3].Agent != "gemini" || acme.Chips[4].Agent != "gemini" {
		t.Errorf("the third provider must come from contour data: agents %+v, readings %+v", acme.Agents, acme.Chips)
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
