package check

import (
	"strings"
	"testing"
)

// A shell the session has finished stays in the list: its output is still
// readable there. The number on the chip does not count it — the chip says how
// many still run, and over nothing running it carries no number at all: a 12
// over twelve finished shells reads as twelve at work, and a 0 is a number where
// the eye expects none.
func TestFinishedShellsStayInTheListNotOnTheChip(t *testing.T) {
	src := screenSrc(t, "src/screens/chat/work.js")

	if strings.Contains(src, `<span class="wnum">${tasks.length}</span>`) {
		t.Error("the chip counts every task, finished ones included: the number reads as " +
			"how many are at work")
	}
	if !strings.Contains(src, "liveTasks.length > 0 && html`<span class=\"wnum\">${liveTasks.length}</span>`") {
		t.Error("the number on the task chip is not tied to the running ones, or a 0 is drawn " +
			"where there should be no number")
	}
	if !strings.Contains(src, "function running(tasks)") ||
		!strings.Contains(src, "tasks.filter((task) => !task.done)") {
		t.Error("nothing tells a shell in flight from one that is over, so the feed cannot " +
			"say how many of the shells still run")
	}
	if !strings.Contains(src, `knows(exec, "task.stop") && Boolean(task && task.line) && !task.done`) {
		t.Error("stopping is offered for work that is over — the button asks the executor to " +
			"kill what nobody runs")
	}
	if !strings.Contains(src, `<span class="wkind gone">over</span>`) {
		t.Error("a finished shell is not marked in the list: it is indistinguishable from a running one")
	}
}

type subShellShot struct {
	Chip  string   `json:"chip"`
	Sub   string   `json:"sub"`
	Whose []string `json:"whose"`
	Rows  int      `json:"rows"`
}

// A shell an agent of the session sent to the background is the session's work:
// its screen holds it and its stop button reaches it. Several agents waiting on
// the same thing produce rows that read identically, so the row names the agent
// it came from — otherwise one wait is shown three times as far as the reader
// can tell, and there is no telling which of them to stop.
func TestAShellOfAnAgentSaysWhoseItIs(t *testing.T) {
	var got subShellShot
	runFixture(t, "subshell.html", &got)

	if got.Rows != 3 {
		t.Fatalf("the list drew %d rows over three waits", got.Rows)
	}
	if got.Chip != "3" {
		t.Errorf("the chip counts %q of the three waits: the work of an agent is the "+
			"work of the session", got.Chip)
	}
	if got.Whose[0] != "" {
		t.Errorf("a shell of the session itself is attributed to %q", got.Whose[0])
	}
	if got.Whose[1] != "zone-a" || got.Whose[2] != "zone-o" {
		t.Errorf("the rows of the agents name %q and %q", got.Whose[1], got.Whose[2])
	}
}
