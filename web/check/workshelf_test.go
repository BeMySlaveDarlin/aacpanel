package check

import (
	"strings"
	"testing"
)

type workShelfShot struct {
	First struct {
		Height     int      `json:"height"`
		FeedHeight int      `json:"feedHeight"`
		Tabs       []string `json:"tabs"`
		Rows       int      `json:"rows"`
		More       string   `json:"more"`
		Titles     []string `json:"titles"`
	} `json:"first"`
	After struct {
		Rows int    `json:"rows"`
		More string `json:"more"`
	} `json:"after"`
	OnBriefs struct {
		Rows   int      `json:"rows"`
		Titles []string `json:"titles"`
		Kept   int      `json:"kept"`
		States []string `json:"states"`
		Tones  []string `json:"tones"`
		Order  []string `json:"order"`
	} `json:"onBriefs"`
	Opened struct {
		Pages  []string `json:"pages"`
		Briefs []string `json:"briefs"`
	} `json:"opened"`
	Button struct {
		Num   string `json:"num"`
		Wants bool   `json:"wants"`
		Said  string `json:"said"`
	} `json:"button"`
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
	if got.OnBriefs.Rows != 3 || got.OnBriefs.Kept != 3 {
		t.Errorf("the tab of briefs holds %d rows, %d of them briefs", got.OnBriefs.Rows, got.OnBriefs.Kept)
	}
	if len(got.OnBriefs.Titles) != 3 || got.OnBriefs.Titles[0] != "Seven questions after twelve" {
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

// The chip over the deck counts what the conversation made, and a brief is part
// of that. It also says, without opening anything, that some of it is waiting
// for the person: a number alone cannot tell twelve finished rows from twelve
// rows one of which wants an answer.
func TestTheChipCountsTheBriefsAndSaysWhenOneWaits(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	// Three artifacts in the feed and three briefs.
	if got.Button.Num != "6" {
		t.Errorf("the chip counts %q of six things the conversation made", got.Button.Num)
	}
	if !got.Button.Wants {
		t.Error("two briefs are unsent and the chip says nothing is waiting")
	}
	if !strings.Contains(got.Button.Said, "waiting") {
		t.Errorf("the chip tells a screen reader %q", got.Button.Said)
	}
}

// A row of a brief says where the document stands. Without it the shelf lists
// what was asked and hides whether it was answered, which is the one thing the
// reader came to see.
func TestEachBriefSaysWhereItStands(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	want := []string{"0 of 7", "sent", "2 of 4"}
	if len(got.OnBriefs.States) != len(want) {
		t.Fatalf("the rows say %v", got.OnBriefs.States)
	}
	for i, w := range want {
		if got.OnBriefs.States[i] != w {
			t.Errorf("row %d says %q, expected %q", i+1, got.OnBriefs.States[i], w)
		}
	}
	if len(got.OnBriefs.Tones) < 2 || got.OnBriefs.Tones[1] != "is-sent" {
		t.Errorf("a brief already sent is drawn as %v", got.OnBriefs.Tones)
	}
}

// Newest first, in both tabs. A shelf that lists the oldest at the top hides
// today's work under a month of it.
func TestBothTabsPutTheNewestOnTop(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	if got.First.Titles[0] != "Recent page 2" {
		t.Errorf("the pages start with %q", got.First.Titles[0])
	}
	want := []string{"Seven questions after twelve", "What is still open", "Half answered"}
	for i, w := range want {
		if i >= len(got.OnBriefs.Order) || got.OnBriefs.Order[i] != w {
			t.Errorf("the briefs read %v, expected %v", got.OnBriefs.Order, want)
			break
		}
	}
}

// The shelf is a list to scan: a row that takes the height of a feed card fits
// four to a phone screen, and the one being looked for is the fifth.
func TestTheRowsOfTheShelfAreRows(t *testing.T) {
	var got workShelfShot
	runFixture(t, "workshelf.html", &got)

	if got.First.Height == 0 || got.First.FeedHeight == 0 {
		t.Fatal("nothing was measured")
	}
	// A tenth is the difference between a row and a card here; anything less
	// and the shelf is the feed with a narrower gap.
	if got.First.Height*10 > got.First.FeedHeight*9 {
		t.Errorf("a row of the shelf is %d tall against %d for the same card outside it: "+
			"the shelf is a list to scan, and the card is one to read",
			got.First.Height, got.First.FeedHeight)
	}
}
