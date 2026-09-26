package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

type workScreen struct {
	rows  []string
	at    int
	open  bool
	blind bool
	// card opens the list on the card of its first row, the way claude does
	// when there is one piece of work to show.
	card   bool
	carded bool

	sent    []string
	stopped []string
}

func (w *workScreen) kind() string  { return "tmux" }
func (w *workScreen) attempts() int { return 1 }

func (w *workScreen) send(_ context.Context, payload string) error {
	w.sent = append(w.sent, payload)
	switch {
	case strings.Contains(payload, workOpen):
		w.open = true
		w.carded = w.card
	case payload == escKey:
		w.open, w.carded = false, false
	case !w.open:
	case w.carded:
		if payload == workBackKey {
			w.carded = false
		}
	case payload == workDownKey:
		if w.at < len(w.rows)-1 {
			w.at++
		}
	case payload == workUpKey:
		if w.at > 0 {
			w.at--
		}
	case payload == workStopKey:
		if w.at < len(w.rows) {
			w.stopped = append(w.stopped, w.rows[w.at])
			w.rows = append(w.rows[:w.at], w.rows[w.at+1:]...)
			if w.at > len(w.rows)-1 && w.at > 0 {
				w.at = len(w.rows) - 1
			}
		}
	}
	return nil
}

func (w *workScreen) screen(context.Context) (string, bool) {
	if w.blind {
		return "", false
	}
	if !w.open {
		return "❯ an ordinary composer, no list on the screen\n", true
	}
	if w.carded {
		return "   " + w.rows[0] + "\n   2m 2s · 49.8k tokens · haiku\n   Prompt\n   Write an essay.\n" +
			"   ← to go back · Esc/Enter/Space to close · x to stop · f to foreground\n", true
	}
	var agents, shells []string
	for i, row := range w.rows {
		mark := "  "
		if i == w.at {
			mark = "❯ "
		}
		if strings.HasPrefix(row, "@") {
			agents = append(agents, "   "+mark+row)
			continue
		}
		shells = append(shells, "   "+mark+row)
	}
	var b strings.Builder
	b.WriteString("   Background\n")
	b.WriteString(fmt.Sprintf("   %d agents · %d active shells\n\n", len(agents), len(shells)))
	if len(agents) > 0 {
		b.WriteString(fmt.Sprintf("     Agents (%d)\n", len(agents)))
		b.WriteString("     Team: session-4f95269d (2)\n")
		b.WriteString(strings.Join(agents, "\n") + "\n\n")
	}
	if len(shells) > 0 {
		b.WriteString(fmt.Sprintf("     Shells (%d)\n", len(shells)))
		b.WriteString(strings.Join(shells, "\n") + "\n\n")
	}
	b.WriteString("   ↑/↓ to select · Enter to view · x to stop · Esc to close\n")
	return b.String(), true
}

func (w *workScreen) pressed(key string) int {
	n := 0
	for _, s := range w.sent {
		if s == key {
			n++
		}
	}
	return n
}

func TestWorkStopTakesTheNamedRow(t *testing.T) {
	w := &workScreen{rows: []string{
		"@probe-a: idle",
		"npm run build (running)",
		"sleep 400 (running)",
	}}

	if err := openWorkScreen(t.Context(), w); err != nil {
		t.Fatalf("the screen did not open: %v", err)
	}
	if err := aimAndStop(t.Context(), w, "sleep 400"); err != nil {
		t.Fatalf("the work was not stopped: %v", err)
	}
	if want := []string{"sleep 400 (running)"}; !equalRows(w.stopped, want) {
		t.Fatalf("%v was stopped while %v was asked for", w.stopped, want)
	}
	if len(w.rows) != 2 {
		t.Fatalf("%d rows are left in the list instead of two: %v", len(w.rows), w.rows)
	}
	if w.pressed(workStopKey) != 1 {
		t.Errorf("%d presses of x, while one stops the work", w.pressed(workStopKey))
	}
}

func TestWorkStopWalksUpToo(t *testing.T) {
	w := &workScreen{open: true, at: 2, rows: []string{
		"sleep 400 (running)",
		"npm run build (running)",
		"tail -f log (running)",
	}}
	if err := aimAndStop(t.Context(), w, "sleep 400"); err != nil {
		t.Fatalf("the work was not stopped: %v", err)
	}
	if want := []string{"sleep 400 (running)"}; !equalRows(w.stopped, want) {
		t.Fatalf("%v was stopped while %v was asked for", w.stopped, want)
	}
	if w.pressed(workUpKey) == 0 {
		t.Error("nothing moved up at all — which means the target was found by chance")
	}
}

