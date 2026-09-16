package check

import (
	"strings"
	"testing"
)

type briefRowShot struct {
	Before struct {
		Titles  []string `json:"titles"`
		Descs   []string `json:"descs"`
		Metas   []string `json:"metas"`
		Tags    []string `json:"tags"`
		Icons   int      `json:"icons"`
		Heights []int    `json:"heights"`
		Feed    int      `json:"feed"`
		Paper   []string `json:"paper"`
	} `json:"before"`
	Opened  []string `json:"opened"`
	Nowhere struct {
		Tag  string `json:"tag"`
		Dead bool   `json:"dead"`
	} `json:"nowhere"`
}

// A session that published a brief said so in a sentence that scrolls away with
// everything else. The card stays in the run at the point the document went out,
// and says what it is and how much it asks.
func TestAPublishedBriefLeavesACardInTheFeed(t *testing.T) {
	var got briefRowShot
	runFixture(t, "briefrow.html", &got)

	if len(got.Before.Titles) != 2 {
		t.Fatalf("cards drawn: %v", got.Before.Titles)
	}
	if got.Before.Titles[0] != "Seven questions after twelve" {
		t.Errorf("the card does not carry the title of the document: %q", got.Before.Titles[0])
	}
	if len(got.Before.Descs) != 1 || got.Before.Descs[0] != "after the review" {
		t.Errorf("the line under the title went missing: %v", got.Before.Descs)
	}
	if !strings.Contains(got.Before.Metas[0], "7 questions") {
		t.Errorf("the card does not say how much it asks: %q", got.Before.Metas[0])
	}
	if !strings.Contains(got.Before.Metas[1], "nothing to answer") {
		t.Errorf("a brief with no questions promises an answer it has no room for: %q", got.Before.Metas[1])
	}
	if got.Before.Icons != 2 {
		t.Errorf("%d cards carry the mark of a document", got.Before.Icons)
	}
}

// The card opens the document it names, inside the panel: a brief is not a link
// out of it, and a row that only looks pressable is worse than a flat one.
func TestTheBriefCardOpensTheDocument(t *testing.T) {
	var got briefRowShot
	runFixture(t, "briefrow.html", &got)

	for i, tag := range got.Before.Tags {
		if tag != "BUTTON" {
			t.Errorf("card %d is a %s while it has somewhere to go", i+1, tag)
		}
	}
	if len(got.Opened) != 1 || got.Opened[0] != "seven-after-twelve" {
		t.Errorf("the tap opened %v", got.Opened)
	}
	if got.Nowhere.Tag == "BUTTON" || !got.Nowhere.Dead {
		t.Errorf("with nowhere to go the card still looks pressable: %+v", got.Nowhere)
	}
}

// The card points at a document; it is not one. A brief page carries a palette
// and a page's height under a class of its own, and a card that answered to the
// same name took both: the feed showed one row of text on a sheet of paper the
// size of the screen.
func TestTheBriefCardIsARowAndNotThePage(t *testing.T) {
	var got briefRowShot
	runFixture(t, "briefrow.html", &got)

	for i, h := range got.Before.Heights {
		if h > got.Before.Feed/3 {
			t.Errorf("card %d is %d tall in a feed of %d: it stretched to the page", i+1, h, got.Before.Feed)
		}
	}
	for i, paper := range got.Before.Paper {
		if paper != "" {
			t.Errorf("card %d took the paper of the document: %q", i+1, paper)
		}
	}
}
