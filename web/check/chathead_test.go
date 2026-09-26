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
	Wide           bool    `json:"wide"`
}

// On a phone the header of a conversation is one line of who and how: where
// the session works is in its tools, whole and with the home as a tilde, and
// the tokens in and out stand at the left of the row under the composer.
func TestThePhoneToolsSayWhereTheSessionWorks(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: whether the path is cut is the stylesheet's business — run make front first")
	}
	var got chatHeadSeen
	runFixture(t, "chathead.html", &got)
	if got.Path {
		t.Error("the header still carries the path — it is a line of who and how, the path lives in the tools")
	}
	if got.HeadUse {
		t.Error("the tokens in and out are in the header again")
	}
	if !got.DeckUse || !got.DeckUseFirst || got.DeckUseLeftGap > 2 {
		t.Errorf("the tokens in and out are not at the left of the row under the composer: shown %v, first %v, %.0f px in",
			got.DeckUse, got.DeckUseFirst, got.DeckUseLeftGap)
	}
	if got.PathText != "~/Projects/Pets/service/a-rather-long-group/and-a-subgroup/aacpanel" {
		t.Errorf("the tools say the session works in %q — the home is meant to be a tilde", got.PathText)
	}
	if got.Cut {
		t.Error("the path is cut in the tools, where there is room for all of it")
	}
	if got.Wide {
		t.Error("the screen is wider than the phone: something in the conversation does not fit its width")
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
