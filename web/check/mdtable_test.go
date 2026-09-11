package check

import (
	"os"
	"strings"
	"testing"
)

func TestFeedTableGetsAScrollbarOnWideScreens(t *testing.T) {
	const mdCSS = "src/css/markdown.css"
	raw, err := os.ReadFile(webPath(mdCSS))
	if err != nil {
		t.Fatalf("markdown styles: %v", err)
	}
	css := cssWithoutComments(string(raw))

	at := strings.Index(css, "@media (min-width: 1100px) {")
	if at < 0 {
		t.Fatalf("%s: there is no wide-screen media query at all — the table scrollbar either "+
			"disappeared or moved to the phone", mdCSS)
	}
	phone, wide := css[:at], css[at:]

	if strings.Contains(phone, "scrollbar") {
		t.Errorf("%s: the scrollbar rule sits outside the media query — on the phone a line "+
			"appears under the table that no other block has", mdCSS)
	}

	block := cssBlock(t, wide, ".deskshell .mdtable")
	if !strings.Contains(block, "scrollbar-width: thin") {
		t.Errorf("%s: the feed table has no `scrollbar-width: thin` — the project-wide rule "+
			"hides the scrollbar again, and on the desktop the right columns are out of reach of the mouse", mdCSS)
	}
	if !strings.Contains(block, "scrollbar-color:") {
		t.Errorf("%s: the table scrollbar has no color of its own — the browser default draws "+
			"a light bar over the dark feed", mdCSS)
	}
}
