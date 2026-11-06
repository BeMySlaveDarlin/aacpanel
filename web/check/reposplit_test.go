package check

import "testing"

type repoSplitShot struct {
	SplitOffered      bool   `json:"splitOffered"`
	PickLabel         string `json:"pickLabel"`
	UnifiedRows       int    `json:"unifiedRows"`
	Rows              int    `json:"rows"`
	Unpaired          int    `json:"unpaired"`
	Voids             int    `json:"voids"`
	LastLeft          string `json:"lastLeft"`
	LastRight         string `json:"lastRight"`
	LastOldNumber     string `json:"lastOldNumber"`
	LastNewNumber     string `json:"lastNewNumber"`
	AddOnTheRightOnly bool   `json:"addOnTheRightOnly"`
	DelOnTheLeftOnly  bool   `json:"delOnTheLeftOnly"`
}

// Side by side is offered where there is room for it, and the diff starts in
// one column: two half-width columns of code is neither side read, and the
// view that opens is the one a narrow window can also hold.
func TestSideBySideIsOfferedAndNotForced(t *testing.T) {
	var got repoSplitShot
	runWideFixture(t, "reposplit.html", &got)

	if !got.SplitOffered {
		t.Fatal("there is no way to ask for the two columns on a screen wide enough for them")
	}
	if got.PickLabel != "In one column" {
		t.Errorf("the diff opens as %q — one column is what it should open as", got.PickLabel)
	}
	if got.UnifiedRows == 0 {
		t.Error("the diff drew nothing before the columns were asked for")
	}
}

// The columns are matched up by blocks: every row holds a line of each side,
// and a block one line short of its neighbour leaves an empty place. Two lines
// removed where one was added, twice over, is exactly what walks a column laid
// out by line numbers out of step with its neighbour — by the end of this hunk
// it would be two lines out.
func TestTheColumnsPairUpThroughALopsidedDiff(t *testing.T) {
	var got repoSplitShot
	runWideFixture(t, "reposplit.html", &got)

	if got.Rows == 0 {
		t.Fatal("the side-by-side diff drew no rows at all")
	}
	if got.Unpaired != 0 {
		t.Errorf("%d of %d rows do not hold a half of each side — the columns have come apart", got.Unpaired, got.Rows)
	}
	if got.Voids != 2 {
		t.Errorf("%d empty places where the blocks were lopsided — two blocks each one line short is two", got.Voids)
	}
}

// The last line of the hunk is context: the same line on both sides, in one
// row, carrying the number each side knows it by. It is the line that proves
// the alignment survived the whole hunk and not just its first block.
func TestTheLineThatClosesTheHunkReadsTheSameOnBothSides(t *testing.T) {
	var got repoSplitShot
	runWideFixture(t, "reposplit.html", &got)

	if got.LastLeft != got.LastRight {
		t.Errorf("the hunk closes on %q against %q — the two columns are no longer describing the same place", got.LastLeft, got.LastRight)
	}
	if got.LastOldNumber != "76" || got.LastNewNumber != "74" {
		t.Errorf("the closing line is numbered %q and %q — each side carries the number its own file knows",
			got.LastOldNumber, got.LastNewNumber)
	}
}

// What was removed belongs on the left and what was added on the right. A
// deletion drawn on the right is a diff read backwards.
func TestRemovalsStayLeftAndAdditionsStayRight(t *testing.T) {
	var got repoSplitShot
	runWideFixture(t, "reposplit.html", &got)

	if !got.AddOnTheRightOnly {
		t.Error("an added line is drawn in the left column — the left column is the file as it was")
	}
	if !got.DelOnTheLeftOnly {
		t.Error("a removed line is drawn in the right column — the right column is the file as it is")
	}
}
