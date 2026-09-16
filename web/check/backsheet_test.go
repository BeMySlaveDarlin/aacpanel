package check

import "testing"

type backSheetShot struct {
	Before struct {
		Sheet bool `json:"sheet"`
		Page  bool `json:"page"`
		Doc   bool `json:"doc"`
		Mine  bool `json:"mine"`
	} `json:"before"`
	After struct {
		Sheet bool `json:"sheet"`
		Page  bool `json:"page"`
		Doc   bool `json:"doc"`
	} `json:"after"`
	Closed []string `json:"closed"`
}

// A brief opened out of the shelf of a conversation: a sheet closes and a page
// with a document on it opens in the same turn. Three claims on the back
// gesture change hands at once, and the gesture still has to take off the
// document — on a phone, closing the page instead reads as the application
// going away.
func TestTheGestureAfterASheetHandsOverToTheDocument(t *testing.T) {
	var got backSheetShot
	runFixture(t, "backsheet.html", &got)

	if got.Before.Sheet || !got.Before.Page || !got.Before.Doc {
		t.Fatalf("the fixture did not get to the document: %+v", got.Before)
	}
	if !got.Before.Mine {
		t.Fatal("the layers changed hands and the entry of the open one went with them: " +
			"the next gesture walks past the application instead of closing the document")
	}
	if got.After.Doc {
		t.Error("the gesture left the document open")
	}
	if !got.After.Page {
		t.Error("the gesture closed the page under the document: on a phone that is the application")
	}
	if len(got.Closed) == 0 || got.Closed[len(got.Closed)-1] != "doc" {
		t.Errorf("what closed, in order: %v", got.Closed)
	}
}
