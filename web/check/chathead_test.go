package check

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestLimitStoppedSessionsShowTheirResetAndWaitAfterIt(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Limits struct {
					Seen  []struct{ Name, Expected, Phone, PhoneTone, PhoneColour, Desk, DeskTone, DeskDot, DeskColour, Head, HeadTone, HeadColour, Orbit, Warning string }
					Folds []struct {
						Reset                                                 bool
						First, FirstShown, Phone, Desk, PhoneWaits, DeskWaits string
						Rank                                                  int
					}
				}
			}
			screen.run(t, "chathead.html", &got)
			if len(got.Limits.Seen) != 10 || len(got.Limits.Folds) != 2 {
				t.Fatalf("limit states were not exercised: %+v", got.Limits)
			}
			for _, state := range got.Limits.Seen {
				tone, headTone := "limit", "limit"
				if state.Expected == "stopped by the limit" {
					tone, headTone = "wait", "waiting"
				}
				if state.Name == "unmarked" {
					tone, headTone = "idle", "idle"
				}
				if state.Phone != state.Expected || state.Desk != state.Expected || state.Head != state.Expected || state.Orbit != state.Expected {
					t.Errorf("%s: inconsistent status words: %+v", state.Name, state)
				}
				if !strings.Contains(state.PhoneTone, "pj-"+tone) || state.DeskTone != tone || state.HeadTone != headTone ||
					!strings.Contains(state.DeskDot, "dk"+headTone) {
					t.Errorf("%s: inconsistent status tones: %+v", state.Name, state)
				}
				if tone == "limit" && (state.PhoneColour != state.Warning || state.HeadColour != state.Warning ||
					(screen.name == "desk" && state.DeskColour != state.Warning)) {
					t.Errorf("%s: the limit is not painted as a warning: %+v", state.Name, state)
				}
			}
			for _, fold := range got.Limits.Folds {
				tone, first, rank := "limit", "quiet", 2
				if fold.Reset {
					tone, first, rank = "wait", "limited", 0
				}
				if fold.Phone != tone || fold.Desk != tone || fold.First != first || fold.FirstShown != first || fold.Rank != rank {
					t.Errorf("a folded limit stopped session lost its state or priority: %+v", fold)
				}
				if fold.Reset && (!strings.Contains(fold.PhoneWaits, "child waits") || !strings.Contains(fold.DeskWaits, "child waits")) {
					t.Errorf("the reset session is hidden without a waiting mark: %+v", fold)
				}
			}
		})
	}
}

