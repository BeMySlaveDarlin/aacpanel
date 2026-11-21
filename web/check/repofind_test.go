package check

import "testing"

type repoFindShot struct {
	BeforeKeys bool     `json:"beforeKeys"`
	Opened     bool     `json:"opened"`
	Focused    bool     `json:"focused"`
	AskedEmpty bool     `json:"askedEmpty"`
	Rows       []string `json:"rows"`
	FirstOn    bool     `json:"firstOn"`
	EmptySaid  bool     `json:"emptySaid"`
	SecondOn   bool     `json:"secondOn"`
	Closed     bool     `json:"closed"`
	Tabs       []string `json:"tabs"`
	Reopened   bool     `json:"reopened"`
	Escaped    bool     `json:"escaped"`
}

// The finder is not on the screen until it is asked for, opens on the keys it
// is bound to, and takes the typing straight away: a box that opens without
// the caret in it is a box that eats the first letters of every name.
func TestRepoTheFinderOpensOnItsKeysAndTakesTheTyping(t *testing.T) {
	var got repoFindShot
	runWideFixture(t, "repofind.html", &got)

	if got.BeforeKeys {
		t.Error("the finder is drawn before anyone asked for it")
	}
	if !got.Opened {
		t.Fatal("Ctrl+K did not open the finder")
	}
	if !got.Focused {
		t.Error("the finder opened without the caret in its box — the first letters typed go nowhere")
	}
	if got.AskedEmpty {
		t.Error("an empty box asked the repository for something: the whole tree is not an answer to a question nobody typed")
	}
}

// What comes back is a list to pick from, with the first one already picked so
// that Enter means something the moment the name is typed.
func TestRepoWhatIsFoundIsAListWithTheFirstOneReady(t *testing.T) {
	var got repoFindShot
	runWideFixture(t, "repofind.html", &got)

	if len(got.Rows) != 2 {
		t.Fatalf("the finder shows %v — two names matched", got.Rows)
	}
	if !got.FirstOn {
		t.Error("nothing is picked when the list arrives: Enter would do nothing on a list a person is already looking at")
	}
	if !got.SecondOn {
		t.Error("the arrow key did not move the pick down the list")
	}
	if !got.EmptySaid {
		t.Error("a search that matched nothing draws a blank panel instead of saying so")
	}
}

// Enter opens the picked file and clears the results in the sidebar.
func TestRepoEnterOpensThePickedFile(t *testing.T) {
	var got repoFindShot
	runWideFixture(t, "repofind.html", &got)

	if !got.Closed {
		t.Error("the finder is still over the code after opening a file from it")
	}
	if len(got.Tabs) != 1 || got.Tabs[0] != "pkg/deep/env_test.go" {
		t.Errorf("the open path is %v — the file picked with the arrow key is the one that should have opened", got.Tabs)
	}
}

// Escape leaves the search field and clears its results.
func TestRepoEscapeLeavesTheFinderWithoutOpeningAnything(t *testing.T) {
	var got repoFindShot
	runWideFixture(t, "repofind.html", &got)

	if !got.Reopened {
		t.Fatal("the finder did not open the second time")
	}
	if !got.Escaped {
		t.Error("Escape left the finder standing over the code")
	}
}
