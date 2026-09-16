package check

import "testing"

type backStackShot struct {
	Before struct {
		Doc  bool `json:"doc"`
		Page bool `json:"page"`
	} `json:"before"`
	AfterOne struct {
		Doc  bool `json:"doc"`
		Page bool `json:"page"`
	} `json:"afterOne"`
	Closed []string `json:"closed"`
}

// The back gesture takes off the topmost thing the reader sees, which is the
// document, not the page under it. Effects run child first, so a page that
// claims the gesture while something is open above it ends up on top of the
// stack and the swipe closes the page whole — on a phone that reads as the
// application closing.
func TestTheBackGestureTakesOffTheDocumentFirst(t *testing.T) {
	var got backStackShot
	runFixture(t, "backstack.html", &got)

	if !got.Before.Doc || !got.Before.Page {
		t.Fatalf("the fixture did not draw both: %+v", got.Before)
	}
	if got.AfterOne.Doc {
		t.Error("the first gesture left the document open")
	}
	if !got.AfterOne.Page {
		t.Error("the first gesture closed the page under the document: on a phone that is the application")
	}
	if len(got.Closed) != 1 || got.Closed[0] != "doc" {
		t.Errorf("what the gesture closed: %v", got.Closed)
	}
}
