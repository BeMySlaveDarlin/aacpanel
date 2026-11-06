package check

import "testing"

type pastOrderShot struct {
	Heads       []string `json:"heads"`
	GroupsFirst bool     `json:"groupsFirst"`
	Stacks      int      `json:"stacks"`
	Error       string   `json:"error"`
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
