package check

import (
	"reflect"
	"strings"
	"testing"
)

type blocksShot struct {
	OneClass string
	Copies   [][]string
	List     struct {
		Buttons int
		Wrapped bool
	}
	Bars int
}

// A block of code and a table carry a copy button each, with no bar over them
// and no wrapper of its own for the button; a block of one line is a command
// to run and is told apart from a block of several. A list is prose: it
// carries no copy button and stands in the message itself.
func TestTheBlocksOfAnAnswerCarryTheirCopyButtons(t *testing.T) {
	var got blocksShot
	runFixture(t, "feedblocks.html", &got)
	if got.Bars != 0 {
		t.Errorf("%d bars and wrappers of a copy button stand in the answer", got.Bars)
	}
	want := [][]string{{"Copy the code"}, {"Copy the code"}, {"Copy the table"}}
	if !reflect.DeepEqual(got.Copies, want) {
		t.Errorf("the command, the block and the table carry the copy buttons %q, expected %q", got.Copies, want)
	}
	if !strings.Contains(got.OneClass, "oneline") {
		t.Errorf("the command of one line is not told from a block: %q", got.OneClass)
	}
	if got.List.Buttons != 0 || !got.List.Wrapped {
		t.Errorf("a list carries %d copy buttons and stands in a wrapper %v: a list is prose", got.List.Buttons, !got.List.Wrapped)
	}
}
