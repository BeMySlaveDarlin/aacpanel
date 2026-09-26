package check

import (
	"os/exec"
	"path/filepath"
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
	Turn      string   `json:"turn"`
	TurnNum   string   `json:"turnNum"`
	TurnSays  string   `json:"turnLeftSay"`
	TurnIcon  bool     `json:"turnIcon"`
	TurnOpens []int    `json:"turnOpens"`
	Worked    bool     `json:"worked"`
	Under     []string `json:"under"`
}

// What a terminal prints beside the conversation is in the feed as well: a
// hook's message adds to a badge of its run, a background task done is a plate
// under the run it ended in — one width for all, read from the left, opening
// what the task left behind — the end of a turn is a badge under its last
// answer that opens the calls of the turn with how long it took, and a
// warning of claude is a line of its own.
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
		t.Errorf("the badge of the turn starts at %.1f, the feed at %.1f", got.TurnLeft, got.FeedLeft)
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
	if got.Worked {
		t.Errorf("the length of a turn is printed in the feed: it belongs to the calls the badge opens")
	}
	if !got.TurnIcon || got.TurnNum != "2" {
		t.Errorf("the badge of the turn is not an hourglass with the calls it opens: icon %v, number %q — "+
			"a number on a badge is the calls behind it, as on every badge beside it", got.TurnIcon, got.TurnNum)
	}
	if got.TurnSays != "· 3 at work" {
		t.Errorf("the agents the turn left at work read %q: in words, so they are not taken for calls", got.TurnSays)
	}
	if !strings.Contains(got.Turn, "worked 2m 49s") || !strings.Contains(got.Turn, "2 calls") ||
		!strings.Contains(got.Turn, "3 background agents were still at work") {
		t.Errorf("the badge of the turn says %q: how long it took, the calls and what it left at work", got.Turn)
	}
	if len(got.TurnOpens) != 1 || got.TurnOpens[0] != 7 {
		t.Errorf("tapping the badge of the turn opened %v: the calls of that turn", got.TurnOpens)
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

// The end of a turn counts the calls of its own turn only: the number on its
// badge is what the calls sheet it opens lists, and the turn before it has a
// badge of its own.
func TestTheTurnCountsItsOwnCalls(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the feed is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "feed.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { rows } from ` + jsString("file://"+path) + `;
const items = [
    { role: "tools", kind: "bash", run: 1, pos: 1, calls: [{ name: "Bash", pos: 1 }, { name: "Read", pos: 1, index: 1 }] },
    { role: "think", run: 1, pos: 2, spots: [{ pos: 2 }] },
    { role: "turn", pos: 3, ms: 1000 },
    { role: "ai", text: "next", pos: 4 },
    { role: "tools", kind: "bash", run: 5, pos: 5, calls: [{ name: "Bash", pos: 5 }] },
    { role: "turn", pos: 6, ms: 1000, agents: 3 },
    { role: "turn", pos: 7, ms: 1000 },
];
process.stdout.write(JSON.stringify(rows(items).filter((r) => r.role === "turn").map((r) => r.calls)));
`
	out, err := exec.Command(node, "--input-type=module", "-e", script).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if got := string(out); got != "[2,1,0]" {
		t.Errorf("the ends of three turns count %s calls, expected [2,1,0]: a thought is no call, "+
			"and a turn counts from the end of the one before it", got)
	}
}
