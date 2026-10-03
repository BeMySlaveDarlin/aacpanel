package check

import "testing"

type pastOrderShot struct {
	Heads     []string `json:"heads"`
	GroupsTop float64  `json:"groupsTop"`
	CardTop   float64  `json:"cardTop"`
	CardTall  float64  `json:"cardTall"`
	Screen    float64  `json:"screen"`
	Stacks    int      `json:"stacks"`
	Error     string   `json:"error"`
}

// The archive of a contour opens on its projects: a page of the archive runs
// many screens long, and the groups to start a new session in stand over it,
// on the first screen, rather than under the last conversation of the page.
func TestTheArchiveOpensOnTheProjectsToStartASessionIn(t *testing.T) {
	var got pastOrderShot
	runFixture(t, "pastorder.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if got.Stacks != 2 || got.GroupsTop < 0 || got.CardTop < 0 {
		t.Fatalf("the page drew %d groups, the groups at %.0f px and the archive at %.0f px: %v",
			got.Stacks, got.GroupsTop, got.CardTop, got.Heads)
	}
	if got.GroupsTop >= got.CardTop {
		t.Errorf("the project groups stand at %.0f px, under the archive at %.0f px — a new session waits at the bottom of a long page",
			got.GroupsTop, got.CardTop)
	}
	if got.GroupsTop >= got.Screen {
		t.Errorf("the project groups start at %.0f px, past the first screen of %.0f px", got.GroupsTop, got.Screen)
	}
	if got.CardTall < got.Screen {
		t.Errorf("the archive of the fixture is %.0f px tall, under a screen — the page is not long enough to tell the order apart", got.CardTall)
	}
}
