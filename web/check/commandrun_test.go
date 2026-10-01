package check

import (
	"math"
	"strings"
	"testing"
)

type commandRunCard struct {
	Command  string  `json:"command"`
	Time     string  `json:"time"`
	Hook     bool    `json:"hook"`
	Said     string  `json:"said"`
	State    string  `json:"state"`
	Running  bool    `json:"running"`
	More     string  `json:"more"`
	Shown    float64 `json:"shown"`
	Line     float64 `json:"line"`
	Failed   bool    `json:"failed"`
	ErrColor string  `json:"errColor"`
	Rich     bool    `json:"rich"`
	Left     float64 `json:"left"`
	Right    float64 `json:"right"`
}

type commandRunShot struct {
	Error string `json:"error"`
	Col   struct {
		Left  float64 `json:"left"`
		Right float64 `json:"right"`
	} `json:"col"`
	Cards        []commandRunCard `json:"cards"`
	Notes        []string         `json:"notes"`
	CritColor    string           `json:"critColor"`
	McpOpen      commandRunCard   `json:"mcpOpen"`
	McpOpenLines int              `json:"mcpOpenLines"`
	McpFolded    commandRunCard   `json:"mcpFolded"`
	Opened       []string         `json:"opened"`
}

func (s commandRunShot) card(t *testing.T, command string) commandRunCard {
	t.Helper()
	for _, c := range s.Cards {
		if c.Command == command {
			return c
		}
	}
	t.Fatalf("no card of %q among %d cards", command, len(s.Cards))
	return commandRunCard{}
}

// A local command and its answer are one card: the command a line of its own
// with the time beside it, the answer under it on the mark a terminal hangs an
// answer on. The card is as wide as the column on a phone and on a desk — it
// is no bubble of the person, and the plate in the middle stays the session's.
func TestACommandAndItsAnswerAreOneCard(t *testing.T) {
	for _, run := range []struct {
		screen  string
		fixture func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		var got commandRunShot
		run.fixture(t, "commandrun.html", &got)
		if got.Error != "" {
			t.Fatalf("%s: %s", run.screen, got.Error)
		}
		if len(got.Cards) != 10 {
			t.Fatalf("%s: %d cards, want 10", run.screen, len(got.Cards))
		}
		plugins := got.card(t, "/reload-plugins")
		if !plugins.Hook || plugins.Said != "Reloaded: 10 plugins · 30 skills · 14 agents · 3 hooks · 1 plugin LSP server" {
			t.Errorf("%s: the answer of /reload-plugins reads %q under the hook %v", run.screen, plugins.Said, plugins.Hook)
		}
		if !strings.Contains(plugins.Time, ":") {
			t.Errorf("%s: the command says no time: %q", run.screen, plugins.Time)
		}
		if plugins.More != "" {
			t.Errorf("%s: an answer of a line folds behind %q", run.screen, plugins.More)
		}
		for _, c := range got.Cards {
			if math.Abs(c.Left-got.Col.Left) > 1 || math.Abs(c.Right-got.Col.Right) > 1 {
				t.Errorf("%s: the card of %q stands %.0f–%.0f in a column of %.0f–%.0f: it is drawn as a bubble",
					run.screen, c.Command, c.Left, c.Right, got.Col.Left, got.Col.Right)
			}
		}
		if len(got.Notes) != 1 || !strings.HasPrefix(got.Notes[0], "the context was compacted") {
			t.Errorf("%s: the plates in the middle are %q: they are the session's own, a compaction and no answer", run.screen, got.Notes)
		}
	}
}

// Without an answer a card says where the command stands, in the place the
// answer takes: waiting in the queue, taken back from it, or running.
func TestACommandWithoutAnAnswerSaysWhereItStands(t *testing.T) {
	var got commandRunShot
	runFixture(t, "commandrun.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	for _, c := range []struct {
		command, state string
		running        bool
	}{{"/reload-skills", "running", true}, {"/model fable", "queued", false}, {"/effort max", "taken back — it did not run", false}} {
		card := got.card(t, c.command)
		if card.State != c.state || card.Running != c.running || card.Said != "" {
			t.Errorf("%s says %q (running %v) and %q, want %q", c.command, card.State, card.Running, card.Said, c.state)
		}
	}
	if clear := got.card(t, "/clear"); clear.State != "done, nothing printed" {
		t.Errorf("a command that answered with nothing says %q", clear.State)
	}
}

