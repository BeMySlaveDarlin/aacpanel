package check

import (
	"strings"
	"testing"
)

type workShelfShot struct {
	First struct {
		Tabs   []string `json:"tabs"`
		Rows   int      `json:"rows"`
		More   string   `json:"more"`
		Titles []string `json:"titles"`
	} `json:"first"`
	After struct {
		Rows int    `json:"rows"`
		More string `json:"more"`
	} `json:"after"`
	OnBriefs struct {
		Rows   int      `json:"rows"`
		Titles []string `json:"titles"`
		Kept   int      `json:"kept"`
	} `json:"onBriefs"`
	Opened struct {
		Pages  []string `json:"pages"`
		Briefs []string `json:"briefs"`
	} `json:"opened"`
}

// The window of a feed reaches back only so far, and what fell out of it used
// to fall out of the shelf too. The copies the panel keeps are the rest of the
// list, and a page in both places is one row, not two.
func TestTheShelfShowsWhatTheFeedNoLongerReaches(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	// Three in the feed, twenty-six on the shelf, one of them the same page.
	const whole = 28
	if got.First.Rows+countOf(got.First.More) != whole {
		t.Errorf("the shelf holds %d rows and offers %q: the conversation published %d",
			got.First.Rows, got.First.More, whole)
	}
	if got.First.Titles[0] != "Recent page 2" {
		t.Errorf("the newest page is not first: %v", got.First.Titles)
	}
}

// A list that keeps going under the thumb is a list nobody reaches the end of.
func TestTheShelfShowsAPageAtATime(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	if got.First.Rows != 20 {
		t.Errorf("the first page holds %d rows", got.First.Rows)
	}
	if !strings.Contains(got.First.More, "8 more") {
		t.Errorf("the rest is offered as %q", got.First.More)
	}
	if got.After.Rows != 28 {
		t.Errorf("pressing for more left %d rows", got.After.Rows)
	}
	if got.After.More != "" {
		t.Errorf("there is still something offered after the end: %q", got.After.More)
	}
}

// The briefs of a conversation are read where everything else it made is read,
// and they open the document rather than a copy of a page.
func TestTheShelfCarriesTheBriefsOfTheConversation(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	if len(got.First.Tabs) != 2 {
		t.Fatalf("tabs: %v", got.First.Tabs)
	}
	if !strings.Contains(got.First.Tabs[0], "published") || !strings.Contains(got.First.Tabs[1], "briefs") {
		t.Errorf("the tabs read %v", got.First.Tabs)
	}
	if got.OnBriefs.Rows != 2 || got.OnBriefs.Kept != 2 {
		t.Errorf("the tab of briefs holds %d rows, %d of them briefs", got.OnBriefs.Rows, got.OnBriefs.Kept)
	}
	if len(got.OnBriefs.Titles) != 2 || got.OnBriefs.Titles[0] != "Seven questions after twelve" {
		t.Errorf("the briefs read %v", got.OnBriefs.Titles)
	}
	if len(got.Opened.Briefs) != 1 || got.Opened.Briefs[0] != "seven-after-twelve" {
		t.Errorf("pressing a brief opened %v", got.Opened.Briefs)
	}
}

// countOf reads the number out of "8 more"; an empty offer counts as nothing.
func countOf(more string) int {
	n := 0
	for _, c := range more {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
