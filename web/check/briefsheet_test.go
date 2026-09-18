package check

import "testing"

type sheetShot struct {
	Window int     `json:"window"`
	Root   float64 `json:"root"`
	Win    bool    `json:"win"`
	Doc    bool    `json:"doc"`
	Sheet  int     `json:"sheet"`
	Page   int     `json:"page"`
	Prose  float64 `json:"prose"`
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
}