// An answer longer than three lines is folded to them, and the row under it
// says how many more lines there are and opens them, the lines as the command
// printed them; an answer of one long paragraph folds by the lines the screen
// sets it in.
func TestALongAnswerFoldsToThreeLines(t *testing.T) {
	for _, run := range []struct {
		screen  string
		fixture func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		var got commandRunShot
		run.fixture(t, "commandrun.html", &got)
		if got.Error != "" {
			t.Fatalf("%s: %s", run.screen, got.Error)
		}
		mcp := got.card(t, "/mcp")
		if lines := mcp.Shown / mcp.Line; math.Abs(lines-3) > 0.1 {
			t.Errorf("%s: the folded list of servers shows %.1f lines", run.screen, lines)
		}
		if mcp.More != "5 more lines" {
			t.Errorf("%s: the row under the folded list says %q", run.screen, mcp.More)
		}
		if strings.Count(got.McpOpen.Said, "\n") != 7 || got.McpOpenLines < 8 {
			t.Errorf("%s: the opened list is %d lines on the screen from %d breaks: the lines of the answer ran together",
				run.screen, got.McpOpenLines, strings.Count(got.McpOpen.Said, "\n"))
		}
		if got.McpOpen.More != "fold" || got.McpOpen.Shown < 8*got.McpOpen.Line-1 {
			t.Errorf("%s: opened, the list is %.0fpx under a row saying %q", run.screen, got.McpOpen.Shown, got.McpOpen.More)
		}
		if got.McpFolded.More != "5 more lines" || math.Abs(got.McpFolded.Shown/got.McpFolded.Line-3) > 0.1 {
			t.Errorf("%s: folded again, the list shows %.0fpx under %q", run.screen, got.McpFolded.Shown, got.McpFolded.More)
		}
	}
	var phone commandRunShot
	runFixture(t, "commandrun.html", &phone)
	recap := phone.card(t, "/recap")
	if !strings.HasSuffix(recap.More, "more lines") && !strings.HasSuffix(recap.More, "more line") {
		t.Errorf("on a phone an answer of one long paragraph does not fold: %q", recap.More)
	}
}

// An error of a command is in the critical colour and marks its card; an
// answer whose command the feed never saw is the answer alone; the card of an
// answer the feed knows stands under the line of its command and opens its
// breakdown.
func TestTheAnswersOfCommandsReadAsTheyAre(t *testing.T) {
	var got commandRunShot
	runFixture(t, "commandrun.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	compact := got.card(t, "/compact")
	if !compact.Failed || compact.ErrColor != got.CritColor || compact.Said != "Error during compaction: API Error: 529" {
		t.Errorf("the error of /compact is drawn as %q in %q (failed %v), the critical colour is %q",
			compact.Said, compact.ErrColor, compact.Failed, got.CritColor)
	}
	alone := got.card(t, "")
	if alone.Hook || alone.Time != "" || alone.Said != "Set model to Fable 5.1" {
		t.Errorf("an answer without its command is drawn with hook %v and time %q: %q", alone.Hook, alone.Time, alone.Said)
	}
	context := got.card(t, "/context")
	if !context.Rich || context.Hook {
		t.Errorf("the answer of /context is a card %v under the hook %v", context.Rich, context.Hook)
	}
	if len(got.Opened) != 1 || got.Opened[0] != "context" {
		t.Errorf("the card of /context opened %v", got.Opened)
	}
}

// A command the person typed into the composer as a message comes back as a
// card of a command, and the row of the page waiting for it settles on it.
func TestATypedCommandSettlesItsLocalRow(t *testing.T) {
	card := map[string]any{"role": "command", "text": "/simplify", "pos": 9}
	got := runModuleJS(t, "src/screens/chat/feed.js", "arrived", [][]any{
		{[]any{card}, map[string]any{"text": "/simplify", "sent": "/simplify", "after": 1}},
		{[]any{card}, map[string]any{"text": "/simplify", "sent": "/simplify", "after": 9}},
		{[]any{map[string]any{"role": "command", "pos": 9, "out": "/simplify"}},
			map[string]any{"text": "/simplify", "sent": "/simplify", "after": 1}},
	})
	want := []bool{true, false, false}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("arrived case %d = %v, expected %v", i, got, want[i])
		}
	}
}
