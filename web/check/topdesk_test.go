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
		Rows []struct {
			Kind  string `json:"kind"`
			Name  string `json:"name"`
			Avg   string `json:"avg"`
			Peak  string `json:"peak"`
			Title string `json:"title"`
		} `json:"rows"`
		Note string `json:"note"`
	}
	runWideFixture(t, "topdesk.html", &got)

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
