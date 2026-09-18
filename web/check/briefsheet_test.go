package check

import (
	"regexp"
	"strings"
	"testing"
)

type sheetShot struct {
	Window      int     `json:"window"`
	Height      int     `json:"height"`
	Exits       int     `json:"exits"`
	ExitKinds   string  `json:"exitKinds"`
	SheetHeight int     `json:"sheetHeight"`
	Root        float64 `json:"root"`
	Win         bool    `json:"win"`
	Doc         bool    `json:"doc"`
	Sheet       int     `json:"sheet"`
	Page        int     `json:"page"`
	Prose       float64 `json:"prose"`
}

// A brief opens as a layer over the conversation, and on a wide screen every
// other layer is a dialog the width of a question. A document in that window
// is a column of four words a line: the sheet that carries one takes the room
// a page of the panel takes.
func TestABriefOverAConversationIsNotDrawnAsADialog(t *testing.T) {
	var got sheetShot
	runWideFixture(t, "briefsheet.html", &got)

	if !got.Win || !got.Doc {
		t.Fatalf("the sheet is drawn as %v/%v (window/document) — the fixture is not testing what it says", got.Win, got.Doc)
	}
	// The dialog width every other sheet takes on a desktop.
	if got.Sheet <= 560 {
		t.Errorf("the document window is %d px wide — that is the width of a dialog, not of a document", got.Sheet)
	}
	if want := 0.7 * float64(got.Window); float64(got.Sheet) < want {
		t.Errorf("the document window is %d px on a %d px screen — it leaves more room empty than it uses (wanted at least %.0f)",
			got.Sheet, got.Window, want)
	}
	if float64(got.Page) < 45*got.Root {
		t.Errorf("the reading column inside the window is %d px on a %.0f px root — the window grew and the column did not",
			got.Page, got.Root)
	}
	if got.Prose <= got.Root {
		t.Errorf("the prose reads at %.2f px on a %.2f px root: a document at a desk steps above the panel around it", got.Prose, got.Root)
	}
	if got.Exits != 1 {
		t.Errorf("%d ways out of the document (%s) — a handle, a cross and an arrow stacked at its top read as none", got.Exits, got.ExitKinds)
	}
}

// On a phone the document takes the screen. A sheet stopped at half of it
// leaves six lines of a piece read for the better part of an hour, and the
// dock lands on the text instead of under it.
func TestABriefOnAPhoneTakesTheScreen(t *testing.T) {
	var got sheetShot
	runFixture(t, "briefsheet.html", &got)

	if got.Exits != 1 {
		t.Errorf("%d ways out of the document on a phone — one is the arrow it draws itself", got.Exits)
	}
	if want := float64(got.Height) * 0.8; float64(got.SheetHeight) < want {
		t.Errorf("the document is %d px tall on a %d px screen — it opens as a sheet stopped partway (wanted at least %.0f)",
			got.SheetHeight, got.Height, want)
	}
}

// The window is only half of it: the conversation has to say that what it puts
// in the sheet is a document. Without that word the brief is drawn as every
// other layer of the run — a dialog the width of a question.
func TestTheConversationCallsABriefADocument(t *testing.T) {
	src := srcFiles(t)["src/screens/chat.js"]
	if src == "" {
		t.Fatal("src/screens/chat.js not found — the test is looking in the wrong place")
	}
	if !strings.Contains(src, "doc=") {
		t.Fatal("the conversation never calls anything a document — a brief opened over it is drawn as a dialog")
	}
	doc := regexp.MustCompile(`doc=\$\{[^}]*\}`).FindString(src)
	if !strings.Contains(doc, "brief") {
		t.Errorf("the document sheet is tied to %q, not to a brief — either everything opened over the run is drawn as a document or nothing is", doc)
	}
}
