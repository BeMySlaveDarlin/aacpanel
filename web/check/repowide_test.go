package check

import "testing"

type repoWideShot struct {
	LaidOut          bool     `json:"laidOut"`
	PaneOnTheRight   bool     `json:"paneOnTheRight"`
	PaneWidth        int      `json:"paneWidth"`
	MainWidth        int      `json:"mainWidth"`
	TabNames         []string `json:"tabNames"`
	ActiveName       string   `json:"activeName"`
	CodeScrolls      string   `json:"codeScrolls"`
	PaneScrolls      string   `json:"paneScrolls"`
	AfterClose       []string `json:"afterClose"`
	ActiveAfter      string   `json:"activeAfter"`
	PaneGone         bool     `json:"paneGone"`
	MainGrew         bool     `json:"mainGrew"`
	PhoneStripHidden bool     `json:"phoneStripHidden"`
}

// At a desk the directory belongs beside the code, not on a tab in front of
// it: the whole point of the wide screen is reading a file while still seeing
// where it sits.
func TestTheDirectoryStandsBesideTheCode(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if !got.LaidOut {
		t.Fatal("the wide shape did not draw at all — there is no panel and no middle to measure")
	}
	if !got.PaneOnTheRight {
		t.Error("the panel is not beside the code: it overlaps the middle instead of standing to its right")
	}
	if got.PaneWidth < 200 {
		t.Errorf("the panel is %d px wide — a directory that narrow shows paths and nothing else", got.PaneWidth)
	}
	if got.MainWidth <= got.PaneWidth {
		t.Errorf("the code gets %d px against the panel's %d — the middle is what the screen is for", got.MainWidth, got.PaneWidth)
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
// the middle, and the code takes the room it gives back.
func TestThePanelFoldsAwayAndTheCodeTakesTheRoom(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if !got.PaneGone {
		t.Error("the panel is still drawn after it was folded away")
	}
	if !got.MainGrew {
		t.Error("the panel folded away and the code stayed the same width — the room went nowhere")
	}
}

// Two scrolls, not one: walking a directory must not move the file being read,
// and reading a long file must not lose the place in the tree.
func TestTheCodeAndThePanelScrollApart(t *testing.T) {
	var got repoWideShot
	runWideFixture(t, "repowide.html", &got)

	if got.CodeScrolls != "auto" && got.CodeScrolls != "scroll" {
		t.Errorf("the code scrolls as %q — the middle has to carry its own scroll", got.CodeScrolls)
	}
	if got.PaneScrolls != "auto" && got.PaneScrolls != "scroll" {
		t.Errorf("the panel scrolls as %q — a long directory has to scroll without taking the code along", got.PaneScrolls)
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
