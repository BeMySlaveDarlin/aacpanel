package check

import (
	"strings"
	"testing"
)

type handedOver struct {
	ID    string `json:"id"`
	Notes int    `json:"notes"`
	Puts  int    `json:"puts"`
}

type repoNotesWideShot struct {
	SendDisabledEmpty bool         `json:"sendDisabledEmpty"`
	EmptyHint         string       `json:"emptyHint"`
	TreeGoneOnNotes   bool         `json:"treeGoneOnNotes"`
	BoxInTheCode      bool         `json:"boxInTheCode"`
	NotedKinds        []string     `json:"notedKinds"`
	NotedCount        int          `json:"notedCount"`
	Listed            []string     `json:"listed"`
	TabCounts         string       `json:"tabCounts"`
	SendReads         string       `json:"sendReads"`
	SendDisabledFull  bool         `json:"sendDisabledFull"`
	Handed            []handedOver `json:"handed"`
	PutsBefore        int          `json:"putsBefore"`
	SentLabel         string       `json:"sentLabel"`
	SentDisabled      bool         `json:"sentDisabled"`
	CrossesAfterSend  int          `json:"crossesAfterSend"`
	SaidSent          string       `json:"saidSent"`
	ReadingsNamed     int          `json:"readingsNamed"`
	FirstStillSent    int          `json:"firstStillSent"`
}

// Notes replace the directory in the sidebar and leave the code in view.
func TestRepoNotesStandBesideTheCode(t *testing.T) {
	var got repoNotesWideShot
	runWideFixture(t, "reponoteswide.html", &got)
	if !got.TreeGoneOnNotes {
		t.Error("the directory is still drawn under the notes")
	}
	if !got.BoxInTheCode {
		t.Error("the note editor did not open beside its line")
	}
	if !strings.Contains(got.TabCounts, "1") {
		t.Errorf("the Notes button reads %q", got.TabCounts)
	}
}

// A note goes to the line it was written on and to no other. In a diff the
// line that went out and the line that replaced it wear the same number, and a
// note pinned by the number alone marks both — which of the two it was about
// is then anybody's guess.
func TestRepoANoteMarksTheLineItWasWrittenOnAndNotItsNamesake(t *testing.T) {
	var got repoNotesWideShot
	runWideFixture(t, "reponoteswide.html", &got)

	if got.NotedCount != 1 {
		t.Fatalf("%d lines are marked as noted: %v — one note was written", got.NotedCount, got.NotedKinds)
	}
	if got.NotedKinds[0] != "del" {
		t.Errorf("the mark landed on the %q line — the note was written on the line that went out", got.NotedKinds[0])
	}
	if len(got.Listed) != 1 || !strings.Contains(got.Listed[0], "env.go") {
		t.Errorf("the list holds %v", got.Listed)
	}
}

// The button hands over a reading the panel already holds. An empty reading
// has nothing to hand over, and a reading sent with the last note still in
// flight is a file the session reads without it.
func TestRepoTheReadingIsHandedOverOnlyOnceItIsKept(t *testing.T) {
	var got repoNotesWideShot
	runWideFixture(t, "reponoteswide.html", &got)

	if !got.SendDisabledEmpty {
		t.Error("an empty reading can be sent: there is nothing in it to send")
	}
	if got.EmptyHint == "" {
		t.Error("an empty list says nothing about how a note is left")
	}
	if got.SendDisabledFull {
		t.Error("a reading with a note in it cannot be sent")
	}
	if !strings.Contains(strings.ToLower(got.SendReads), "send") {
		t.Errorf("the button reads %q", got.SendReads)
	}
	if len(got.Handed) != 1 {
		t.Fatalf("the conversation was handed %d readings — the button was pressed once", len(got.Handed))
	}
	if got.Handed[0].ID == "" {
		t.Error("the reading was handed over without the name the panel keeps it under")
	}
	if got.Handed[0].Notes != 1 {
		t.Errorf("the reading handed over carries %d notes", got.Handed[0].Notes)
	}
	if got.Handed[0].Puts < 1 {
		t.Error("the reading went to the conversation before a single note reached the panel")
	}
}

// A reading that has gone is settled: nothing in it can be edited or taken
// back, because the session is holding the file it was made into. What is
// written afterwards starts the next reading.
func TestRepoASentReadingIsReadOnlyAndTheNextOneStartsFresh(t *testing.T) {
	var got repoNotesWideShot
	runWideFixture(t, "reponoteswide.html", &got)

	if !strings.Contains(strings.ToLower(got.SentLabel), "sent") {
		t.Errorf("after the send the button reads %q", got.SentLabel)
	}
	if !got.SentDisabled {
		t.Error("a reading that has gone can be sent again")
	}
	if got.CrossesAfterSend != 0 {
		t.Errorf("%d notes of a sent reading can still be taken back", got.CrossesAfterSend)
	}
	if !strings.Contains(strings.ToLower(got.SaidSent), "sent") {
		t.Errorf("the list says %q about a reading that has gone", got.SaidSent)
	}
	if got.ReadingsNamed != 2 {
		t.Errorf("%d readings were named: a note written after the send belongs to the next one", got.ReadingsNamed)
	}
	if got.FirstStillSent != 1 {
		t.Errorf("%d readings are marked as sent — what the session was handed has to stay as it was read", got.FirstStillSent)
	}
}
