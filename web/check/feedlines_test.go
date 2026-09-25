package check

import (
	"strings"
	"testing"
)

type feedLinesShot struct {
	Badges []string `json:"badges"`
	Pills  []struct {
		Mark       string `json:"mark"`
		Name       string `json:"name"`
		Aside      string `json:"aside"`
		Said       string `json:"said"`
		MarkColour string `json:"markColour"`
		Centred    bool   `json:"centred"`
	} `json:"pills"`
	Lines []struct {
		Text   string `json:"text"`
		Dot    string `json:"dot"`
		Colour string `json:"colour"`
	} `json:"lines"`
	Turn  string   `json:"turn"`
	Under []string `json:"under"`
}

// What a terminal prints beside the conversation is in the feed as well: a
// hook's message adds to a badge of its run, a background task done is a pill
// under the run it ended in, the length of a turn stands under its last answer
// with the time it ended, and a warning of claude is a line of its own.
func TestWhatTheTerminalPrintsBesideTheConversationIsInTheFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)

	if strings.Join(got.Badges, " | ") != "commands: 1 call | hooks: 1 call" {
		t.Errorf("the run's badges: %v", got.Badges)
	}
	if len(got.Pills) != 2 {
		t.Fatalf("pills drawn: %+v", got.Pills)
	}
	agent, failed := got.Pills[0], got.Pills[1]
	if agent.Mark != "✓" || agent.Name != "Commit the notes" || agent.Aside != "24s · 33k tokens" {
		t.Errorf("an agent done reads %q %q %q", agent.Mark, agent.Name, agent.Aside)
	}
	if !strings.Contains(agent.Said, `Agent "Commit the notes" finished`) {
		t.Errorf("the pill does not say in words what happened: %q", agent.Said)
	}
	if failed.Mark != "✗" || failed.Name != "make check" || failed.Aside != "exit 2" {
		t.Errorf("a failed command reads %q %q %q", failed.Mark, failed.Name, failed.Aside)
	}
	if failed.MarkColour == agent.MarkColour {
		t.Errorf("a failed task is marked in the colour of a finished one: %s", failed.MarkColour)
	}
	for _, p := range got.Pills {
		if !p.Centred {
			t.Errorf("the pill of %q is not in the middle of the feed", p.Name)
		}
	}
	if len(got.Under) != 2 {
		t.Errorf("the tasks done inside the run do not hang under its badges: %v", got.Under)
	}
	if !strings.HasPrefix(got.Turn, "worked 2m 49s") || got.Turn == "worked 2m 49s" {
		t.Errorf("the turn reads %q: the length the way the terminal says it, and the time it ended", got.Turn)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("lines drawn: %+v", got.Lines)
	}
	recap, warn, crit := got.Lines[0], got.Lines[1], got.Lines[2]
	if !strings.HasPrefix(recap.Text, "while you were away") {
		t.Errorf("the recap does not say what it is: %q", recap.Text)
	}
	if warn.Dot == crit.Dot || crit.Colour == warn.Colour {
		t.Errorf("a refusal reads like a warning: %+v %+v", warn, crit)
	}
}
