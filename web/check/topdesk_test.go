package check

import (
	"strings"
	"testing"
)

// The same two sources meet at a desk, in a table that has a column for the
// kind of each row: a container measured over the period, a process measured
// this second. A process has no average of its own, and the column says so
// rather than printing the figure of right now under the wrong heading.
func TestTheMachineTableAtADeskKeepsBothKinds(t *testing.T) {
	var got struct {
		Heavy        heavySeen `json:"heavy"`
		HeavyEmpty   heavySeen `json:"heavyEmpty"`
		HeavyError   string    `json:"heavyError"`
		HeavyMissing bool      `json:"heavyMissing"`
		Rows         []struct {
			Kind  string `json:"kind"`
			Name  string `json:"name"`
			Avg   string `json:"avg"`
			Peak  string `json:"peak"`
			Title string `json:"title"`
		} `json:"rows"`
		Note        string `json:"note"`
		Certificate string `json:"certificate"`
		Warning     bool   `json:"warning"`
		Programs    string `json:"programs"`
		Order       []int  `json:"order"`

		// Release is the card of the panel drawn with each panel the
		// fixture gives it.
		Release map[string]panelShot `json:"release"`
	}
	runWideFixture(t, "topdesk.html", &got)
	checkHeavyRuns(t, got.Heavy, got.HeavyEmpty)
	if !strings.Contains(got.HeavyError, "Queue unavailable: host read failed") || got.HeavyMissing {
		t.Errorf("the queue without a view is misreported: %q, missing %v", got.HeavyError, got.HeavyMissing)
	}
	checkPanelRelease(t, got.Release["newer"].Text, got.Release["newer"].Link, "v1.2.0")
	for name, want := range map[string][]string{
		"level":   {"v1.3.0", "Updated by ./install.sh update in its clone"},
		"off":     {"v1.2.0", "Not asking GitHub for newer releases: AACP_UPDATE_CHECK=0"},
		"unknown": {"version unknown", "Built without a release: told of none"},
	} {
		r := got.Release[name]
		for _, w := range want {
			if !strings.Contains(r.Text, w) {
				t.Errorf("the card of the panel %s reads %q, want %q in it", name, r.Text, w)
			}
		}
		if strings.Contains(r.Text, "is out") || r.Link != "" {
			t.Errorf("the card of the panel %s tells of a release with %q and the link %q", name, r.Text, r.Link)
		}
	}
	if len(got.Order) != 3 || got.Order[0] >= got.Order[1] || got.Order[1] >= got.Order[2] {
		t.Errorf("metrics must precede agents and the certificate: %v", got.Order)
	}
	if !strings.Contains(got.Programs, "0.163.0") || !strings.Contains(got.Programs, "2.1.283") {
		t.Errorf("desktop agent versions are missing: %s", got.Programs)
	}

	if !strings.Contains(got.Certificate, "panel.example.org") || !strings.Contains(got.Certificate, "13 days left") || !got.Warning {
		t.Errorf("LAN certificate is not visible: %q (warning %v)", got.Certificate, got.Warning)
	}
	if len(got.Rows) != 3 {
		t.Fatalf("the table holds %d rows, and the fixture gave it two containers and one process", len(got.Rows))
	}

	var kinds []string
	for _, row := range got.Rows {
		kinds = append(kinds, row.Kind)
	}
	if got.Rows[0].Kind != "process" || !strings.Contains(got.Rows[0].Name, "burn.py") {
		t.Errorf("the table opens with a %s row named %q: the biggest consumer is not at the top", got.Rows[0].Kind, got.Rows[0].Name)
	}
	if !strings.Contains(strings.Join(kinds, " "), "container") {
		t.Errorf("no container is left in the table, only %v", kinds)
	}
	if got.Rows[0].Peak != "—" {
		t.Errorf("the process row shows a peak of %q: nothing recorded one, and a figure there reads as history", got.Rows[0].Peak)
	}
	if !strings.Contains(got.Rows[0].Title, "pid 4242") {
		t.Errorf("the process row carries %q: without the pid there is nothing to go and look at", got.Rows[0].Title)
	}

	// A container keeps the two figures the history has for it.
	for _, row := range got.Rows {
		if row.Kind != "container" {
			continue
		}
		if row.Avg == "—" || row.Peak == "—" {
			t.Errorf("the container %q shows avg=%q peak=%q: the history has both", row.Name, row.Avg, row.Peak)
		}
	}
	if !strings.Contains(got.Note, "right now") {
		t.Errorf("the table explains itself as %q: nothing says which half of it is not history", got.Note)
	}
}

// panelShot is the card of the panel as a fixture read it: its words and the
// link of the news.
type panelShot struct {
	Text string `json:"text"`
	Link string `json:"link"`
}

// checkPanelRelease holds the card of the panel with a newer release out: the
// release it runs, the release out with the link to its page, and the command
// that gets there.
func checkPanelRelease(t *testing.T, text, link, running string) {
	t.Helper()
	for _, want := range []string{"Panel", running, "v1.3.0 is out — ./install.sh update"} {
		if !strings.Contains(text, want) {
			t.Errorf("the card of the panel omits %q: %q", want, text)
		}
	}
	if link != "https://github.com/BeMySlaveDarlin/aacpanel/releases/tag/v1.3.0" {
		t.Errorf("the news of the release links to %q, not to its page", link)
	}
}
