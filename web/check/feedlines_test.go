package check

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type feedLinesShot struct {
	Folded struct {
		Again   bool `json:"again"`
		Changed bool `json:"changed"`
	} `json:"folded"`
	Marks []string `json:"marks"`
	Cards []struct {
		Rows int    `json:"rows"`
		Head string `json:"head"`
	} `json:"cards"`
	Tasks []struct {
		Tag   string `json:"tag"`
		Name  string `json:"name"`
		Aside string `json:"aside"`
		Said  string `json:"said"`
		Tone  string `json:"tone"`
		Node  string `json:"node"`
	} `json:"tasks"`
	Turn      string   `json:"turn"`
	TurnNum   string   `json:"turnNum"`
	TurnIcon  bool     `json:"turnIcon"`
	TurnOpens []int    `json:"turnOpens"`
	HookOpens []string `json:"hookOpens"`
	Worked    bool     `json:"worked"`
	Badges    int      `json:"badges"`
	Opened    []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Agent bool   `json:"agent"`
	} `json:"opened"`
	Lines []struct {
		Text  string `json:"text"`
		Level string `json:"level"`
	} `json:"lines"`
}

// What a terminal prints beside the conversation is in the feed as well. A
// hook's message is a badge of its own in the stack of its run on the
// timeline, and the badge opens that message alone; background
// tasks done are cards of the build of the files sent to the person — the
// ones that ended side by side share one card, a row each, opening what the
// task left behind; the end of a turn is a mark on the
// timeline that opens the calls of the turn and says how long it took; a
// warning of claude is a line of its own.
func TestWhatTheTerminalPrintsBesideTheConversationIsInTheFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)

	if strings.Join(got.Marks, " | ") != "1 command — open the calls | 1 hook — open the calls" {
		t.Errorf("the badges of the run: %v", got.Marks)
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
	if agent.Tone != "s-ok" || failed.Tone != "s-crit" {
		t.Errorf("a finished task is marked in the tone %q and a failed one in %q, expected s-ok and s-crit", agent.Tone, failed.Tone)
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
	if !strings.HasPrefix(recap.Text, "while you were away") || recap.Level != "s-info" {
		t.Errorf("the recap does not say what it is: %+v", recap)
	}
	if warn.Text != "Unknown command: /storage" || warn.Level != "s-warn" ||
		!strings.HasSuffix(crit.Text, "The safeguards flagged this message.") || crit.Level != "s-crit" {
		t.Errorf("a warning and a refusal read %+v and %+v, expected each in its own level", warn, crit)
	}
}

type taskCardShot struct {
	Wide  bool `json:"wide"`
	Cards []struct {
		Head string `json:"head"`
		Icon string `json:"icon"`
		Rows []struct {
			Mark string `json:"mark"`
			Tone string `json:"tone"`
		} `json:"rows"`
	} `json:"cards"`
}

// The card of tasks done names their kind in its head and nowhere else: one
// task by its kind and how it ended; several of one kind counted, with the
// ending they share or "ended" when they ended apart; several of different
// kinds by their kinds counted. The icon of the head is the icon of the kind,
// a list only over kinds apart. A row carries no word of the kind — only a
// mark of how the task ended, a tick for one that did its work and a cross
// for one that did not, in the tone of the ending: a failed task apart from
// one stopped. So on a phone as at a desk.
func TestTheCardOfTasksDoneNamesTheKindInItsHead(t *testing.T) {
	parallel(t)
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
		wide bool
	}{
		{"phone", runFixture, false},
		{"desk", runWideFixture, true},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got taskCardShot
			screen.run(t, "taskcard.html", &got)
			if got.Wide != screen.wide {
				t.Fatalf("the fixture drew the feed of a desk %v on the screen of a %s", got.Wide, screen.name)
			}
			checkTaskCard(t, got)
		})
	}
}

func checkTaskCard(t *testing.T, got taskCardShot) {
	t.Helper()
	want := []struct {
		head, marks string
		tones       []string
	}{
		{"agent finished", "✓", []string{"s-ok"}},
		{"1 agent, 1 command", "✓✗", []string{"s-ok", "s-crit"}},
		{"3 agents finished", "✓✓✓", []string{"s-ok", "s-ok", "s-ok"}},
		{"agent stopped", "✗", []string{"s-faint"}},
		{"2 commands ended", "✗✗", []string{"s-crit", "s-faint"}},
		{"1 agent, 1 command, 1 monitor", "✓✓✓", []string{"s-ok", "s-ok", "s-ok"}},
	}
	if len(got.Cards) != len(want) {
		t.Fatalf("%d cards drawn, wanted %d: %+v", len(got.Cards), len(want), got.Cards)
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
		marks := ""
		var tones []string
		for _, row := range card.Rows {
			marks += row.Mark
			tones = append(tones, row.Tone)
		}
		if marks != w.marks {
			t.Errorf("%q: the rows are marked %q, wanted %q: a mark alone, no word of the kind", w.head, marks, w.marks)
		}
		if strings.Join(tones, " ") != strings.Join(w.tones, " ") {
			t.Errorf("%q: the rows end in the tones %v, wanted %v", w.head, tones, w.tones)
		}
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

// Every render folds the feed again — runs wrapped into rows, their calls
// marked with the tasks they started, the end of a turn counting its calls —
// and a row folded again around the same items is the same row: the feed is
// not split, laid out and measured anew, nor its rows drawn again, for a
// snapshot that changed nothing in it. A call that says something else is a
// change.
func TestAFeedFoldedAgainIsTheSameFeed(t *testing.T) {
	var got feedLinesShot
	runFixture(t, "feedlines.html", &got)
	if !got.Folded.Again {
		t.Error("the same items folded twice are not the same rows — every snapshot lays the feed out anew")
	}
	if got.Folded.Changed {
		t.Error("rows folded from a call that says something else are taken for the same — the change is never drawn")
	}
}
