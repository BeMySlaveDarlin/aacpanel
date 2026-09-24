package check

import (
	"strings"
	"testing"
)

type repoPlainShot struct {
	Wide  bool `json:"wide"`
	First struct {
		TreeShown bool     `json:"treeShown"`
		Chips     []string `json:"chips"`
		Tabs      []string `json:"tabs"`
		Title     string   `json:"title"`
		Crumb     string   `json:"crumb"`
		Idle      string   `json:"idle"`
		Refusal   bool     `json:"refusal"`
	} `json:"first"`
	Inner []string `json:"inner"`
	File  struct {
		Opened bool     `json:"opened"`
		Lines  []string `json:"lines"`
		Chips  []string `json:"chips"`
		Tabs   []string `json:"tabs"`
	} `json:"file"`
	NoteBox bool `json:"noteBox"`
	Picked  bool `json:"picked"`
	Found   *struct {
		Partial    []string `json:"partial"`
		Rows       []string `json:"rows"`
		Tabs       []string `json:"tabs"`
		Lines      []string `json:"lines"`
		AfterClose struct {
			Tabs  []string `json:"tabs"`
			Strip bool     `json:"strip"`
			Idle  string   `json:"idle"`
		} `json:"afterClose"`
	} `json:"found"`
	AskedBranch []string `json:"askedBranch"`
}

// The files of a project are there whether git keeps it or not. A screen that
// answers a plain directory with "nothing here to compare" shuts the catalogue
// because of a question nobody asked it.
func TestAProjectWithoutGitOpensOnItsFiles(t *testing.T) {
	for _, shape := range []struct {
		name string
		run  func(*testing.T, string, any)
		wide bool
	}{
		{"phone", runFixture, false},
		{"desk", runWideFixture, true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			var got repoPlainShot
			shape.run(t, "repoplain.html", &got)
			if got.Wide != shape.wide {
				t.Fatalf("the fixture drew the wide shape %v on the %s", got.Wide, shape.name)
			}
			if got.First.Refusal {
				t.Error("the screen still refuses a directory for having no repository")
			}
			if !got.First.TreeShown {
				t.Fatal("the tree is not on the screen — a project without git opens on nothing")
			}
			if got.First.Crumb != "notes" {
				t.Errorf("the root of the tree is called %q — without a branch it is the directory, notes", got.First.Crumb)
			}
			if !shape.wide && got.First.Title != "notes" {
				t.Errorf("the head reads %q — without a branch it names the directory", got.First.Title)
			}
			if !hasTab(got.Inner, "plan.md") {
				t.Errorf("a directory opened to %v — plan.md sits in it", got.Inner)
			}
			if !got.File.Opened || len(got.File.Lines) != 2 || got.File.Lines[0] != "# todo" {
				t.Errorf("the file opened as %v — it has two lines, the first of them \"# todo\"", got.File.Lines)
			}
		})
	}
}

// What belongs to a branch has nothing to stand on without one: the run of
// changes, the notes, the diff and the base are left out rather than drawn
// empty or refused on tap.
func TestAProjectWithoutGitShowsNothingOfABranch(t *testing.T) {
	for _, shape := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{
		{"phone", runFixture},
		{"desk", runWideFixture},
	} {
		t.Run(shape.name, func(t *testing.T) {
			var got repoPlainShot
			shape.run(t, "repoplain.html", &got)
			for _, stage := range []struct {
				when  string
				chips []string
				tabs  []string
			}{
				{"on opening", got.First.Chips, got.First.Tabs},
				{"with a file open", got.File.Chips, got.File.Tabs},
			} {
				for _, c := range append(append([]string{}, stage.chips...), stage.tabs...) {
					for _, branch := range []string{"Changes", "Notes", "Diff", "project base", "this reading"} {
						if strings.HasPrefix(c, branch) {
							t.Errorf("%s the screen offers %q, and there is no branch for it to read", stage.when, c)
						}
					}
				}
			}
			if len(got.AskedBranch) > 0 {
				t.Errorf("the screen asked about a branch that is not there: %v", got.AskedBranch)
			}
		})
	}
}

// A note is kept with a reading of a branch. Without a branch a line is not
// picked up into a box that has nowhere to keep what is written in it.
func TestALineOfAPlainFileIsNotPickedForANote(t *testing.T) {
	for _, shape := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{
		{"phone", runFixture},
		{"desk", runWideFixture},
	} {
		t.Run(shape.name, func(t *testing.T) {
			var got repoPlainShot
			shape.run(t, "repoplain.html", &got)
			if len(got.File.Lines) == 0 {
				t.Fatal("no file was open to tap a line of")
			}
			if got.NoteBox || got.Picked {
				t.Error("a tap on a line of a plain file opened the box for a note")
			}
		})
	}
}

// At a desk the middle has no run of changes to fall back to. With nothing
// open it says how to get to a file, the open files are the only tabs, and
// the finder works the same as it does over a repository.
func TestAPlainProjectAtADesk(t *testing.T) {
	var got repoPlainShot
	runWideFixture(t, "repoplain.html", &got)

	if !strings.Contains(got.First.Idle, "Ctrl+K") {
		t.Errorf("the middle with nothing open reads %q — it should say how to reach a file", got.First.Idle)
	}
	if len(got.File.Tabs) != 1 || got.File.Tabs[0] != "todo.md" {
		t.Errorf("with one file open the tabs are %v — the file is the only one", got.File.Tabs)
	}
	if got.Found == nil {
		t.Fatal("the fixture did not reach the finder")
	}
	// A walk that stopped short is said as that, not as "type more of the
	// name": typing narrows what was found, not what was walked.
	if len(got.Found.Partial) != 1 || !strings.Contains(got.Found.Partial[0], "stopped") {
		t.Errorf("a search that walked part of the directory reads %v — it should say the walk stopped short, and only that", got.Found.Partial)
	}
	if len(got.Found.Rows) != 1 || !strings.Contains(got.Found.Rows[0], "plan.md") {
		t.Errorf("the finder shows %v — plan.md matched", got.Found.Rows)
	}
	if len(got.Found.Tabs) != 2 || got.Found.Tabs[1] != "plan.md" {
		t.Errorf("after Enter the tabs are %v — the found file opens as a tab of its own", got.Found.Tabs)
	}
	if len(got.Found.Lines) != 1 || got.Found.Lines[0] != "the plan" {
		t.Errorf("the found file drew %v", got.Found.Lines)
	}
	if got.Found.AfterClose.Strip || len(got.Found.AfterClose.Tabs) != 0 {
		t.Errorf("with every file closed the strip still stands with %v", got.Found.AfterClose.Tabs)
	}
	if !strings.Contains(got.Found.AfterClose.Idle, "Ctrl+K") {
		t.Errorf("with every file closed the middle reads %q", got.Found.AfterClose.Idle)
	}
}
