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
	Turn      string   `json:"turn"`
	TurnNum   string   `json:"turnNum"`
	TurnIcon  bool     `json:"turnIcon"`
	TurnOpens []int    `json:"turnOpens"`
	HookOpens []string `json:"hookOpens"`
	Plates    struct {
		Apart    float64 `json:"apart"`
		Own      bool    `json:"own"`
		Stack    string  `json:"stack"`
		NumRight float64 `json:"numRight"`
		Level    float64 `json:"level"`
	} `json:"plates"`
	Worked bool `json:"worked"`
	Badges int  `json:"badges"`
	Opened []struct {
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
// hook's message is a badge of its own in the stack of its run on the
// timeline, and the badge opens that message alone; background
// tasks done are cards of the build of the files sent to the person — the
// ones that ended side by side share one card, a row each, one width for all,
// opening what the task left behind; the end of a turn is a mark on the
// timeline that opens the calls of the turn and says how long it took; a
// warning of claude is a line of its own.
func TestWhatTheTerminalPrintsBesideTheConversationIsInTheFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)

	if strings.Join(got.Marks, " | ") != "1 command — open the calls | 1 hook — open the calls" {
		t.Errorf("the badges of the run: %v", got.Marks)
	}
	if p := got.Plates; p.Apart < 2 || !p.Own || p.Stack != "none" {
		t.Errorf("the badges of one run are not plates of their own one under another: %.1fpx apart, "+
			"each with its own face %v, a face %q behind them all", p.Apart, p.Own, p.Stack)
	}
	if p := got.Plates; p.NumRight < 0 || p.Level > 1 {
		t.Errorf("the number of a badge stands %.1fpx right of its icon and %.1fpx off its line: "+
			"it belongs to the right of the icon, on the same line", p.NumRight, p.Level)
	}
	if strings.Join(got.HookOpens, " | ") != "PreToolUse:Bash hook" {
		t.Errorf("tapping the badge of the hook opened %v: the calls of its kind alone", got.HookOpens)
	}
	if got.Badges != 0 {
		t.Errorf("%d badges of calls stand in the column: the work belongs on the timeline", got.Badges)
	}
	if len(got.Cards) != 2 || got.Cards[0].Rows != 2 || got.Cards[1].Rows != 1 {
		t.Fatalf("the tasks done are drawn as %+v: the two that ended side by side share a card, the one after a reply has its own", got.Cards)
	}
	if got.Cards[0].Head != "1 agent, 1 command" || got.Cards[1].Head != "command finished" {
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
	if agent.Tag != "✓" || agent.Name != "Commit the notes" || agent.Aside != "24s · 33k tokens" {
		t.Errorf("an agent done reads %q %q %q", agent.Tag, agent.Name, agent.Aside)
	}
	if !strings.Contains(agent.Said, `Agent "Commit the notes" finished`) {
		t.Errorf("the row does not say in words what happened: %q", agent.Said)
	}
	if failed.Tag != "✗" || failed.Name != "make check" || failed.Aside != "exit 2" {
		t.Errorf("a failed command reads %q %q %q", failed.Tag, failed.Name, failed.Aside)
	}
	if failed.TagColour == agent.TagColour {
		t.Errorf("a failed task is marked in the colour of a finished one: %s", failed.TagColour)
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

type taskCardBox struct {
	Left, Right, Top, Bottom float64
}

type taskCardShot struct {
	Wide  bool `json:"wide"`
	Tones struct {
		Ok, Crit, Faint string
	} `json:"tones"`
	Cards []struct {
		Head      string      `json:"head"`
		Icon      string      `json:"icon"`
		HeadLines int         `json:"headLines"`
		Apart     int         `json:"apart"`
		LabelBox  taskCardBox `json:"labelBox"`
		AtBox     taskCardBox `json:"atBox"`
		Box       taskCardBox `json:"box"`
		Rows      []struct {
			Mark       string  `json:"mark"`
			MarkColour string  `json:"markColour"`
			MarkWidth  float64 `json:"markWidth"`
			MarkHeight float64 `json:"markHeight"`
			NameLeft   float64 `json:"nameLeft"`
		} `json:"rows"`
	} `json:"cards"`
}

// The card of tasks done names their kind in its head and nowhere else: one
// task by its kind and how it ended; several of one kind counted, with the
// ending they share or "ended" when they ended apart; several of different
// kinds by their kinds counted. The icon of the head is the icon of the kind,
// a list only over kinds apart. A row carries no word of the kind — only a
// mark of how the task ended, a tick for one that did its work and a cross
// for one that did not, in the tone of the ending; the marks stand in squares
// of one width, so the names of the rows start on one line. The head keeps to
// one line beside its time on a phone as at a desk; only one counting three
// kinds may wrap on a phone, between the kinds and never between a number
// and its kind, and then its time still stands on the card.
func testTheCardOfTasksDoneNamesTheKindInItsHead(t *testing.T, run func(*testing.T, string, any)) {
	var got taskCardShot
	run(t, "taskcard.html", &got)
	want := []struct {
		head   string
		wraps  bool
		marks  string
		colour []string
	}{
		{"agent finished", false, "✓", []string{got.Tones.Ok}},
		{"1 agent, 1 command", false, "✓✗", []string{got.Tones.Ok, got.Tones.Crit}},
		{"3 agents finished", false, "✓✓✓", []string{got.Tones.Ok, got.Tones.Ok, got.Tones.Ok}},
		{"agent stopped", false, "✗", []string{got.Tones.Faint}},
		{"2 commands ended", false, "✗✗", []string{got.Tones.Crit, got.Tones.Faint}},
		{"1 agent, 1 command, 1 monitor", !got.Wide, "✓✓✓", []string{got.Tones.Ok, got.Tones.Ok, got.Tones.Ok}},
	}
	if len(got.Cards) != len(want) {
		t.Fatalf("%d cards drawn, wanted %d: %+v", len(got.Cards), len(want), got.Cards)
	}
	if got.Tones.Ok == got.Tones.Crit || got.Tones.Crit == got.Tones.Faint || got.Tones.Ok == got.Tones.Faint {
		t.Fatalf("the tones of the palette are not told apart: %+v", got.Tones)
	}
	if one, three, mixed := got.Cards[0].Icon, got.Cards[2].Icon, got.Cards[1].Icon; one == "" || three != one || mixed == one {
		t.Errorf("the icon of the head is not the kind: three agents are drawn alike to one %v, "+
			"an agent and a command apart from it %v", three == one, mixed != one)
	}
	for i, w := range want {
		card := got.Cards[i]
		if card.Head != w.head {
			t.Errorf("card %d is headed %q, wanted %q", i, card.Head, w.head)
		}
		if (card.HeadLines != 1 && !w.wraps) || card.LabelBox.Right > card.AtBox.Left+0.5 || card.AtBox.Right > card.Box.Right+0.5 {
			t.Errorf("the head %q takes %d lines and ends at %.1f against its time at %.1f–%.1f, the card ending at %.1f",
				card.Head, card.HeadLines, card.LabelBox.Right, card.AtBox.Left, card.AtBox.Right, card.Box.Right)
		}
		if card.Apart != 0 {
			t.Errorf("the head %q wraps %d numbers away from the kind they count", card.Head, card.Apart)
		}
		marks := ""
		for n, row := range card.Rows {
			marks += row.Mark
			if n < len(w.colour) && row.MarkColour != w.colour[n] {
				t.Errorf("%q: row %d is marked %q in %s, wanted %s", w.head, n, row.Mark, row.MarkColour, w.colour[n])
			}
			if !samePx(row.MarkWidth, row.MarkHeight) {
				t.Errorf("%q: the mark of row %d is %.1f by %.1f, not a square of its own", w.head, n, row.MarkWidth, row.MarkHeight)
			}
			if first := card.Rows[0]; !samePx(row.MarkWidth, first.MarkWidth) || !samePx(row.NameLeft, first.NameLeft) {
				t.Errorf("%q: row %d has a mark %.1f wide and its name at %.1f, the first %.1f and %.1f",
					w.head, n, row.MarkWidth, row.NameLeft, first.MarkWidth, first.NameLeft)
			}
		}
		if marks != w.marks {
			t.Errorf("%q: the rows are marked %q, wanted %q: a mark alone, no word of the kind", w.head, marks, w.marks)
		}
	}
}

func TestTheCardOfTasksDoneNamesTheKindInItsHeadOnAPhone(t *testing.T) {
	testTheCardOfTasksDoneNamesTheKindInItsHead(t, runFixture)
}

func TestTheCardOfTasksDoneNamesTheKindInItsHeadAtADesk(t *testing.T) {
	testTheCardOfTasksDoneNamesTheKindInItsHead(t, runWideFixture)
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
