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
	Tops   []int    `json:"tops"`
	Widths []int    `json:"widths"`
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
	if len(got.Meta) < 1 || !strings.Contains(got.Meta[0], "evirma") {
		t.Errorf("a brief whose session has ended does not name its directory: %v", got.Meta)
	}
}

// On a wide screen the shelf is a grid. A column of full-width rows down the
// middle of a desktop is the shape the screen had when it was called bad.
func TestShelfIsAGridOnAWideScreen(t *testing.T) {
	var got shelfShot
	runWideFixture(t, "briefshelf.html", &got)

	if len(got.Tops) < 4 {
		t.Fatalf("cards drawn: %d", len(got.Tops))
	}
	if got.Tops[0] != got.Tops[1] {
		t.Errorf("the first two cards are not side by side: tops %v", got.Tops)
	}
	if got.Tops[2] <= got.Tops[0] {
		t.Errorf("the shelf fits everything in one row: tops %v", got.Tops)
	}
	// Rows end level: a short title next to a long one must not leave a step.
	if got.Tops[2] != got.Tops[3] {
		t.Errorf("the second row is ragged: tops %v", got.Tops)
	}
}

// On a phone the same shelf is one column: two cards of this width side by side
// would be a title cut into four lines.
func TestShelfIsOneColumnOnAPhone(t *testing.T) {
	var got shelfShot
	runFixture(t, "briefshelf.html", &got)

	if len(got.Tops) < 2 {
		t.Fatalf("cards drawn: %d", len(got.Tops))
	}
	if got.Tops[0] == got.Tops[1] {
		t.Errorf("the phone shelf put two cards in a row: tops %v, widths %v", got.Tops, got.Widths)
	}
}
