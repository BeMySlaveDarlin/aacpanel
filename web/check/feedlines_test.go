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
	} `json:"pills"`
	FeedLeft float64 `json:"feedLeft"`
	Plates   []struct {
		Tag   string  `json:"tag"`
		Left  float64 `json:"left"`
		Width float64 `json:"width"`
	} `json:"plates"`
	TurnLeft float64 `json:"turnLeft"`
	Opened   []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Agent bool   `json:"agent"`
	} `json:"opened"`
	Lines []struct {
		Text   string `json:"text"`
		Dot    string `json:"dot"`
		Colour string `json:"colour"`
	} `json:"lines"`
	Turn  string   `json:"turn"`
	Under []string `json:"under"`
}

// What a terminal prints beside the conversation is in the feed as well: a
// hook's message adds to a badge of its run, a background task done is a plate
// under the run it ended in — one width for all, read from the left, opening
// what the task left behind — the length of a turn stands under its last
// answer with the time it ended, and a warning of claude is a line of its own.
func TestWhatTheTerminalPrintsBesideTheConversationIsInTheFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)

	if strings.Join(got.Badges, " | ") != "commands: 1 call | hooks: 1 call" {
		t.Errorf("the run's badges: %v", got.Badges)
	}
	if len(got.Pills) != 3 {
		t.Fatalf("plates drawn: %+v", got.Pills)
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
	for i, p := range got.Plates {
		if p.Left-got.FeedLeft > 1 || p.Width != got.Plates[0].Width {
			t.Errorf("plate %d starts at %.1f (the feed at %.1f) and is %.1f wide against %.1f",
				i, p.Left, got.FeedLeft, p.Width, got.Plates[0].Width)
		}
	}
	if got.TurnLeft-got.FeedLeft > 1 {
		t.Errorf("the length of the turn starts at %.1f, the feed at %.1f", got.TurnLeft, got.FeedLeft)
	}
	tags := []string{}
	for _, p := range got.Plates {
		tags = append(tags, p.Tag)
	}
	if strings.Join(tags, ",") != "BUTTON,BUTTON,DIV" {
		t.Errorf("the plates are %v: a task that names itself opens, one that does not is not a button", tags)
	}
	if len(got.Opened) != 2 || got.Opened[0].ID != "a515a204cce27a85c" || !got.Opened[0].Agent ||
		got.Opened[1].ID != "bg3me3whb" || got.Opened[1].Agent || got.Opened[1].Name != "make check" {
		t.Errorf("tapping the plates opened %+v: the agent its conversation, the command its output", got.Opened)
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
