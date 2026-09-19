package check

import "testing"

type noteDriftShot struct {
	Marked      []int    `json:"marked"`
	Placed      []int    `json:"placed"`
	Stale       []string `json:"stale"`
	NearestLine []int    `json:"nearestLine"`
	FarStale    []string `json:"farStale"`

	OutdatedSaid bool   `json:"outdatedSaid"`
	OutdatedRows int    `json:"outdatedRows"`
	ListedRows   int    `json:"listedRows"`
	CountSaid    string `json:"countSaid"`
}

// The list keeps a note whose line is gone, under a heading that says so. A
// note that simply disappeared would take with it the one thing worth keeping:
// what somebody had to say about the code.
func TestTheListSetsOutdatedNotesApartInsteadOfDroppingThem(t *testing.T) {
	var got noteDriftShot
	runFixture(t, "notedrift.html", &got)

	if !got.OutdatedSaid {
		t.Fatal("the list says nothing about the note whose line is gone")
	}
	if got.OutdatedRows != 1 {
		t.Errorf("%d notes stand under the outdated heading, expected one", got.OutdatedRows)
	}
	if got.ListedRows != 1 {
		t.Errorf("%d notes stand as still anchored, expected one", got.ListedRows)
	}
	if got.CountSaid != "2 notes" {
		t.Errorf("the count says %q — both notes belong to the reading, wherever they stand", got.CountSaid)
	}
}

// A line moves down when something is committed above it. A note pinned to its
// old number would sit on whatever took that number; a note demanding the old
// number back would vanish. It goes to the line that still says what it was
// written about.
func TestANoteFollowsItsLineWhenTheBranchMoves(t *testing.T) {
	var got noteDriftShot
	runFixture(t, "notedrift.html", &got)

	if len(got.Marked) != 1 || got.Marked[0] != 6 {
		t.Errorf("the note is drawn on %v — its line moved from 4 to 6", got.Marked)
	}
	if len(got.Placed) != 1 || got.Placed[0] != 6 {
		t.Errorf("the note was placed on %v", got.Placed)
	}
}

// A line that is gone takes no note with it: the note is set aside as outdated
// rather than dropped onto a stranger or quietly disappearing.
func TestANoteWhoseLineIsGoneIsSetAsideNotLost(t *testing.T) {
	var got noteDriftShot
	runFixture(t, "notedrift.html", &got)

	if len(got.Stale) != 1 || got.Stale[0] != "gone" {
		t.Errorf("the notes set aside are %v — the one whose line is gone should be there alone", got.Stale)
	}
}

// The same line of code twice in a file is ordinary. The note belongs to the
// one nearest where it was written, not to the first one from the top.
func TestANoteGoesToTheNearestLineThatMatchesNotTheFirst(t *testing.T) {
	var got noteDriftShot
	runFixture(t, "notedrift.html", &got)

	if len(got.NearestLine) != 1 || got.NearestLine[0] != 3 {
		t.Errorf("the note went to line %v — line 3 is the nearer of the two that match", got.NearestLine)
	}
}

// Thirty lines away is a different piece of code wearing the same words, and
// calling that the same place would put a remark on something nobody wrote it
// about.
func TestALineFarAwayIsNotTheSameLine(t *testing.T) {
	var got noteDriftShot
	runFixture(t, "notedrift.html", &got)

	if len(got.FarStale) != 1 {
		t.Errorf("a note forty lines from its words was placed anyway: %v", got.FarStale)
	}
}
