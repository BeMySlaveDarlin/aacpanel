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
