package check

import (
	"strings"
	"testing"
)

// The group "sent to you" in the artifact list is built from the state of
// the session, and a file a session sent from outside the directory of the
// conversation stands there like any other. The reader holds every file
// against that directory, so such a row is not a button: it says where the
// file lies, the way the card in the feed does, instead of promising an
// opening that ends in a refusal.
func TestASentFileOutsideTheDirectoryInTheListSaysSoInsteadOfOpening(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	body := withoutComments(screenSrc(t, chatFile))
	list := jsBlock(t, chatFile, body, "export function WorkList(")

	group := after(list, "sent.map(")
	if end := strings.Index(group, "sent.length === 0"); end > 0 {
		group = group[:end]
	}
	at := strings.Index(group, "file.outside")
	if at < 0 {
		t.Fatalf("%s: the row of a sent file does not know a file outside the directory — "+
			"the list offers to open what the reader refuses", chatFile)
	}
	button := strings.Index(group, "<button")
	if button < at {
		t.Fatalf("%s: the row of a file outside the directory is drawn after the button — "+
			"the test reads the wrong shape", chatFile)
	}

	outside := group[at:button]
	if !strings.Contains(outside, "outside the conversation directory") {
		t.Errorf("%s: the row of a file outside the directory does not say so — the human "+
			"is left to guess why nothing opens", chatFile)
	}
	for _, mark := range []string{"onClick", "setPick", "crgo"} {
		if strings.Contains(outside, mark) {
			t.Errorf("%s: the row of a file outside the directory carries %s — it still "+
				"promises an opening", chatFile, mark)
		}
	}
	if !strings.Contains(outside, `<div class="wrow doc outside"`) {
		t.Errorf("%s: the row of a file outside the directory is not marked by its class — "+
			"it is styled as a row that opens", chatFile)
	}
	if !strings.Contains(outside, `<span class="wnote">`) {
		t.Errorf("%s: the note under the name is not a note — it reads in the weight of a state", chatFile)
	}

	inside := group[button:]
	if strings.Contains(inside, "wnote") || strings.Contains(inside, "outside") {
		t.Errorf("%s: the row of a file the reader opens carries the note of one it cannot — "+
			"every sent file reads as unreachable", chatFile)
	}
	if !strings.Contains(inside, "setPick({ kind: \"file\", path: file.path") {
		t.Errorf("%s: the row of a file inside the directory no longer opens it", chatFile)
	}
}

func TestTheListRowOfAFileOutsideTheDirectoryGivesNoPress(t *testing.T) {
	css := cssSrc(t)
	if !strings.Contains(css, ".wrow.doc.outside {") {
		t.Fatal("details.css: the list row of a file outside the directory has no rule of its own — " +
			"it looks like a row that opens")
	}
	block := cssBlock(t, css, ".wrow.doc.outside")
	if !strings.Contains(block, "cursor: default") {
		t.Error("details.css: the list row of a file outside the directory keeps the pointer of a button")
	}
	if !strings.Contains(block, "padding:") {
		t.Error("details.css: the list row of a file outside the directory has no inset of its own — " +
			"the rows that are buttons take theirs from the browser, and this one stands out of the column")
	}
	if !strings.Contains(css, ".wrow.doc.outside:active {") {
		t.Error("details.css: the list row of a file outside the directory answers a press like a button")
	}
	if !strings.Contains(css, ".wrow .wnote {") {
		t.Error("work.css: the note under the name has no rule — it runs into the name in the same weight")
	}
}
