package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// The calls a turn waits on, as a holder keeps them off claude's task_started.
func foreground() []stream.Task {
	return []stream.Task{
		{ID: "b7x2k9q1", Type: "local_bash", Description: "Wait thirty seconds", ToolUseID: "toolu_01Fg"},
		{ID: "a4f1c2d3", Type: "local_agent", Description: "probe poem", ToolUseID: "toolu_01Ag"},
	}
}

// movesTheForeground takes the calls a background_tasks request names off the
// foreground, as claude does.
func movesTheForeground() func(*stream.State, map[string]any) {
	return func(st *stream.State, fields map[string]any) {
		kept := []stream.Task{}
		for _, t := range st.Foreground {
			if use, named := fields["tool_use_id"]; named && t.ToolUseID != use {
				kept = append(kept, t)
			}
		}
		st.Foreground = kept
	}
}

// The answer a holder passes on, as claude writes it.
func backgrounded(moved bool) string {
	body, _ := json.Marshal(map[string]any{"subtype": "success", "request_id": "panel-3",
		"response": map[string]any{"backgrounded": moved}})
	return string(body)
}

func TestACallOnTheStreamGoesToTheBackgroundByItsUse(t *testing.T) {
	f := onTheStream(t, true)
	f.state.Foreground = foreground()
	f.answers = map[string]string{"background_tasks": backgrounded(true)}
	e, _ := newTest(t, "")
	r := req(action.SessionBackground, "demo")
	r.Use = "toolu_01Ag"
	out, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	if got.Op != stream.OpControl || got.Subtype != "background_tasks" ||
		len(got.Fields) != 1 || got.Fields["tool_use_id"] != "toolu_01Ag" {
		t.Fatalf("the holder was asked %+v", got)
	}
	if !strings.Contains(out, `subagent "probe poem"`) || !strings.Contains(out, "background") {
		t.Errorf("the report does not name what moved: %q", out)
	}
}

// Claude says a named call was not moved when it is not in the foreground any
// more: the call ended, or went by itself. That is no move to report.
func TestACallClaudeDidNotMoveIsNotReportedMoved(t *testing.T) {
	f := onTheStream(t, true)
	f.answers = map[string]string{"background_tasks": backgrounded(false)}
	e, _ := newTest(t, "")
	r := req(action.SessionBackground, "demo")
	r.Use = "toolu_01Fg"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "no longer runs") {
		t.Fatalf("a call claude did not move: %v", err)
	}
}

