package check

import (
	"strings"
	"testing"
	"time"
)

type pastOrderShot struct {
	Heads        []string `json:"heads"`
	GroupsFirst  bool     `json:"groupsFirst"`
	Stacks       int      `json:"stacks"`
	Error        string   `json:"error"`
	Dates        []string `json:"dates"`
	ProjectNames []string `json:"projectNames"`
	Search       struct {
		Asked []map[string]string `json:"asked"`
		Names [][]string          `json:"names"`
	} `json:"search"`
	Clear struct {
		Asked    []map[string]string `json:"asked"`
		Restored bool                `json:"restored"`
	} `json:"clear"`
}

func TestArchiveDatesAndNameSearch(t *testing.T) {
	var got pastOrderShot
	runFixture(t, "pastorder.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if !equalStrings(got.Dates, []string{"19.11 12:05", "06.05." + time.Now().AddDate(-1, 0, 0).Format("2006") + " 09:04"}) {
		t.Errorf("the archive card's local activity dates are %v", got.Dates)
	}
	if len(got.ProjectNames) == 0 || got.ProjectNames[0] != "nginx-cve" {
		t.Errorf("the worktree's own name is absent from the project: %v", got.ProjectNames)
	}
	if len(got.Search.Asked) != 2 {
		t.Fatalf("typing in two archives sent %d requests, want one per archive: %v", len(got.Search.Asked), got.Search.Asked)
	}
	for _, query := range got.Search.Asked {
		if query["q"] != "nginx" || query["offset"] != "" || query["project"] != "" && query["limit"] != "10" {
			t.Errorf("the archive search did not reset its page or debounce its input: %v", query)
		}
	}
	for _, names := range got.Search.Names {
		if len(names) != 1 || !strings.HasSuffix(names[0], "nginx-result") {
			t.Errorf("the archive did not draw the search response: %v", names)
		}
	}
	if !got.Clear.Restored || len(got.Clear.Asked) != 2 {
		t.Errorf("clearing the searches did not restore both archives: %+v", got.Clear)
	}
	for _, query := range got.Clear.Asked {
		if _, ok := query["q"]; ok {
			t.Errorf("the cleared search still sends q: %v", query)
		}
	}
}

// The archive of a contour opens on its projects: a page of the archive runs
// many screens long, and the groups to start a new session in come before it
// rather than after the last conversation of the page.
func TestTheArchivePutsTheProjectsToStartASessionInFirst(t *testing.T) {
	var got pastOrderShot
	runFixture(t, "pastorder.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if got.Stacks != 2 {
		t.Fatalf("the page drew %d groups: %v", got.Stacks, got.Heads)
	}
	if !got.GroupsFirst {
		t.Errorf("the project groups come after the archive, or there is no head of them: %v — "+
			"a new session waits at the bottom of a long page", got.Heads)
	}
}
