package check

import (
	"strings"
	"testing"
)

type codexRunShot struct {
	Rows []struct {
		Name   string `json:"name"`
		Word   string `json:"word"`
		Key    string `json:"key"`
		Colour string `json:"colour"`
		Stop   string `json:"stop"`
		State  string `json:"state"`
	} `json:"rows"`
	Sub          string `json:"sub"`
	ClaudeColour string `json:"claudeColour"`
	Opened       struct {
		ID    string `json:"id"`
		Agent string `json:"agent"`
		Name  string `json:"name"`
	} `json:"opened"`
	FeedID       string `json:"feedId"`
	ClaudeFeedID string `json:"claudeFeedId"`
	Head         *struct {
		Word string `json:"word"`
		Key  string `json:"key"`
	} `json:"head"`
	Asked []string `json:"asked"`
}

// A run of codex exec a claude session started is one of its agents: its row
// says Codex in the colour of codex, offers no stop — claude has no task of it
// and the panel no daemon of its thread — and opens the feed of its thread,
// asked for by the id of the thread alone rather than as an agent beside the
// conversation, which the service would refuse. A run that is over reads as
// over among the agents that are, and is not counted at work.
func TestACodexRunStandsAmongTheAgentsAndOpensItsThread(t *testing.T) {
	const thread = "01a12600-0000-7000-8000-0000000000e1"
	var got codexRunShot
	runFixture(t, "codexrun.html", &got)

	if len(got.Rows) != 3 {
		t.Fatalf("the list of agents drew %+v", got.Rows)
	}
	run, scout, over := got.Rows[0], got.Rows[1], got.Rows[2]
	if !strings.HasPrefix(over.Name, "Review the change") || over.Word != "Codex" || over.Stop != "no" {
		t.Errorf("the run that is over reads %+v", over)
	}
	if !strings.Contains(over.State, " ago") || strings.Contains(over.State, "working") {
		t.Errorf("the run that is over says %q of itself", over.State)
	}
	if got.Sub != "2 working, 1 over" {
		t.Errorf("the list of agents counts %q", got.Sub)
	}
	if !strings.HasPrefix(run.Name, "reviewer") || run.Word != "Codex" || run.Key != "codex" {
		t.Errorf("the run reads %+v: its role, then the word of codex", run)
	}
	if run.Colour == "" || run.Colour == got.ClaudeColour {
		t.Errorf("the word of the run is painted %q, the word of claude %q", run.Colour, got.ClaudeColour)
	}
	if run.Stop != "no" {
		t.Errorf("the run offers a stop the panel has no way to make")
	}
	if scout.Word != "" || scout.Stop != "yes" {
		t.Errorf("an agent of claude's reads %+v: no word of codex, its stop kept", scout)
	}
	if got.Opened.ID != thread || got.Opened.Agent != "codex" {
		t.Errorf("a tap on the run opened %+v", got.Opened)
	}
	if got.FeedID != thread {
		t.Errorf("the feed of the run is asked for as %q, not as its thread", got.FeedID)
	}
	if got.ClaudeFeedID != "55555555-5555-4555-8555-555555555555:adf5ce617c6afff7a" {
		t.Errorf("the feed of an agent of claude's moved: %q", got.ClaudeFeedID)
	}
	if len(got.Asked) == 0 || !strings.Contains(got.Asked[0], "id="+thread+"&") {
		t.Errorf("the feed of the run asked %v", got.Asked)
	}
	if got.Head == nil || got.Head.Word != "Codex" || got.Head.Key != "codex" {
		t.Errorf("the head of the feed of the run reads %+v", got.Head)
	}
}

type codexOutsideShot struct {
	Place       string   `json:"place"`
	DaemonPlace string   `json:"daemonPlace"`
	Actions     int      `json:"actions"`
	Opens       bool     `json:"opens"`
	OwnTools    []string `json:"ownTools"`
	OwnNote     string   `json:"ownNote"`
	DaemonTools []string `json:"daemonTools"`
}

// A codex running on its own is only read: its card opens the conversation
// and offers no actions, a busy one included, and the tools of
// its conversation are where it lives, its id and its resume command. A
// thread of a daemon keeps its stop, its restart and its close.
func TestACodexOfItsOwnIsOnlyRead(t *testing.T) {
	var got codexOutsideShot
	runFixture(t, "codexoutside.html", &got)

	if got.Place != "outside" || got.DaemonPlace != "daemon" {
		t.Errorf("a codex of its own lives %q, a thread of a daemon %q", got.Place, got.DaemonPlace)
	}
	if got.Actions != 0 || !got.Opens {
		t.Errorf("the outside card has %d actions and opens %v", got.Actions, got.Opens)
	}
	if strings.Join(got.OwnTools, " | ") != "Outside the panel | Copy the session ID | Copy the resume command" ||
		!strings.Contains(got.OwnNote, "cannot write to it") {
		t.Errorf("the tools of a codex of its own are %v, %q", got.OwnTools, got.OwnNote)
	}
	daemon := strings.Join(got.DaemonTools, " | ")
	if !strings.Contains(daemon, "Stop the turn") || !strings.Contains(daemon, "Close the session") {
		t.Errorf("a thread of a daemon lost its tools: %v", got.DaemonTools)
	}
}
