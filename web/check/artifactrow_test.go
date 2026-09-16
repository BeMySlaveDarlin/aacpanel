package check

import "testing"

type artifactRowShot struct {
	Shot struct {
		Tags   []string `json:"tags"`
		Hrefs  []string `json:"hrefs"`
		Titles []string `json:"titles"`
	} `json:"shot"`
	Opened []string `json:"opened"`
}

// A page is published into the account its session works under, and the reader
// of the panel is signed into one account at a time. Where the panel kept a
// copy the card opens that copy here; where it did not, the card is still the
// link it always was, for whoever holds the right account.
func TestAKeptPageOpensInThePanelAndTheRestStillLinkOut(t *testing.T) {
	var got artifactRowShot
	runFixture(t, "artifactrow.html", &got)

	if len(got.Shot.Tags) != 2 {
		t.Fatalf("cards drawn: %v", got.Shot.Titles)
	}
	if got.Shot.Tags[0] != "BUTTON" {
		t.Errorf("the card of a kept page is a %s: it sends the reader out of the panel "+
			"to an account they may not be signed into", got.Shot.Tags[0])
	}
	if got.Shot.Hrefs[0] != "" {
		t.Errorf("the card of a kept page still carries an address out: %q", got.Shot.Hrefs[0])
	}
	if got.Shot.Tags[1] != "A" {
		t.Errorf("a page with no copy is drawn as a %s and leads nowhere at all", got.Shot.Tags[1])
	}
	if got.Shot.Hrefs[1] == "" {
		t.Error("a page with no copy lost its link: that link was the only way to it")
	}
	if len(got.Opened) != 1 || got.Opened[0] != "ab12cd34" {
		t.Errorf("pressing the card opened %v", got.Opened)
	}
}
