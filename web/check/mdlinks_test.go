package check

import (
	"strings"
	"testing"
)

// An address in an answer is a link wherever it stands: set in bold, in
// italics, or as a markdown link inside bold. Only code keeps it as text, to
// be copied.
func TestAnAddressIsALinkInBoldAndItalicsToo(t *testing.T) {
	var got struct {
		Links      []string `json:"links"`
		InBold     int      `json:"inBold"`
		InItalic   int      `json:"inItalic"`
		CodeLinked bool     `json:"codeLinked"`
	}
	runFixture(t, "mdlinks.html", &got)
	want := "http://192.168.1.20:8093/board.html,https://b.example/y,https://c.example/z"
	if strings.Join(got.Links, ",") != want {
		t.Errorf("the answer links %v, expected %s", got.Links, want)
	}
	if got.InBold != 2 || got.InItalic != 1 {
		t.Errorf("%d links in bold and %d in italics — an address set in them is dead words", got.InBold, got.InItalic)
	}
	if got.CodeLinked {
		t.Error("an address in code became a link — code is there to be copied")
	}
}
