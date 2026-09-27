package check

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type feedLinesShot struct {
	Marks   []string `json:"marks"`
	ColLeft float64  `json:"colLeft"`
	Cards   []struct {
		Rows  int     `json:"rows"`
		Head  string  `json:"head"`
		Left  float64 `json:"left"`
		Width float64 `json:"width"`
	} `json:"cards"`
	Tasks []struct {
		Tag       string `json:"tag"`
		Name      string `json:"name"`
		Aside     string `json:"aside"`
		Said      string `json:"said"`
		TagColour string `json:"tagColour"`
		Node      string `json:"node"`
	} `json:"tasks"`
	Turn      string `json:"turn"`
	TurnNum   string `json:"turnNum"`
	TurnIcon  bool   `json:"turnIcon"`
	TurnOpens []int  `json:"turnOpens"`
	Worked    bool   `json:"worked"`
	Badges    int    `json:"badges"`
	Opened    []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Agent bool   `json:"agent"`
	} `json:"opened"`
	Lines []struct {
		Text   string `json:"text"`
		Dot    string `json:"dot"`
		Colour string `json:"colour"`
	} `json:"lines"`
}

// What a terminal prints beside the conversation is in the feed as well. A
// hook's message counts on the mark of its run on the timeline; background
// tasks done are cards of the build of the files sent to the person — the
// ones that ended side by side share one card, a row each, one width for all,
// opening what the task left behind; the end of a turn is a mark on the
// timeline that opens the calls of the turn and says how long it took; a
// warning of claude is a line of its own.
func TestWhatTheTerminalPrintsBesideTheConversationIsInTheFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)

	if strings.Join(got.Marks, " | ") != "1 command · 1 hook — open the calls" {
		t.Errorf("the marks of the run: %v", got.Marks)
	}
	if got.Badges != 0 {
		t.Errorf("%d badges of calls stand in the column: the work belongs on the timeline", got.Badges)
	}
	if len(got.Cards) != 2 || got.Cards[0].Rows != 2 || got.Cards[1].Rows != 1 {
		t.Fatalf("the tasks done are drawn as %+v: the two that ended side by side share a card, the one after a reply has its own", got.Cards)
	}
	if got.Cards[0].Head != "2 tasks ended" || got.Cards[1].Head != "command finished" {
		t.Errorf("the cards are headed %q and %q", got.Cards[0].Head, got.Cards[1].Head)
	}
	for i, c := range got.Cards {
		if c.Left-got.ColLeft > 1 || c.Width != got.Cards[0].Width {
			t.Errorf("card %d starts at %.1f (the column at %.1f) and is %.1f wide against %.1f", i, c.Left, got.ColLeft, c.Width, got.Cards[0].Width)
		}
	}
	if len(got.Tasks) != 3 {
		t.Fatalf("task rows drawn: %+v", got.Tasks)
	}
	agent, failed := got.Tasks[0], got.Tasks[1]
	if agent.Tag != "✓ AGENT" || agent.Name != "Commit the notes" || agent.Aside != "24s · 33k tokens" {
		t.Errorf("an agent done reads %q %q %q", agent.Tag, agent.Name, agent.Aside)
	}
	if !strings.Contains(agent.Said, `Agent "Commit the notes" finished`) {
		t.Errorf("the row does not say in words what happened: %q", agent.Said)
	}
	if failed.Tag != "✗ BASH" || failed.Name != "make check" || failed.Aside != "exit 2" {
		t.Errorf("a failed command reads %q %q %q", failed.Tag, failed.Name, failed.Aside)
	}
	if failed.TagColour == agent.TagColour {
		t.Errorf("a failed task is tagged in the colour of a finished one: %s", failed.TagColour)
	}
	nodes := []string{}
	for _, task := range got.Tasks {
		nodes = append(nodes, task.Node)
	}
	if strings.Join(nodes, ",") != "BUTTON,BUTTON,DIV" {
		t.Errorf("the rows are %v: a task that names itself opens, one that does not is not a button", nodes)
	}
	if len(got.Opened) != 2 || got.Opened[0].ID != "a515a204cce27a85c" || !got.Opened[0].Agent ||
		got.Opened[1].ID != "bg3me3whb" || got.Opened[1].Agent || got.Opened[1].Name != "make check" {
		t.Errorf("tapping the rows opened %+v: the agent its conversation, the command its output", got.Opened)
	}
	if got.Worked {
		t.Errorf("the length of a turn is printed in the column: it belongs to the timeline and the calls it opens")
	}
	if !got.TurnIcon || got.TurnNum != "2" {
		t.Errorf("the mark of the turn is not an hourglass with the calls it opens: icon %v, number %q — "+
			"a number on a mark is the calls behind it, as on every mark beside it", got.TurnIcon, got.TurnNum)
	}
	if !strings.Contains(got.Turn, "worked 2m 49s") || !strings.Contains(got.Turn, "2 calls") ||
		!strings.Contains(got.Turn, "3 background agents were still at work") {
		t.Errorf("the mark of the turn says %q: how long it took, the calls and what it left at work", got.Turn)
	}
	if len(got.TurnOpens) != 1 || got.TurnOpens[0] != 7 {
		t.Errorf("tapping the mark of the turn opened %v: the calls of that turn", got.TurnOpens)
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
