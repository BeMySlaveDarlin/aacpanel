package check

import "testing"

type backRaceShot struct {
	Again struct {
		Base string `json:"base"`
		Left string `json:"left"`
	} `json:"again"`
	Shut struct {
		Base string `json:"base"`
		Left string `json:"left"`
	} `json:"shut"`
	Swiped struct {
		Base   string `json:"base"`
		Left   string `json:"left"`
		Pushed int    `json:"pushed"`
		Asked  int    `json:"asked"`
	} `json:"swiped"`
	Handed struct {
		Left   string `json:"left"`
		Pushed int    `json:"pushed"`
		Doc    bool   `json:"doc"`
		Page   bool   `json:"page"`
	} `json:"handed"`
	Error string `json:"error"`
}

// The browser lands a step back in its own time, and until then the history
// still names the entry that is going. Layers that come and go in that window
// must still leave the history at one entry per open layer: an entry too many
// is a swipe that closes nothing, one too few is a swipe out of the
// application. The fixture holds every step until its walk lets it go, so the
// window a busy phone opens by chance is open on every run.
func TestLayersKeepTheHistoryWhileAStepBackIsOnItsWay(t *testing.T) {
	var got backRaceShot
	runFixture(t, "backrace.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	const offBy = ": an entry too many is a swipe that closes nothing, one too few walks the swipe out of the application"
	if got.Again.Left != got.Again.Base {
		t.Errorf("a layer that opened and closed while the step of another was on its way left the history on %s "+
			"instead of %s%s", got.Again.Left, got.Again.Base, offBy)
	}
	if got.Shut.Left != got.Shut.Base {
		t.Errorf("a sheet shut with a call open on it left the history on %s instead of %s%s",
			got.Shut.Left, got.Shut.Base, offBy)
	}
	if s := got.Swiped; s.Left != s.Base || s.Pushed != 0 || s.Asked != 0 {
		t.Errorf("a swipe over a sheet that stays drawn closed left the history on %s instead of %s, "+
			"with %d entries pushed and %d steps back after it, expected none: an entry given and taken back "+
			"a frame later is a window in which the next swipe is taken for that step", s.Left, s.Base, s.Pushed, s.Asked)
	}
	if want := `{"overlay":true,"depth":1}`; got.Handed.Left != want || got.Handed.Pushed != 0 {
		t.Errorf("a page opened as a conversation and its sheet went stands on %s after %d entries pushed, "+
			"expected %s and none: the entry the conversation left is the page's", got.Handed.Left, got.Handed.Pushed, want)
	}
	if got.Handed.Doc || !got.Handed.Page {
		t.Errorf("after the swipe over the page the document is open: %v, the page is open: %v — "+
			"the swipe takes off the document and nothing under it", got.Handed.Doc, got.Handed.Page)
	}
}
