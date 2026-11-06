package check

import (
	"strings"
	"testing"
)

type shelfShot struct {
	Head   string   `json:"head"`
	Titles []string `json:"titles"`
	States []string `json:"states"`
	Live   []string `json:"live"`
	Meta   []string `json:"meta"`
	Bars   []int    `json:"bars"`
}

// A shelf is looked at before it is read: the person wants to know which brief
// wants something from them. Every card says how far it has got on one scale,
// so a glance down the column compares like with like.
func TestShelfSaysWhatEachBriefStillWants(t *testing.T) {
	var got shelfShot
	runFixture(t, "briefshelf.html", &got)

	if !strings.Contains(got.Head, "Briefs") {
		t.Errorf("the shelf does not name itself: %q", got.Head)
	}
	if !strings.Contains(got.Head, "ready to send") {
		t.Errorf("the head does not say what is waiting for the person: %q", got.Head)
	}
	want := []string{"0 of 5", "sent", "5 of 5", "3 of 7"}
	if len(got.States) != len(want) {
		t.Fatalf("states drawn: %v", got.States)
	}
	for i, state := range want {
		if got.States[i] != state {
			t.Errorf("card %d says %q, the document is %q", i+1, got.States[i], state)
		}
	}
	// The bar under a card is the same answer as its number, for the eye
	// rather than the reading: nothing, part, full.
	if got.Bars[0] != 0 || got.Bars[2] != 100 || got.Bars[3] < 35 || got.Bars[3] > 50 {
		t.Errorf("the bars do not follow the answers: %v", got.Bars)
	}
}

// The name on a card is where its answers can go: the session while it runs,
// and the directory it worked in once it is gone.
func TestShelfNamesWhoIsStillThere(t *testing.T) {
	var got shelfShot
	runFixture(t, "briefshelf.html", &got)

	if len(got.Live) != 2 {
		t.Errorf("sessions marked as running: %v", got.Live)
	}
	if len(got.Meta) < 1 || !strings.Contains(got.Meta[0], "gateway") {
		t.Errorf("a brief whose session has ended does not name its directory: %v", got.Meta)
	}
}

// The shelf is where a stack of old briefs is put away, so removal lives on it
// and not only inside the document: opening a dozen to close a dozen is not
// putting them away, it is reading them again. It asks once before each.
func TestTheShelfPutsABriefAway(t *testing.T) {
	var got struct {
		Removing struct {
			There       bool     `json:"there"`
			First       string   `json:"first"`
			Gone        int      `json:"gone"`
			Asked       []string `json:"asked"`
			Titles      []string `json:"titles"`
			StillAsking int      `json:"stillAsking"`
			Before      int      `json:"before"`
		} `json:"removing"`
	}
	runFixture(t, "briefshelf.html", &got)

	if !got.Removing.There {
		t.Fatal("the shelf offers no way to put a brief away")
	}
	if !strings.Contains(got.Removing.First, "remove") {
		t.Errorf("the first press says %q instead of asking", got.Removing.First)
	}
	if len(got.Removing.Asked) != 1 {
		t.Fatalf("what was asked of the service: %v", got.Removing.Asked)
	}
	if !strings.Contains(got.Removing.Asked[0], "/api/briefs/") {
		t.Errorf("the removal went to %q", got.Removing.Asked[0])
	}
	if got.Removing.Before == 0 {
		t.Fatal("the shelf drew no cards at all")
	}
	if got.Removing.Gone != got.Removing.Before-1 {
		t.Errorf("%d cards left of %d after one was removed", got.Removing.Gone, got.Removing.Before)
	}
	if got.Removing.StillAsking != 0 {
		t.Error("the question stayed on the screen after it was answered")
	}
}
