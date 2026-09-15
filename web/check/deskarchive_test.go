package check

import (
	"strings"
	"testing"
)

// The archive panel under a real engine.
//
// Two things are held here. A pick is a label of the project map, and the map is
// free to call a contour whatever its owner likes: the archive knows contours by
// the directory they live in, so a label has to travel as the id of its map
// entry — sent as a bare name it matches nothing, and the panel answers with
// somebody else's conversations. And every contour is asked for on its own: one
// page over all of them comes back sorted by the time of the last message, so
// the contour being worked in fills it and the quieter ones are not on screen
// at all.
func TestDesktopArchiveAsksEachContourByTheIDOfItsMapEntry(t *testing.T) {
	var got struct {
		All      []string `json:"all"`
		Labelled []string `json:"labelled"`
		Unknown  []string `json:"unknown"`
		Sections []string `json:"sections"`
	}
	runFixture(t, "deskarchive.html", &got)

	if len(got.All) != 4 {
		t.Fatalf("with nothing picked the panel sent %d requests (%v) — every contour is asked for on its own, "+
			"or the quiet ones never reach the screen", len(got.All), got.All)
	}
	for _, want := range []string{"contour=7", "contour=9", "contour=3", "profile=off-the-map"} {
		if !hasOne(got.All, want) {
			t.Errorf("no request carried %s: %v — that contour has no page of its own in the panel", want, got.All)
		}
	}
	for _, query := range got.All {
		if !strings.Contains(query, "contour=") && !strings.Contains(query, "profile=") {
			t.Errorf("a request names no contour: %q — the collector answers such a request with the personal "+
				"contour alone, and the rest of the archive is invisible", query)
		}
	}

	if want := []string{"Acme Labs", "The Workshop"}; !equalStrings(got.Labelled, want) {
		t.Errorf("with two contours picked the panel shows %v, expected %v — the filter of the archive decides which "+
			"contours have a page here", got.Labelled, want)
	}

	if len(got.Unknown) != 1 || !strings.Contains(got.Unknown[0], "profile=off-the-map") {
		t.Errorf("a contour the map does not know went out as %v — its name came from the collector itself and is the only "+
			"thing that names it", got.Unknown)
	}

	if want := []string{"Acme Labs", "The Workshop", "personal", "off-the-map"}; !equalStrings(got.Sections, want) {
		t.Errorf("the contours stand as %v, expected %v: they keep the order of the map, so a page turned in one of them "+
			"does not reorder the rest", got.Sections, want)
	}
}

func hasOne(list []string, want string) bool {
	for _, item := range list {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
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