func TestWorkStopRefusesWhenRowIsGone(t *testing.T) {
	w := &workScreen{open: true, rows: []string{"npm run build (running)"}}
	err := aimAndStop(t.Context(), w, "sleep 400")
	if err == nil {
		t.Fatal("stopping work that does not exist was passed off as success")
	}
	if w.pressed(workStopKey) != 0 {
		t.Fatalf("x was pressed with no target around: the work %v of another was stopped", w.stopped)
	}
	if strings.Contains(err.Error(), "npm run build") {
		t.Errorf("the session screen leaked into the refusal: %v", err)
	}
}

func TestWorkStopRefusesTwins(t *testing.T) {
	w := &workScreen{open: true, rows: []string{
		"npm test (running)",
		"sleep 400 (running)",
		"npm test (running)",
	}}
	err := aimAndStop(t.Context(), w, "npm test")
	if err == nil {
		t.Fatal("stopping with two identical rows around was passed off as success")
	}
	if w.pressed(workStopKey) != 0 {
		t.Fatalf("x was pressed with an ambiguous target: %v was stopped", w.stopped)
	}
}

func TestWorkStopMatchesCutRow(t *testing.T) {
	const full = "find /srv/proj -type f -name '*.go' -exec grep -Hn TODO {} ;"
	w := &workScreen{open: true, rows: []string{
		"find /srv/proj -type f -name '*.go' -exec gre… (running)",
	}}
	if err := aimAndStop(t.Context(), w, full); err != nil {
		t.Fatalf("a truncated row went unrecognised: %v", err)
	}
	if len(w.stopped) != 1 {
		t.Fatalf("%v was stopped", w.stopped)
	}
}

func TestWorkStopRefusesBlindScreen(t *testing.T) {
	w := &workScreen{open: true, blind: true, rows: []string{"sleep 400 (running)"}}
	err := aimAndStop(t.Context(), w, "sleep 400")
	if err == nil {
		t.Fatal("a blind stop was passed off as success")
	}
	if !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("the refusal does not name the reason — an unreadable screen: %v", err)
	}
	if w.pressed(workStopKey) != 0 {
		t.Error("x was pressed without seeing the screen")
	}
}

func TestWorkStopWaitsForTheRowToGo(t *testing.T) {
	w := &deafScreen{workScreen{open: true, rows: []string{"sleep 400 (running)"}}}
	err := aimAndStop(t.Context(), w, "sleep 400")
	if err == nil {
		t.Fatal("a swallowed keypress was passed off as stopped work")
	}
}

type deafScreen struct{ workScreen }

func (d *deafScreen) send(ctx context.Context, payload string) error {
	if payload == workStopKey {
		d.sent = append(d.sent, payload)
		return nil
	}
	return d.workScreen.send(ctx, payload)
}

func TestAgentStopLooksForTheAtName(t *testing.T) {
	w := &workScreen{open: true, rows: []string{
		"@probe-a: idle",
		"@probe-b",
		"probe-b (running)",
	}}
	if err := aimAndStop(t.Context(), w, "@probe-b"); err != nil {
		t.Fatalf("the agent was not stopped: %v", err)
	}
	if want := []string{"@probe-b"}; !equalRows(w.stopped, want) {
		t.Fatalf("%v was stopped while %v was asked for — a background command with the same name is not an agent", w.stopped, want)
	}
}

// An agent sent off without a name is a local agent on the screen: its
// description between a mark of its state and the state with the model.
func TestWorkStopFindsALocalAgentByItsDescription(t *testing.T) {
	w := &workScreen{open: true, rows: []string{
		"@team-lead",
		"@lighthouse-essay: idle",
		"● probe poem   running · Haiku 4.5",
	}}
	if err := aimAndStop(t.Context(), w, "probe poem"); err != nil {
		t.Fatalf("the agent was not stopped: %v", err)
	}
	if want := []string{"● probe poem   running · Haiku 4.5"}; !equalRows(w.stopped, want) {
		t.Fatalf("%v was stopped while %v was asked for", w.stopped, want)
	}
}

// With one piece of work running, the list opens on its card; the list is
// one step back from there.
func TestWorkScreenOpensPastTheCardOfTheOnlyWork(t *testing.T) {
	w := &workScreen{card: true, rows: []string{"@lighthouse-essay (working)", "@team-lead"}}
	if err := openWorkScreen(t.Context(), w); err != nil {
		t.Fatalf("the list did not open past the card: %v", err)
	}
	if w.pressed(workBackKey) != 1 || w.carded {
		t.Errorf("the card was left %d times and is still open: %v", w.pressed(workBackKey), w.carded)
	}

	shut := &workScreen{card: true, rows: []string{"@lighthouse-essay (working)"}}
	_ = shut.send(t.Context(), workOpen)
	closeWorkScreen(t.Context(), shut)
	if shut.pressed(escKey) != 1 {
		t.Error("the card left open was not closed")
	}
}

