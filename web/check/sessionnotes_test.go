package check

import "testing"

type sessionNotesShot struct {
	Notes []struct {
		Text string `json:"text"`
		Cls  string `json:"cls"`
	} `json:"notes"`
}

// A line of the session's own is one plate whatever its length: a compaction
// longer than a line of a phone is drawn as the same note as the short ones.
func TestALineOfTheSessionIsOnePlateWhateverItsLength(t *testing.T) {
	var got sessionNotesShot
	runFixture(t, "sessionnotes.html", &got)

	if len(got.Notes) != 3 {
		t.Fatalf("the lines are drawn as %d plates: %+v", len(got.Notes), got.Notes)
	}
	for _, n := range got.Notes {
		if n.Cls != "mnote" {
			t.Errorf("the line %q is drawn as %q", n.Text, n.Cls)
		}
	}
}
