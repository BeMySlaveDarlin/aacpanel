package check

import (
	"strings"
	"testing"
)

// The archive panel under a real engine. A pick is a label of the project map,
// and the map is free to call a contour whatever its owner likes: the archive
// knows contours by the directory they live in, so a label has to travel as the
// id of its map entry. Sent as a bare name, it matches nothing on the other
// side and the panel answers with somebody else's conversations.
func TestDesktopArchiveAsksByTheIDOfTheMapEntry(t *testing.T) {
	var got struct {
		None     string   `json:"none"`
		Labelled string   `json:"labelled"`
		Unknown  string   `json:"unknown"`
		Sections []string `json:"sections"`
	}
	runFixture(t, "deskarchive.html", &got)

	if strings.Contains(got.None, "contour=") || strings.Contains(got.None, "profile=") {
		t.Errorf("with nothing picked the panel still names a contour: %q", got.None)
	}

	for _, want := range []string{"contour=7", "contour=9"} {
		if !strings.Contains(got.Labelled, want) {
			t.Errorf("the picked contours went out as %q, without %s — a label of the map reaches the archive as a name "+
				"it has never heard of, and the answer holds conversations of another contour", got.Labelled, want)
		}
	}
	if strings.Contains(got.Labelled, "profile=") {
		t.Errorf("a label of the map went out as a name: %q", got.Labelled)
	}

	if !strings.Contains(got.Unknown, "profile=off-the-map") {
		t.Errorf("a contour the map does not know went out as %q — its name came from the collector itself and is the only "+
			"thing that names it", got.Unknown)
	}

	if want := []string{"acme", "personal"}; !equalStrings(got.Sections, want) {
		t.Errorf("the contours stand as %v, expected %v: the section with the freshest conversation goes first, "+
			"otherwise the count of rows on the page decides the order and it changes from page to page", got.Sections, want)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
