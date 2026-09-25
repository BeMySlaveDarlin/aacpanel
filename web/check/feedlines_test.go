package check

import (
	"strings"
	"testing"
)

type feedLinesShot struct {
	Badges []string `json:"badges"`
	Lines  []struct {
		Text   string `json:"text"`
		Dot    string `json:"dot"`
		Colour string `json:"colour"`
	} `json:"lines"`
	Turn  string   `json:"turn"`
	Under []string `json:"under"`
}

// What a terminal prints beside the conversation is in the feed as well: a
// hook's message adds to a badge of its run, a background task done hangs
// under the run it ended in, the length of a turn stands under its last
// answer with the time it ended, and a warning of claude is a line of its own.
func TestWhatTheTerminalPrintsBesideTheConversationIsInTheFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)

	if strings.Join(got.Badges, " | ") != "commands: 1 call | hooks: 1 call" {
		t.Errorf("the run's badges: %v", got.Badges)
	}
	if len(got.Lines) != 5 {
		t.Fatalf("lines drawn: %+v", got.Lines)
	}
	if got.Lines[0].Text != `Agent "Commit the notes" finished 24s · 33k tokens` {
		t.Errorf("an agent done reads %q", got.Lines[0].Text)
	}
	if len(got.Under) != 2 {
		t.Errorf("the tasks done inside the run do not hang under its badges: %v", got.Under)
	}
	if !strings.HasPrefix(got.Turn, "worked 2m 49s") || got.Turn == "worked 2m 49s" {
		t.Errorf("the turn reads %q: the length the way the terminal says it, and the time it ended", got.Turn)
	}
	if got.Lines[1].Dot == got.Lines[0].Dot {
		t.Errorf("a failed task has the dot of a finished one: %s", got.Lines[1].Dot)
	}
	warn, crit := got.Lines[3], got.Lines[4]
	if warn.Dot == crit.Dot || crit.Colour == warn.Colour {
		t.Errorf("a refusal reads like a warning: %+v %+v", warn, crit)
	}
	if !strings.HasPrefix(got.Lines[2].Text, "while you were away") {
		t.Errorf("the recap does not say what it is: %q", got.Lines[2].Text)
	}
}
