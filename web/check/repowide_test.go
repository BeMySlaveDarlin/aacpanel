package check

import "testing"

type repoWideShot struct {
	LaidOut          bool     `json:"laidOut"`
	PaneAfterMain    bool     `json:"paneAfterMain"`
	TabNames         []string `json:"tabNames"`
	ActiveName       string   `json:"activeName"`
	AfterClose       []string `json:"afterClose"`
	ActiveAfter      string   `json:"activeAfter"`
	PaneGone         bool     `json:"paneGone"`
	PhoneStripHidden bool     `json:"phoneStripHidden"`
}

// At a desk the directory is a panel of its own after the code, not a tab in
// front of it: the whole point of the wide screen is reading a file while
// still seeing where it sits.
func TestTheDirectoryIsAPanelBesideTheCodeNotATab(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if !got.LaidOut {
		t.Fatal("the wide shape did not draw at all — there is no panel and no middle")
	}
	if !got.PaneAfterMain {
		t.Error("the panel of the directory does not come after the code")
	}
	if !got.PhoneStripHidden {
		t.Error("the phone's Files tab is still in the strip: the tree is drawn twice, once beside the code and once in front of it")
	}
}

// Every file opened gets a tab, and the changes keep the first one. A viewer
// whose every tab can be shut leaves a blank panel and no way back to the run.
func TestEveryOpenFileGetsATabAndTheChangesKeepTheFirst(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if len(got.TabNames) != 4 {
		t.Fatalf("tabs are %v — three files were opened beside the changes", got.TabNames)
	}
	if got.TabNames[0] != "Changes" {
		t.Errorf("the first tab is %q: the run of changes is where a reading starts", got.TabNames[0])
	}
	for _, want := range []string{"README.md", "fresh.txt", "env.go"} {
		if !hasTab(got.TabNames, want) {
			t.Errorf("%q was opened and has no tab of its own: %v", want, got.TabNames)
		}
	}
	if got.ActiveName != "env.go" {
		t.Errorf("the tab in front is %q — the file opened last is the one being read", got.ActiveName)
	}
}

// Closing the tab being read hands the screen to the tab beside it: that is
// where the eye already is. Dropping back to the changes every time makes
// closing a file cost the place in the reading.
func TestClosingTheReadTabHandsTheScreenToItsNeighbour(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if len(got.AfterClose) != 3 {
		t.Fatalf("after closing one tab there are %v — one of four should have gone", got.AfterClose)
	}
	if hasTab(got.AfterClose, "env.go") {
		t.Error("the closed tab is still there")
	}
	if got.ActiveAfter != "fresh.txt" {
		t.Errorf("after the close the screen reads %q — the neighbour of what was closed is fresh.txt", got.ActiveAfter)
	}
}

// The panel folds away for a file whose lines are wider than what is left of
// the middle.
func TestThePanelFoldsAway(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if !got.PaneGone {
		t.Error("the panel is still drawn after it was folded away")
	}
}

func hasTab(tabs []string, want string) bool {
	for _, t := range tabs {
		if t == want {
			return true
		}
	}
	return false
}
