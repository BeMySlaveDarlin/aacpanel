package check

import (
	"strings"
	"testing"
)

// A file the reader does not open — a secret a session sent, say — is drawn
// in the card, since it did reach the human. Its row says so and is not a
// button — a tap that ends in a refusal is a promise the panel cannot keep.
func TestASentFileTheReaderDoesNotOpenSaysSoInsteadOfOpening(t *testing.T) {
	const filesFile = "src/screens/chat/files.js"
	src := srcFiles(t)[filesFile]
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", filesFile)
	}
	card := jsBlock(t, filesFile, withoutComments(src), "export function FileCard(")

	at := strings.Index(card, "file.outside")
	if at < 0 {
		t.Fatalf("%s: the file row does not know a file the reader does not open — the card "+
			"offers to open what the reader refuses", filesFile)
	}
	button := strings.Index(card, "<button")
	if button < at {
		t.Fatalf("%s: the row of a file the reader does not open is drawn after the button — "+
			"the test reads the wrong shape", filesFile)
	}
	outside := card[at:button]
	if !strings.Contains(outside, "the panel does not open it") {
		t.Errorf("%s: the row of a file the reader does not open does not say so — the human "+
			"is left to guess why nothing opens", filesFile)
	}
	for _, mark := range []string{"onClick", "data-path", "onOpen"} {
		if strings.Contains(outside, mark) {
			t.Errorf("%s: the row of a file the reader does not open carries %s — a tap on it "+
				"goes to the reader and ends in a refusal", filesFile, mark)
		}
	}
	if !strings.Contains(outside, `class=${`+"`mfile${tag ? \" tagged\" : \"\"} outside`"+`}`) {
		t.Errorf("%s: the row of a file the reader does not open is not marked by its class — "+
			"it is styled as a row that opens", filesFile)
	}

	inside := card[button:]
	if strings.Contains(inside, "mfnote") || strings.Contains(inside, "outside") {
		t.Errorf("%s: the row of a file the reader opens carries the note of one it cannot — "+
			"every attachment reads as unreachable", filesFile)
	}
}

func TestTheRowOfAFileThatDoesNotOpenGivesNoPress(t *testing.T) {
	css := cssSrc(t)
	if !strings.Contains(css, ".mfile.outside {") {
		t.Fatal("feed.css: the row of a file the reader does not open has no rule of its own — " +
			"it looks like a row that opens")
	}
	if !strings.Contains(cssBlock(t, css, ".mfile.outside"), "cursor: default") {
		t.Error("feed.css: the row of a file the reader does not open keeps the pointer of a button")
	}
	if !strings.Contains(css, ".mfile.outside:active {") {
		t.Error("feed.css: the row of a file the reader does not open answers a press like a button")
	}
	if !strings.Contains(css, ".mfnote {") {
		t.Error("feed.css: the note under the name has no rule — it runs into the name in the same weight")
	}
}
