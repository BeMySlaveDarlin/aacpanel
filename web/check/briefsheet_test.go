package check

import (
	"regexp"
	"strings"
	"testing"
)

type sheetShot struct {
	Exits     int    `json:"exits"`
	ExitKinds string `json:"exitKinds"`
	Win       bool   `json:"win"`
	Doc       bool   `json:"doc"`
	Decisions int    `json:"decisions"`
	Task      string `json:"task"`
	Inline    bool   `json:"inline"`
	Single    bool   `json:"single"`
	Marker    string `json:"marker"`
	Icon      bool   `json:"icon"`
	Closed    bool   `json:"closed"`
	Sent      string `json:"sent"`
}

func TestBriefDecisionsLiveInTheMoveSlotAndTheDocumentSheet(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got sheetShot
			screen.run(t, "briefsheet.html", &got)
			if got.Decisions != 2 || !strings.Contains(got.Task, "Choose how the search moves") || !got.Inline || !got.Icon || !got.Single {
				t.Errorf("the form is missing from the sheet or move slot: %+v", got)
			}
			if !strings.Contains(got.Marker, "BRIEF") || !strings.Contains(got.Marker, "2 decisions · yours · below") {
				t.Errorf("the published brief is not a move marker: %q", got.Marker)
			}
			if !got.Closed || got.Sent != "/api/briefs/at-the-desk/send" {
				t.Errorf("sending did not close the move slot: %+v", got)
			}
		})
	}
}

// A brief opens as a layer over the conversation, and on a wide screen every
// other layer is a dialog the width of a question. The sheet that carries a
// document is marked a document, with one way out.
func TestABriefOverAConversationIsNotDrawnAsADialog(t *testing.T) {
	var got sheetShot
	runWideFixture(t, "briefsheet.html", &got)

	if !got.Win || !got.Doc {
		t.Fatalf("the sheet is drawn as %v/%v (window/document) — the fixture is not testing what it says", got.Win, got.Doc)
	}
	if got.Exits != 1 {
		t.Errorf("%d ways out of the document (%s) — a handle, a cross and an arrow stacked at its top read as none", got.Exits, got.ExitKinds)
	}
}

// On a phone the document has one way out: the arrow it draws itself.
func TestABriefOnAPhoneHasOneWayOut(t *testing.T) {
	var got sheetShot
	runFixture(t, "briefsheet.html", &got)

	if got.Exits != 1 {
		t.Errorf("%d ways out of the document on a phone — one is the arrow it draws itself", got.Exits)
	}
}

// The window is only half of it: the conversation has to say that what it puts
// in the sheet is a document. Without that word the brief is drawn as every
// other layer of the run — a dialog the width of a question.
func TestTheConversationCallsABriefADocument(t *testing.T) {
	src := srcFiles(t)["src/screens/chat/conversation.js"]
	if src == "" {
		t.Fatal("src/screens/chat/conversation.js not found — the test is looking in the wrong place")
	}
	if !strings.Contains(src, "doc=") {
		t.Fatal("the conversation never calls anything a document — a brief opened over it is drawn as a dialog")
	}
	doc := regexp.MustCompile(`doc=\$\{[^}]*\}`).FindString(src)
	if !strings.Contains(doc, "brief") {
		t.Errorf("the document sheet is tied to %q, not to a brief — either everything opened over the run is drawn as a document or nothing is", doc)
	}
}
