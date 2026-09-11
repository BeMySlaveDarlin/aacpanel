package check

import (
	"strings"
	"testing"
)

// A shell the session has finished stays on its screen: its output is still
// readable, and the screen counts it among the ones the session holds. The feed
// counts it too — otherwise it shows nothing where the session screen shows four.
func TestFinishedShellsStayInTheCount(t *testing.T) {
	src := screenSrc(t, "src/screens/chat/work.js")

	if !strings.Contains(src, `<span class="wnum">${tasks.length}</span>`) {
		t.Error("the chip counts something other than every task: the number in the feed " +
			"parts from the number on the session screen")
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
