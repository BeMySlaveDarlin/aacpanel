package check

import (
	"os"
	"testing"
)

type chatHeadSeen struct {
	Deck           bool    `json:"deck"`
	HeadUse        bool    `json:"headUse"`
	Path           bool    `json:"path"`
	DeckUse        bool    `json:"deckUse"`
	DeckUseLeftGap float64 `json:"deckUseLeftGap"`
	DeckUseFirst   bool    `json:"deckUseFirst"`
	PathText       string  `json:"pathText"`
	Cut            bool    `json:"cut"`
	EndShown       bool    `json:"endShown"`
	StartHidden    bool    `json:"startHidden"`
	LineHeight     float64 `json:"lineHeight"`
	HeadRight      float64 `json:"headRight"`
	PathRight      float64 `json:"pathRight"`
	WholeText      string  `json:"wholeText"`
	WholeCut       bool    `json:"wholeCut"`
	WholeHeight    float64 `json:"wholeHeight"`
}

// On a phone the third line of the conversation header says where the session
// works, and the tokens in and out stand at the left of the row under the
// composer. A long path is cut at its head and keeps the project it ends with.
func TestThePhoneHeaderSaysWhereTheSessionWorks(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: where the path is cut is the stylesheet's business — run make front first")
	}
	var got chatHeadSeen
	runFixture(t, "chathead.html", &got)
	if !got.Path {
		t.Fatal("the header does not say where the session works")
	}
	if got.HeadUse {
		t.Error("the tokens in and out are still in the header, where the path goes")
	}
	if !got.DeckUse || !got.DeckUseFirst || got.DeckUseLeftGap > 2 {
		t.Errorf("the tokens in and out are not at the left of the row under the composer: shown %v, first %v, %.0f px in",
			got.DeckUse, got.DeckUseFirst, got.DeckUseLeftGap)
	}
	if got.PathText != "~/Projects/Pets/service/a-rather-long-group/and-a-subgroup/aacpanel" {
		t.Errorf("the path reads %q — the home is meant to be a tilde", got.PathText)
	}
	if !got.Cut || !got.EndShown || !got.StartHidden {
		t.Errorf("a long path is not cut at its head: cut %v, the project shown %v, the head hidden %v",
			got.Cut, got.EndShown, got.StartHidden)
	}
	if got.PathRight > got.HeadRight+1 {
		t.Errorf("the path runs past the header: %.0f against %.0f", got.PathRight, got.HeadRight)
	}
	if got.WholeText != "/home/owner/Projects/Pets/service/a-rather-long-group/and-a-subgroup/aacpanel" || got.WholeCut ||
		got.WholeHeight <= got.LineHeight {
		t.Errorf("a tap does not show the whole path: %q, cut %v, %.0f px against %.0f",
			got.WholeText, got.WholeCut, got.WholeHeight, got.LineHeight)
	}
}

// A wide screen has the tokens in and out among the facts of its header, and
// the row under the composer does not repeat them.
func TestTheWideDeckDoesNotRepeatTheTokens(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got chatHeadSeen
	runWideFixture(t, "chathead.html", &got)
	if !got.Deck {
		t.Fatal("the wide screen drew no row under the composer — there is nothing to check")
	}
	if got.DeckUse {
		t.Error("the row under the composer repeats the tokens in and out the wide header already shows")
	}
}
