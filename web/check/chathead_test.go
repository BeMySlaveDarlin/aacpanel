package check

import (
	"os"
	"testing"
)

type chatHeadSeen struct {
	Deck            bool    `json:"deck"`
	HeadUse         bool    `json:"headUse"`
	HeadPath        string  `json:"headPath"`
	HeadPathCut     bool    `json:"headPathCut"`
	HeadTailShown   bool    `json:"headTailShown"`
	HeadPathOnLine  bool    `json:"headPathOnLine"`
	DeckUse         bool    `json:"deckUse"`
	DeckUseLeftGap  float64 `json:"deckUseLeftGap"`
	DeckUseFirst    bool    `json:"deckUseFirst"`
	PathText        string  `json:"pathText"`
	Cut             bool    `json:"cut"`
	Wide            bool    `json:"wide"`
	StripChips      int     `json:"stripChips"`
	StripChipTall   float64 `json:"stripChipTall"`
	StripWordsTall  float64 `json:"stripWordsTall"`
	StripChipsRight float64 `json:"stripChipsRight"`
}

// On a phone the header of a conversation says where the session works on the
// line of how it stands: the home as a tilde, and a path too long for the line
// cut at its start, so the project at its end stays in sight. The tools hold
// the path whole, and the tokens in and out stand at the left of the row under
// the composer.
func TestThePhoneToolsSayWhereTheSessionWorks(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: whether the path is cut is the stylesheet's business — run make front first")
	}
	var got chatHeadSeen
	runFixture(t, "chathead.html", &got)
	if got.HeadPath != "~/Projects/Side/service/a-rather-long-group/and-a-subgroup/aacpanel" {
		t.Errorf("the header says the session works in %q — it is meant to say where, with the home as a tilde", got.HeadPath)
	}
	if !got.HeadPathCut || !got.HeadTailShown {
		t.Errorf("a path longer than the line is not cut at its start (cut %v, its end in sight %v) — the project at its end is what tells one session from another",
			got.HeadPathCut, got.HeadTailShown)
	}
	if !got.HeadPathOnLine {
		t.Error("the path took a line of its own — it stands on the line of how the session stands")
	}
	if got.HeadUse {
		t.Error("the tokens in and out are in the header again")
	}
	if !got.DeckUse || !got.DeckUseFirst || got.DeckUseLeftGap > 2 {
		t.Errorf("the tokens in and out are not at the left of the row under the composer: shown %v, first %v, %.0f px in",
			got.DeckUse, got.DeckUseFirst, got.DeckUseLeftGap)
	}
	if got.PathText != "~/Projects/Side/service/a-rather-long-group/and-a-subgroup/aacpanel" {
		t.Errorf("the tools say the session works in %q — the home is meant to be a tilde", got.PathText)
	}
	if got.Cut {
		t.Error("the path is cut in the tools, where there is room for all of it")
	}
	if got.Wide {
		t.Error("the screen is wider than the phone: something in the conversation does not fit its width")
	}
}

// A wide screen keeps what the session has in flight in the composer's band,
// at its right end and as tall as the words of the model beside it, rather
// than in a row of its own under the frame; the tokens in and out stay in the
// session info its panel opens.
func TestTheWideScreenKeepsTheWorkInTheComposerBand(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got chatHeadSeen
	runWideFixture(t, "chathead.html", &got)
	if got.Deck {
		t.Error("the wide screen still draws a row under the composer — the work chips belong in its band")
	}
	if got.StripChips != 5 {
		t.Fatalf("the composer band holds %d work chips, expected the five: background, agents, workflows, pages, briefs", got.StripChips)
	}
	if got.StripChipsRight > 8 {
		t.Errorf("the work chips stand %.0f px short of the band's right end — they are meant to close it", got.StripChipsRight)
	}
	if got.StripWordsTall > 0 && got.StripChipTall < got.StripWordsTall-1 {
		t.Errorf("a work chip is %.0f px tall beside words of %.0f px — the chips were meant to grow to them", got.StripChipTall, got.StripWordsTall)
	}
	if got.DeckUse {
		t.Error("the tokens in and out came back on the wide screen, where the session info holds them")
	}
}
