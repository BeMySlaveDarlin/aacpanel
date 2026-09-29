package check

import (
	"regexp"
	"strings"
	"testing"
)

type shellCard struct {
	Command string `json:"command"`
	Stands  string `json:"stands"`
	Tone    string `json:"tone"`
	Output  string `json:"output"`
	Failed  string `json:"failed"`
}

type shellSend struct {
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	MessageID string `json:"messageId"`
}

type shellCardShot struct {
	Stream struct {
		Title    string      `json:"title"`
		Going    []shellCard `json:"going"`
		Started  []shellCard `json:"started"`
		Running  []shellCard `json:"running"`
		Ended    []shellCard `json:"ended"`
		MoreText string      `json:"moreText"`
		Whole    struct {
			Open    bool   `json:"open"`
			Command string `json:"command"`
			Lines   int    `json:"lines"`
		} `json:"whole"`
		Again   []shellCard `json:"again"`
		Refused []shellCard `json:"refused"`
		Sends   []shellSend `json:"sends"`
		Bubbles int         `json:"bubbles"`
	} `json:"stream"`
	Console struct {
		Sends   []shellSend `json:"sends"`
		Cards   int         `json:"cards"`
		Bubbles []string    `json:"bubbles"`
	} `json:"console"`
}

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// "!" in the composer of a session on the stream is a command for its shell,
// as it is at a terminal: it goes out as a command, named for the card that
// follows it, and not as a message the model would take for a request. A
// console takes the line as typed — its composer runs the command itself.
func TestTheBangRunsACommandOnTheStreamAndIsTypedIntoAConsole(t *testing.T) {
	var got shellCardShot
	runFixture(t, "shellcard.html", &got)

	s := got.Stream
	if len(s.Sends) == 0 || s.Sends[0].Kind != "session.shell" || s.Sends[0].Text != "git status" {
		t.Fatalf("the command went out as %+v", s.Sends)
	}
	if !uuidShape.MatchString(s.Sends[0].MessageID) {
		t.Errorf("the command went without an id for its card to be followed by: %q", s.Sends[0].MessageID)
	}
	if s.Bubbles != 0 {
		t.Errorf("the command stands in the feed as %d message bubbles", s.Bubbles)
	}
	if !strings.Contains(s.Title, "session's directory") {
		t.Errorf("the send button says %q over a command", s.Title)
	}

	c := got.Console
	if len(c.Sends) != 1 || c.Sends[0].Kind != "session.send" || c.Sends[0].Text != "! git status" {
		t.Errorf("a session in tmux got %+v rather than the line as typed", c.Sends)
	}
	if c.Cards != 0 || len(c.Bubbles) != 1 {
		t.Errorf("a line to a session in tmux is drawn as %d command cards and bubbles %v", c.Cards, c.Bubbles)
	}
}

// The card of a command says where the command stands at every moment: going
// out, running and for how long, and once the transcript brings its output,
// how it ended and the end of what it printed, with the whole of it a tap away.
func TestTheCommandCardFollowsTheCommand(t *testing.T) {
	var got shellCardShot
	runFixture(t, "shellcard.html", &got)
	s := got.Stream

	one := func(when string, list []shellCard) shellCard {
		t.Helper()
		if len(list) != 1 {
			t.Fatalf("%s the feed has %d command cards: %+v", when, len(list), list)
		}
		return list[0]
	}
	if c := one("while it goes out", s.Going); c.Command != "git status" || c.Stands != "starting" {
		t.Errorf("going out the card reads %+v", c)
	}
	if c := one("once the host took it", s.Started); c.Stands != "running 0:00" || c.Tone != "run" {
		t.Errorf("once taken the card reads %+v", c)
	}
	if c := one("a second on", s.Running); c.Stands != "running 0:01" {
		t.Errorf("the clock of a running command does not go: %+v", c)
	}
	ended := one("once over", s.Ended)
	if ended.Stands != "exit 0" || ended.Tone != "ok" {
		t.Errorf("an ended command reads %+v", ended)
	}
	if !strings.HasPrefix(ended.Output, "… 8 more above") || !strings.HasSuffix(ended.Output, "line 9\nline 10\nline 11\nline 12\nline 13\nline 14") {
		t.Errorf("the card shows the output as %q, not its last lines", ended.Output)
	}
	if s.MoreText != "the whole output · 14 lines" {
		t.Errorf("the way to the whole output reads %q", s.MoreText)
	}
	if !s.Whole.Open || s.Whole.Command != "git status" || s.Whole.Lines != 14 {
		t.Errorf("the sheet of the whole output is %+v", s.Whole)
	}
}