type chatHeadSeen struct {
	Deck            bool    `json:"deck"`
	HeadUse         bool    `json:"headUse"`
	HeadPath        string  `json:"headPath"`
	HeadPathCut     bool    `json:"headPathCut"`
	HeadTailShown   bool    `json:"headTailShown"`
	HeadPathBelow   bool    `json:"headPathBelow"`
	HeadPathWhole   bool    `json:"headPathWhole"`
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

type headRenameSeen struct {
	Rename struct {
		Value           string   `json:"value"`
		Selected        bool     `json:"selected"`
		SameOff         bool     `json:"sameOff"`
		SameSent        int      `json:"sameSent"`
		FontMatches     bool     `json:"fontMatches"`
		NameHeight      float64  `json:"nameHeight"`
		FieldHeight     float64  `json:"fieldHeight"`
		Before          float64  `json:"before"`
		Editing         float64  `json:"editing"`
		Invalid         float64  `json:"invalid"`
		ControlsVisible bool     `json:"controlsVisible"`
		BadOff          bool     `json:"badOff"`
		BadWhy          string   `json:"badWhy"`
		BadSent         int      `json:"badSent"`
		TakenOff        bool     `json:"takenOff"`
		TakenWhy        string   `json:"takenWhy"`
		EmptyOff        bool     `json:"emptyOff"`
		EmptyWhy        string   `json:"emptyWhy"`
		Escaped         bool     `json:"escaped"`
		Cancelled       bool     `json:"cancelled"`
		Busy            bool     `json:"busy"`
		Once            bool     `json:"once"`
		Refused         string   `json:"refused"`
		RefusedWhy      string   `json:"refusedWhy"`
		ErrorCleared    bool     `json:"errorCleared"`
		Saved           string   `json:"saved"`
		Another         string   `json:"another"`
		Followed        bool     `json:"followed"`
		Sent            []string `json:"sent"`
		Blocked         []struct {
			Kind   string `json:"kind"`
			Text   bool   `json:"text"`
			Cursor string `json:"cursor"`
		} `json:"blocked"`
		Codex struct {
			Value     string   `json:"value"`
			TakenOff  bool     `json:"takenOff"`
			SpacedOff bool     `json:"spacedOff"`
			Saved     string   `json:"saved"`
			Sent      []string `json:"sent"`
		} `json:"codex"`
	} `json:"rename"`
}

func TestTheConversationHeaderRenamesInPlace(t *testing.T) {
	for _, screen := range []string{"phone", "desk"} {
		t.Run(screen, func(t *testing.T) {
			var seen headRenameSeen
			if screen == "phone" {
				runFixture(t, "chathead.html", &seen)
			} else {
				runWideFixture(t, "chathead.html", &seen)
			}
			got := seen.Rename
			if got.Value != "helios" || !got.Selected || !got.SameOff || got.SameSent != 0 {
				t.Errorf("the name did not open selected and unchanged: value %q, selected %v, save off %v, sent %d",
					got.Value, got.Selected, got.SameOff, got.SameSent)
			}
			if !got.FontMatches || math.Abs(got.NameHeight-got.FieldHeight) > .5 {
				t.Errorf("the name changed typography: same font %v, name %.2f px, field %.2f px", got.FontMatches, got.NameHeight, got.FieldHeight)
			}
			if got.Before == 0 || math.Abs(got.Before-got.Editing) > .5 || math.Abs(got.Before-got.Invalid) > .5 {
				t.Errorf("the header jumps: before %.2f px, editing %.2f px, invalid %.2f px", got.Before, got.Editing, got.Invalid)
			}
			t.Logf("header height: before %.2f px, editing %.2f px, invalid %.2f px", got.Before, got.Editing, got.Invalid)
			if !got.ControlsVisible {
				t.Error("the save and cancel icons are not visible without hovering")
			}
			if !got.BadOff || !strings.Contains(got.BadWhy, "latin") || got.BadSent != 0 {
				t.Errorf("an invalid name can be saved: save off %v, reason %q, sent %d", got.BadOff, got.BadWhy, got.BadSent)
			}
			if !got.TakenOff || !strings.Contains(got.TakenWhy, "already") || !got.EmptyOff || got.EmptyWhy != "Enter a name." {
				t.Errorf("taken or empty names are allowed: taken off %v (%q), empty off %v (%q)", got.TakenOff, got.TakenWhy, got.EmptyOff, got.EmptyWhy)
			}
			if !got.Escaped || !got.Cancelled {
				t.Errorf("cancel changed or submitted the name: Escape %v, cross %v", got.Escaped, got.Cancelled)
			}
			if !got.Busy || !got.Once || got.Refused != "helios-pilot" || !strings.Contains(got.RefusedWhy, "reserved") || !got.ErrorCleared {
				t.Errorf("a pending or refused rename lost its state: busy %v, sent once %v, value %q, reason %q, cleared %v",
					got.Busy, got.Once, got.Refused, got.RefusedWhy, got.ErrorCleared)
			}
			want := `session.rename:helios:{"name":"helios-pilot"}|session.rename:helios:{"name":"helios-pilot-2"}`
			if strings.Join(got.Sent, "|") != want || got.Saved != "helios-pilot-2" || !got.Followed {
				t.Errorf("Enter and the check did not rename and follow the session: sent %v, saved %q, followed %v", got.Sent, got.Saved, got.Followed)
			}
			if got.Another != "shop" {
				t.Errorf("the header kept the renamed session's name for another session: %q", got.Another)
			}
			if len(got.Blocked) != 3 {
				t.Fatalf("only %d kinds of read-only names were checked", len(got.Blocked))
			}
			for _, blocked := range got.Blocked {
				if !blocked.Text || blocked.Cursor == "pointer" {
					t.Errorf("%s name offers renaming: plain text %v, cursor %q", blocked.Kind, blocked.Text, blocked.Cursor)
				}
			}
			// A codex thread's title is any line: a name another session
			// answers to and spaces do not hold it, and the session keeps its address.
			c := got.Codex
			wantCodex := `session.rename:helios:{"name":"CVE fix, round two"}`
			if c.Value != "cve fix" || c.TakenOff || c.SpacedOff || c.Saved != "CVE fix, round two" ||
				strings.Join(c.Sent, "|") != wantCodex {
				t.Errorf("the codex title was not renamed in place: %+v", c)
			}
		})
	}
}

// On a phone the header of a conversation says where the session works on a
// line of its own under the line of how it stands, which has no room left for
// it: the home as a tilde, and a path too long for the line cut at its start,
// so the project at its end stays in sight. The tools hold
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
	if !got.HeadPathBelow || !got.HeadPathWhole {
		t.Errorf("the path is not on a line of its own under how the session stands (below %v, the width of the header %v) — "+
			"the line of the agent, the model and the state has no room left for it", got.HeadPathBelow, got.HeadPathWhole)
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
