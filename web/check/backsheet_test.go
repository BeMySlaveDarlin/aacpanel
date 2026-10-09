package check

import (
	"strings"
	"testing"
)

type backSheetShot struct {
	Before struct {
		Sheet bool `json:"sheet"`
		Page  bool `json:"page"`
		Doc   bool `json:"doc"`
		Mine  bool `json:"mine"`
	} `json:"before"`
	After struct {
		Sheet bool   `json:"sheet"`
		Page  bool   `json:"page"`
		Doc   bool   `json:"doc"`
		Kept  string `json:"kept"`
	} `json:"after"`
	Second struct {
		Page bool   `json:"page"`
		Left string `json:"left"`
		Base string `json:"base"`
	} `json:"second"`
	Closed []string `json:"closed"`
}

// A brief opened out of the shelf of a conversation: a sheet closes and a page
// with a document on it opens in the same turn. Three claims on the back
// gesture change hands at once, and the gesture still has to take off the
// document — on a phone, closing the page instead reads as the application
// going away. The page stays under the document it put down, so it stands on
// an entry again, and the second gesture closes the page rather than walking
// out of the application.
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
	if want := `{"overlay":true,"depth":1}`; got.After.Kept != want {
		t.Errorf("the gesture put down the document and left the page on %s instead of %s: "+
			"the next swipe walks out of the application", got.After.Kept, want)
	}
	if got.Second.Page {
		t.Error("the second gesture left the page open")
	}
	if got.Second.Left != got.Second.Base {
		t.Errorf("the second gesture took the history to %s instead of %s, where it stood before anything "+
			"was opened: on a phone that walks out of the application", got.Second.Left, got.Second.Base)
	}
	if strings.Join(got.Closed, ",") != "doc,page" {
		t.Errorf("what the two gestures closed, in order: %v, expected the document and then the page", got.Closed)
	}
}