// With no call named, every call in the foreground goes, and the move counts
// once they have left it: claude answers such a request with nothing.
func TestEveryCallOnTheStreamGoesWhenNoneIsNamed(t *testing.T) {
	f := onTheStream(t, true)
	f.state.Foreground = foreground()
	f.answers = map[string]string{"background_tasks": `{"subtype":"success","request_id":"panel-3","response":{}}`}
	f.effects = map[string]func(*stream.State, map[string]any){"background_tasks": movesTheForeground()}
	e, _ := newTest(t, "")
	out, err := e.Execute(context.Background(), req(action.SessionBackground, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); got.Subtype != "background_tasks" || len(got.Fields) != 0 {
		t.Fatalf("the holder was asked %+v", got)
	}
	if !strings.Contains(out, "2 calls moved") {
		t.Errorf("the report does not count what moved: %q", out)
	}
}

func TestCallsThatStayInTheForegroundAreNotReportedMoved(t *testing.T) {
	f := onTheStream(t, true)
	f.state.Foreground = foreground()
	e, _ := newTest(t, "")
	if _, err := e.Execute(context.Background(), req(action.SessionBackground, "demo")); err == nil ||
		!strings.Contains(err.Error(), "stayed there") {
		t.Fatalf("calls that stayed in the foreground: %v", err)
	}
}

// A call that stays while the rest went is said, not taken for a failure of
// the move: a call a subagent makes goes with the subagent.
func TestACallThatStaysIsSaidBesideTheOnesThatWent(t *testing.T) {
	f := onTheStream(t, true)
	f.state.Foreground = append(foreground(),
		stream.Task{ID: "b9inner", Type: "local_bash", Description: "the subagent's own", ToolUseID: "toolu_01In"})
	f.effects = map[string]func(*stream.State, map[string]any){"background_tasks": func(st *stream.State, _ map[string]any) {
		st.Foreground = st.Foreground[2:]
	}}
	e, _ := newTest(t, "")
	out, err := e.Execute(context.Background(), req(action.SessionBackground, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2 calls of 3 moved") || !strings.Contains(out, "1 still in the foreground") {
		t.Errorf("the report does not say what stayed: %q", out)
	}
}

func TestNothingInTheForegroundIsSaidSo(t *testing.T) {
	onTheStream(t, false)
	e, _ := newTest(t, "")
	if _, err := e.Execute(context.Background(), req(action.SessionBackground, "demo")); err == nil ||
		!strings.Contains(err.Error(), "nothing ran in the foreground") {
		t.Fatalf("a move with nothing in the foreground: %v", err)
	}
}

// A holder keeps the program it was started with for the life of its session,
// and one started before the request refuses it in its own words: the person
// is told what to do about it instead.
func TestAMoveToAnOlderHolderSaysToRestartTheSession(t *testing.T) {
	f := onTheStream(t, true)
	f.fails = map[string]string{stream.OpControl: `"background_tasks" is not a request the panel passes on`}
	e, _ := newTest(t, "")
	_, err := e.Execute(context.Background(), req(action.SessionBackground, "demo"))
	if err == nil || !strings.Contains(err.Error(), "restart the session") ||
		!strings.Contains(err.Error(), "conversation is kept") || strings.Contains(err.Error(), "passes on") {
		t.Fatalf("an older holder's refusal reached the person as %v", err)
	}
}

// In the console one key moves every call at once: one call is not aimed at
// there, and nothing is pressed for it.
func TestOneCallIsNotMovedInTheConsole(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	r := req(action.SessionBackground, "term")
	r.Use = "toolu_01Fg"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "only on the stream") {
		t.Fatalf("one call in the console: %v", err)
	}
}

// hintScreen is a console where claude runs calls in the foreground and shows
// its hint under each, until the key it names comes.
type hintScreen struct {
	kindName string
	hint     string
	calls    int
	blind    bool
	// stays keeps the calls in the foreground whatever is pressed.
	stays bool
	sent  []string
}

func (h *hintScreen) kind() string  { return h.kindName }
func (h *hintScreen) attempts() int { return 1 }

func (h *hintScreen) send(_ context.Context, payload string) error {
	h.sent = append(h.sent, payload)
	if !h.stays {
		h.calls = 0
	}
	return nil
}

func (h *hintScreen) screen(context.Context) (string, bool) {
	if h.blind {
		return "", false
	}
	var b strings.Builder
	b.WriteString("● I'll wait for the command to run in the background (ctrl+b to run in background) as asked.\n")
	for i := 0; i < h.calls; i++ {
		b.WriteString("● Bash(sleep 30 && echo woke)\n  ⎿  Running…\n     " + h.hint + "\n")
	}
	b.WriteString("\n✻ Waiting… (esc to interrupt)\n\n❯ \n  ? for shortcuts\n")
	return b.String(), true
}

// The key goes in only under claude's hint, as the hint names it: in tmux
// claude doubles every ctrl+b for the prefix, and the panel, which writes past
// tmux, presses it once.
func TestTheConsoleIsPressedTheKeyItsHintNames(t *testing.T) {
	for _, c := range []struct {
		name, kind, hint, want string
	}{
		{"tmux", "tmux", "(ctrl+b ctrl+b (twice) to run in background)", "\x02"},
		{"a chord in tmux", "tmux", "(ctrl+x ctrl+b ctrl+b to run in background)", "\x18\x02"},
		{"konsole", "konsole", "(ctrl+b to run in background)", "\x02"},
		{"a chord in konsole", "konsole", "(ctrl+x ctrl+b to run in background)", "\x18\x02"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := &hintScreen{kindName: c.kind, hint: c.hint, calls: 2}
			out, err := moveOnScreen(t.Context(), h, "term")
			if err != nil {
				t.Fatal(err)
			}
			if len(h.sent) != 1 || h.sent[0] != c.want {
				t.Fatalf("pressed %q, where the hint names %q", h.sent, c.want)
			}
			if !strings.Contains(out, "2 calls moved") {
				t.Errorf("the report does not count what moved: %q", out)
			}
		})
	}
}

// No hint, no key: the same key into a free composer is a key into the
// conversation. A line of the conversation that quotes the hint is no hint.
func TestNoKeyGoesInWithoutTheHint(t *testing.T) {
	for _, h := range []*hintScreen{
		{kindName: "tmux", hint: "(ctrl+b ctrl+b (twice) to run in background)"},
		{kindName: "tmux", hint: "(ctrl+b to run in background)", calls: 1, blind: true},
		{kindName: "tmux", hint: "(alt+b to run in background)", calls: 1},
	} {
		if _, err := moveOnScreen(t.Context(), h, "term"); err == nil {
			t.Errorf("a move with %d calls on a screen blind %v, hint %q was reported", h.calls, h.blind, h.hint)
		}
		if len(h.sent) != 0 {
			t.Errorf("keys went in blind: %q", h.sent)
		}
	}
}

func TestAHintThatStaysIsNotReportedMoved(t *testing.T) {
	h := &hintScreen{kindName: "tmux", hint: "(ctrl+b ctrl+b (twice) to run in background)", calls: 1, stays: true}
	if _, err := moveOnScreen(t.Context(), h, "term"); err == nil || !strings.Contains(err.Error(), "not moved") {
		t.Fatalf("a call that stayed in the foreground: %v", err)
	}
}
