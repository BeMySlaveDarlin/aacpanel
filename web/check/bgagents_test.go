package check

import (
	"strings"
	"testing"
)

type bgAgentsShot struct {
	Chips []struct {
		Label string `json:"label"`
		Num   string `json:"num"`
	} `json:"chips"`
	Sub  string `json:"sub"`
	Rows []struct {
		Name  string `json:"name"`
		State string `json:"state"`
		Stop  string `json:"stop"`
	} `json:"rows"`
	Hint   string   `json:"hint"`
	Tasks  []string `json:"tasks"`
	Opened []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"opened"`
	StopSent []struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
		Work   *struct {
			ID   string `json:"id"`
			Line string `json:"line"`
		} `json:"work"`
	} `json:"stopSent"`
}

// An agent the session sent to the background is one of its agents: it stands
// in their list and on their chip, and opens its conversation. One that ended
// keeps its row, saying how it ended; the list of commands holds commands. The
// one at work is stopped as the background task it is to claude.
func TestABackgroundAgentStandsAmongTheAgents(t *testing.T) {
	var got bgAgentsShot
	runFixture(t, "bgagents.html", &got)

	if len(got.Chips) != 3 || got.Chips[0].Num != "1" || got.Chips[1].Num != "1" {
		t.Errorf("the chips read %+v: one command and one agent at work", got.Chips)
	}
	if len(got.Rows) != 4 {
		t.Fatalf("the list of agents drew %+v", got.Rows)
	}
	live := got.Rows[0]
	if !strings.HasPrefix(live.Name, "cx-scout") || !strings.HasPrefix(live.State, "working for") {
		t.Errorf("the agent at work reads %+v and does not stand first", live)
	}
	states := []string{}
	for _, r := range got.Rows[1:] {
		states = append(states, r.State)
	}
	all := strings.Join(states, " | ")
	for _, want := range []string{"finished 11 min ago · ran 1 min", "failed 25 min ago · ran 5 min", "silent for"} {
		if !strings.Contains(all, want) {
			t.Errorf("the agents that are not at work read %q: no %q", all, want)
		}
	}
	if got.Sub != "1 working, 1 reported, 2 over" {
		t.Errorf("the head of the list reads %q", got.Sub)
	}
	if got.Hint == "" {
		t.Errorf("a teammate that reported lost the line that says what reported means")
	}
	for _, r := range got.Rows {
		if strings.HasPrefix(r.Name, "cx-scout") && r.Stop != "on" {
			t.Errorf("the stop of the agent at work is %s", r.Stop)
		}
		if strings.HasPrefix(r.Name, "sub-run") && r.Stop != "off" {
			t.Errorf("the stop of %q, which is over, is %s", r.Name, r.Stop)
		}
	}
	if len(got.StopSent) != 1 || got.StopSent[0].Kind != "task.stop" || got.StopSent[0].Work == nil ||
		got.StopSent[0].Work.ID != "adf5ce617c6afff7a" || got.StopSent[0].Work.Line != "@cx-scout" {
		t.Errorf("the stop of the agent at work sent %+v: a stop of its background task, by its id and its line", got.StopSent)
	}
	if len(got.Tasks) != 1 || !strings.HasPrefix(got.Tasks[0], "make check") {
		t.Errorf("the list of commands holds %v", got.Tasks)
	}
	if len(got.Opened) != 1 || got.Opened[0].ID != "adf5ce617c6afff7a" || got.Opened[0].Kind != "background" {
		t.Errorf("tapping the agent opened %+v: its conversation, as a background agent", got.Opened)
	}
}
