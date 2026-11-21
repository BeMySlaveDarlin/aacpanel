package check

import (
	"strings"
	"testing"
)

type repoPlainShot struct {
	Wide           bool     `json:"wide"`
	TreeShown      bool     `json:"treeShown"`
	BranchBefore   int      `json:"branchBefore"`
	Button         string   `json:"button"`
	Inner          []string `json:"inner"`
	Lines          []string `json:"lines"`
	BranchWithFile int      `json:"branchWithFile"`
	NoteBox        bool     `json:"noteBox"`
	Picked         bool     `json:"picked"`
	Partial        []string `json:"partial"`
	Rows           []string `json:"rows"`
	FoundLines     []string `json:"foundLines"`
	Path           string   `json:"path"`
	Returned       bool     `json:"returned"`
	AskedBranch    []string `json:"askedBranch"`
}

func TestRepoPlainDirectoryKeepsItsFilesAndSearch(t *testing.T) {
	parallel(t)
	for _, shape := range []struct {
		name string
		run  func(*testing.T, string, any)
		wide bool
	}{
		{"phone", runFixture, false}, {"desk", runWideFixture, true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			var got repoPlainShot
			shape.run(t, "repoplain.html", &got)
			if got.Wide != shape.wide || !got.TreeShown {
				t.Fatalf("the plain directory did not open: %+v", got)
			}
			if got.BranchBefore != 0 || got.BranchWithFile != 0 || len(got.AskedBranch) != 0 {
				t.Errorf("a plain directory has branch controls or requests: %+v", got)
			}
			if strings.Contains(got.Button, "changed") {
				t.Errorf("the plain Files button counts changes: %q", got.Button)
			}
			if !strings.Contains(strings.Join(got.Inner, " "), "plan.md") {
				t.Errorf("drafts did not open: %v", got.Inner)
			}
			if len(got.Lines) != 2 || got.Lines[0] != "# todo" {
				t.Errorf("todo.md drew %v", got.Lines)
			}
			if got.NoteBox || got.Picked {
				t.Error("a plain file's line was picked for a note")
			}
			if len(got.Partial) != 1 || !strings.Contains(got.Partial[0], "stopped") {
				t.Errorf("the partial search says %v", got.Partial)
			}
			if len(got.Rows) != 1 || !strings.Contains(got.Rows[0], "plan.md") {
				t.Errorf("the search found %v", got.Rows)
			}
			if got.Path != "drafts/plan.md" || len(got.FoundLines) != 1 || got.FoundLines[0] != "the plan" {
				t.Errorf("the search opened %q: %v", got.Path, got.FoundLines)
			}
			if !got.Returned {
				t.Error("returning from the plain file lost the feed or the directory")
			}
		})
	}
}
