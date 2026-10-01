package check

import (
	"math"
	"testing"
)

type commandNotesShot struct {
	Notes []struct {
		Text  string  `json:"text"`
		Cls   string  `json:"cls"`
		Left  float64 `json:"left"`
		Right float64 `json:"right"`
		Lines []struct {
			Left  float64 `json:"left"`
			Right float64 `json:"right"`
		} `json:"lines"`
		Align string `json:"align"`
	} `json:"notes"`
	ColLeft  float64 `json:"colLeft"`
	ColRight float64 `json:"colRight"`
}

// The answer of a local command is one plate whatever its length: centred in
// the column with its lines centred in it. An answer longer than a line of a
// phone wraps and widens its plate to the column; with its lines set from the
// left it would read as a block of text in a frame beside the short answers.
func TestTheAnswerOfACommandIsOnePlateWhateverItsLength(t *testing.T) {
	var got commandNotesShot
	runFixture(t, "commandnotes.html", &got)

	if len(got.Notes) != 3 {
		t.Fatalf("the answers are drawn as %d plates: %+v", len(got.Notes), got.Notes)
	}
	middle := (got.ColLeft + got.ColRight) / 2
	wrapped := false
	for _, n := range got.Notes {
		if n.Cls != "mnote" {
			t.Errorf("the answer %q is drawn as %q", n.Text, n.Cls)
		}
		if off := (n.Left+n.Right)/2 - middle; math.Abs(off) > 1 {
			t.Errorf("the plate of %q stands %.1fpx off the middle of the column", n.Text, off)
		}
		if len(n.Lines) > 1 {
			wrapped = true
		}
		for i, l := range n.Lines {
			if off := (l.Left+l.Right)/2 - middle; math.Abs(off) > 1.5 {
				t.Errorf("line %d of %q stands %.1fpx off the middle of the column (%s): the lines of a plate "+
					"are set from its middle, or a wrapped answer reads as a block of text", i+1, n.Text, off, n.Align)
			}
		}
	}
	if !wrapped {
		t.Errorf("no answer wraps on a phone: the fixture no longer holds the case of a long one")
	}
}