// The same command run again is a run of its own: the first run's end in the
// feed does not end the second. A command the host did not take says why.
func TestARepeatedCommandAndARefusedOneKeepTheirCards(t *testing.T) {
	var got shellCardShot
	runFixture(t, "shellcard.html", &got)
	s := got.Stream

	if len(s.Again) != 2 || s.Again[1].Command != "git status" || !strings.HasPrefix(s.Again[1].Stands, "running") {
		t.Errorf("the second run of a command is drawn as %+v", s.Again)
	}
	last := s.Refused[len(s.Refused)-1]
	if last.Command != "make deploy" || !strings.Contains(last.Failed, "did not start") || !strings.Contains(last.Failed, "closing") {
		t.Errorf("a command the host refused reads %+v", last)
	}
}

// A local row settles against what the transcript wrote after it was sent,
// and only that; a row sent before the feed had anything settles on the first
// echo.
func TestALocalRowSettlesOnlyOnALaterEcho(t *testing.T) {
	first := map[string]any{"role": "shell", "text": "git status", "pos": 60}
	later := map[string]any{"role": "shell", "text": "git status", "pos": 90}
	got := runModuleJS(t, "src/screens/chat/feed.js", "arrived", [][]any{
		{[]any{first}, map[string]any{"text": "git status", "sent": "! git status", "after": 60}},
		{[]any{first, later}, map[string]any{"text": "git status", "sent": "! git status", "after": 60}},
		{[]any{first}, map[string]any{"text": "git status", "sent": "! git status"}},
		{[]any{map[string]any{"role": "me", "text": "ok", "pos": 10}}, map[string]any{"text": "ok", "after": 40}},
	})
	want := []any{false, true, true, false}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("arrived = %v, expected %v", got, want)
		}
	}
	ends := runModuleJS(t, "src/screens/chat/feed.js", "lastPos", [][]any{
		{[]any{map[string]any{"pos": 20}, map[string]any{"pos": 90}, map[string]any{"pos": 40}}},
		{[]any{}},
	})
	if len(ends) != 2 || ends[0] != float64(90) || ends[1] != float64(-1) {
		t.Errorf("lastPos = %v", ends)
	}
}

// What is typed is a command when it starts with "!" and is what follows it.
func TestAShellCommandIsWhatFollowsTheBang(t *testing.T) {
	got := runModuleJS(t, "src/screens/chat/shell.js", "shellOf", [][]any{
		{"! git status"},
		{"  !ls -la  "},
		{"!"},
		{"git status"},
		{"say !this"},
	})
	want := []any{
		map[string]any{"command": "git status"},
		map[string]any{"command": "ls -la"},
		map[string]any{"command": ""},
		nil,
		nil,
	}
	if len(got) != len(want) {
		t.Fatalf("shellOf = %v", got)
	}
	for i := range want {
		if (want[i] == nil) != (got[i] == nil) {
			t.Errorf("shellOf case %d = %v, expected %v", i, got[i], want[i])
			continue
		}
		if want[i] != nil && got[i].(map[string]any)["command"] != want[i].(map[string]any)["command"] {
			t.Errorf("shellOf case %d = %v, expected %v", i, got[i], want[i])
		}
	}
}
