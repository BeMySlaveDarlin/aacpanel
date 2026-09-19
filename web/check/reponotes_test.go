package check

import (
	"strings"
	"testing"
)

type keptNote struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Quote string `json:"quote"`
	Text  string `json:"text"`
}

type repoNotesShot struct {
	BoxUnderTheLine    bool       `json:"boxUnderTheLine"`
	QuotedBack         string     `json:"quotedBack"`
	EmptyRefuses       bool       `json:"emptyRefuses"`
	BoxClosed          bool       `json:"boxClosed"`
	NotedLines         []string   `json:"notedLines"`
	Kept               []keptNote `json:"kept"`
	Started            int        `json:"started"`
	ChipReads          string     `json:"chipReads"`
	Listed             []string   `json:"listed"`
	Trouble            string     `json:"trouble"`
	AfterReload        []string   `json:"afterReload"`
	StartedAfterReload int        `json:"startedAfterReload"`
	AfterRemove        []string   `json:"afterRemove"`
	Dropped            bool       `json:"dropped"`
	ReadingsLeft       int        `json:"readingsLeft"`
}

// A note is left by the number of the line it is about, and the box for it
// opens under that line. A box that stands anywhere else is written into with
// the line out of sight, and what is written from memory is written about the
// wrong line.
func TestANoteIsLeftByTheNumberOfItsLine(t *testing.T) {
	var got repoNotesShot
	runFixture(t, "reponotes.html", &got)

	if !got.BoxUnderTheLine {
		t.Error("the box is not under the line it is about: nothing follows the picked line")
	}
	if !strings.Contains(got.QuotedBack, "childEnv") {
		t.Errorf("the box quotes %q back — the line being written about has to be in it", got.QuotedBack)
	}
	if !got.EmptyRefuses {
		t.Error("a note with nothing written in it can be saved: the service refuses it, and the screen should not offer it")
	}
	if !got.BoxClosed {
		t.Error("the box is still open after the note was saved")
	}
	if len(got.NotedLines) != 1 || strings.TrimSpace(got.NotedLines[0]) != "3" {
		t.Errorf("the lines marked as noted are %v — the note was left on line 3 and on nothing else", got.NotedLines)
	}
}

// The note carries the text of the line it stands on. Without it the service
// refuses the reading outright, and a line number alone stops meaning anything
// the moment somebody commits above it.
func TestANoteCarriesTheLineItStandsOn(t *testing.T) {
	var got repoNotesShot
	runFixture(t, "reponotes.html", &got)

	if len(got.Kept) != 1 {
		t.Fatalf("%d notes reached the panel — one was written", len(got.Kept))
	}
	note := got.Kept[0]
	if note.Quote != "func childEnv() {}" {
		t.Errorf("the note quotes %q: what went out is not the line it was written on", note.Quote)
	}
	if note.Path != "pkg/env.go" || note.Line != 3 {
		t.Errorf("the note stands at %s:%d — it was written on pkg/env.go line 3", note.Path, note.Line)
	}
	if strings.TrimSpace(note.Text) == "" {
		t.Error("the note reached the panel with nothing written in it")
	}
	if got.Trouble != "" {
		t.Errorf("the reading did not reach the panel: %s", got.Trouble)
	}
	if got.Started != 1 {
		t.Errorf("the panel was asked for %d readings — the first note starts one, and one only", got.Started)
	}
}

// The list of the reading is one chip along from the run of changes, and it
// says how much is in it without being opened.
func TestTheNotesAreAChipAlongFromTheChanges(t *testing.T) {
	var got repoNotesShot
	runFixture(t, "reponotes.html", &got)

	if !strings.Contains(got.ChipReads, "Notes") || !strings.Contains(got.ChipReads, "1") {
		t.Errorf("the chip reads %q — it names the list and counts what is in it", got.ChipReads)
	}
	if len(got.Listed) != 1 {
		t.Fatalf("the list holds %v — one note was written", got.Listed)
	}
	if !strings.Contains(got.Listed[0], "env.go") || !strings.Contains(got.Listed[0], ":3") {
		t.Errorf("the note reads %q — a list that does not say where a note stands is read with a second hand", got.Listed[0])
	}
}

// A reload throws the tab away, and the reading has to survive it: the notes
// live in the panel, not in the page. And it has to be the same reading — a
// second one started beside the first is a shelf of halves.
func TestTheReadingOutlivesTheTab(t *testing.T) {
	var got repoNotesShot
	runFixture(t, "reponotes.html", &got)

	if len(got.AfterReload) != 1 {
		t.Fatalf("after the reload the list holds %v — what was written was lost with the tab", got.AfterReload)
	}
	if !strings.Contains(got.AfterReload[0], "env.go") {
		t.Errorf("after the reload the note reads %q", got.AfterReload[0])
	}
	if got.StartedAfterReload != 1 {
		t.Errorf("%d readings were started: the reload picked up the one in hand rather than opening another", got.StartedAfterReload)
	}
}

// The last note taken back puts the reading down. A reading with nothing
// written on it is a name on a shelf saying a review happened when none did.
func TestTheLastNoteTakenBackPutsTheReadingDown(t *testing.T) {
	var got repoNotesShot
	runFixture(t, "reponotes.html", &got)

	if len(got.AfterRemove) != 0 {
		t.Errorf("the note is still listed after it was taken back: %v", got.AfterRemove)
	}
	if !got.Dropped {
		t.Error("the empty reading was left on the shelf instead of being put down")
	}
	if got.ReadingsLeft != 0 {
		t.Errorf("%d readings are still kept by the panel", got.ReadingsLeft)
	}
}
