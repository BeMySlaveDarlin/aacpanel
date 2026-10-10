package check

import (
	"reflect"
	"strings"
	"testing"
)

type codexShelfRow struct {
	Name  string `json:"name"`
	Word  string `json:"word"`
	Model string `json:"model"`
	Sub   string `json:"sub"`
	State string `json:"state"`
	Stop  string `json:"stop"`
}

// A codex thread whose own turn is over while an agent it started works on is
// busy as a claude session waiting for its agents is: the bar over the
// composer says the agent works, the chip of the agents counts it, and the
// composer sends a message rather than offering to stop a turn there is none
// of. The list of the agents names each by its name with codex's word, its
// model and its role, the one at work first, and says how long it works or
// when and how it ended. The one at work is stopped by the id of its own
// thread, the stop of one that ended is off, and an agent opens its own
// thread.
func TestTheAgentsOfACodexThreadStandOnItsShelf(t *testing.T) {
	var got struct {
		Bar   string `json:"bar"`
		Chips []struct {
			Label string `json:"label"`
			Num   string `json:"num"`
		} `json:"chips"`
		Send     bool            `json:"send"`
		Stop     bool            `json:"stop"`
		Sub      string          `json:"sub"`
		Rows     []codexShelfRow `json:"rows"`
		StopSent []struct {
			Kind   string            `json:"kind"`
			Target string            `json:"target"`
			Params map[string]string `json:"params"`
		} `json:"stopSent"`
		Stopped string `json:"stopped"`
		Agent   struct {
			Name string `json:"name"`
			Sub  string `json:"sub"`
		} `json:"agent"`
	}
	runFixture(t, "codexshelf.html", &got)

	if got.Bar != "1 agent working" {
		t.Errorf("the bar over the composer says %q", got.Bar)
	}
	if len(got.Chips) != 1 || got.Chips[0].Num != "1" || got.Chips[0].Label != "subagents: 1 working" {
		t.Errorf("the chips under the composer read %+v: one, of the agents, counting the one at work", got.Chips)
	}
	if !got.Send || got.Stop {
		t.Errorf("the composer sends: %v, offers a stop: %v — the thread has no turn to stop", got.Send, got.Stop)
	}
	if got.Sub != "1 working, 1 over" {
		t.Errorf("the list of the agents is headed %q", got.Sub)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("the list of the agents drew %+v", got.Rows)
	}
	at, over := got.Rows[0], got.Rows[1]
	if at.Name != "tests" || at.Word != "Codex" || at.Model != "gpt-6-astra" || at.Sub != "" ||
		!strings.HasPrefix(at.State, "working for") || at.Stop != "on" {
		t.Errorf("the agent at work reads %+v and does not stand first", at)
	}
	want := codexShelfRow{Name: "lexer", Word: "Codex", Model: "gpt-6-astra", Sub: "explorer",
		State: "finished 6 min ago · ran 7 min", Stop: "off"}
	if !reflect.DeepEqual(over, want) {
		t.Errorf("the agent that finished reads %+v, want %+v", over, want)
	}
	if len(got.StopSent) != 1 || got.StopSent[0].Kind != "agent.stop" || got.StopSent[0].Target != "codex-0000abcd" ||
		!reflect.DeepEqual(got.StopSent[0].Params, map[string]string{"id": "01a12348-0000-7000-8000-0000000000c2"}) {
		t.Errorf("the stop of the agent at work sent %+v: a stop of an agent of the session, by its thread", got.StopSent)
	}
	if got.Stopped != "stopped from the panel" {
		t.Errorf("the agent stopped reads %q", got.Stopped)
	}
	if got.Agent.Name != "lexer" || !strings.Contains(got.Agent.Sub, "gpt-6-astra") {
		t.Errorf("the thread of the agent opened headed %+v", got.Agent)
	}
}