// A console checks the keypress against the line of the task: without one
// there is nothing to aim at.
func TestAConsoleTaskIsNotStoppedWithoutItsLine(t *testing.T) {
	procFS(t,
		fakeProc{pid: 860, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 861, comm: "claude", args: []string{"claude"}, ppid: 860, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 861, name: "aacpanel", start: "77", status: "idle"})

	e := &Executor{}
	_, err := e.taskStop(t.Context(), "aacpanel", &action.Work{ID: "b1"})
	if err == nil || !strings.Contains(err.Error(), "named on the session screen") {
		t.Fatalf("a console task without its line was not refused for it: %v", err)
	}
}

func TestWorkScreenClosesAfterItself(t *testing.T) {
	w := &workScreen{open: true, rows: []string{"sleep 400 (running)"}}
	closeWorkScreen(t.Context(), w)
	if w.pressed(escKey) != 1 {
		t.Errorf("the open list was not closed: Esc was sent %d times", w.pressed(escKey))
	}

	shut := &workScreen{rows: []string{"sleep 400 (running)"}}
	closeWorkScreen(t.Context(), shut)
	if shut.pressed(escKey) != 0 {
		t.Error("Esc went to a session with the list closed — that interrupts the work of the model")
	}
}

func TestWorkRowsSkipSectionHeaders(t *testing.T) {
	w := &workScreen{open: true, rows: []string{"@probe-a", "sleep 400 (running)"}}
	screen, _ := w.screen(t.Context())
	rows := workRows(screen)
	if len(rows) != 2 {
		var got []string
		for _, r := range rows {
			got = append(got, r.name)
		}
		t.Fatalf("%d rows were parsed: %v", len(rows), got)
	}
	if rows[0].name != "@probe-a" || rows[1].name != "sleep 400" {
		t.Fatalf("%q and %q were parsed", rows[0].name, rows[1].name)
	}
	if !rows[0].cursor || rows[1].cursor {
		t.Fatalf("the cursor was parsed in the wrong place: %v, %v", rows[0].cursor, rows[1].cursor)
	}
}

func TestWorkStopNeedsWork(t *testing.T) {
	e := &Executor{}
	if _, err := e.taskStop(t.Context(), "aacpanel", nil); err == nil {
		t.Error("stopping a task without a task went through")
	}
	if _, err := e.agentStop(t.Context(), "aacpanel", nil); err == nil {
		t.Error("stopping an agent without an agent went through")
	}
}

func equalRows(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestWorkStopRefusesBusySession(t *testing.T) {
	procFS(t,
		fakeProc{pid: 860, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 861, comm: "claude", args: []string{"claude"}, ppid: 860, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 861, name: "aacpanel", start: "77", status: "busy"})

	e := &Executor{}
	_, err := e.taskStop(t.Context(), "aacpanel", &action.Work{ID: "b1", Line: "sleep 400"})
	if err == nil {
		t.Fatal("a stop on a busy session was passed off as success")
	}
	if !strings.Contains(err.Error(), "is busy") {
		t.Errorf("the refusal does not name the reason — a busy session: %v", err)
	}
}

// A busy session whose turn is over is busy with its background work: the
// list of that work opens at once, and the stop goes on to the screen. Whether
// the turn is over is the collector's to say.
func TestWorkStopGoesOnWhenTheBusyIsTheBackgroundWork(t *testing.T) {
	procFS(t,
		fakeProc{pid: 860, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 861, comm: "claude", args: []string{"claude"}, ppid: 860, start: "77"},
	)
	sessionFiles(t, fakeSession{pid: 861, name: "aacpanel", start: "77", status: "busy", sid: consoleSID})
	agent := startFakeSeen(t)

	e := &Executor{}
	work := &action.Work{ID: "a1", Line: "probe tale"}
	if _, err := e.taskStop(t.Context(), "aacpanel", work); err == nil || !strings.Contains(err.Error(), "is busy") {
		t.Fatalf("a session in a turn was not refused as busy: %v", err)
	}
	agent.mu.Lock()
	agent.ended = true
	agent.mu.Unlock()
	if _, err := e.taskStop(t.Context(), "aacpanel", work); err == nil || strings.Contains(err.Error(), "is busy") {
		t.Fatalf("a session busy with its background work was refused as busy: %v", err)
	}
}
