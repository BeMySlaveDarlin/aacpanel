package check

import (
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
					Seen []struct {
						Name, Expected, Ends, Phone, PhoneTone, PhoneEnds, Desk, DeskTone, DeskGlyph string
						Head, HeadTone, Orbit                                                        string
					}
					Folds []struct {
						Reset                 bool
						First, FirstShown     string
						PhoneWaits, DeskWaits string
						Rank                  int
					}
				}
			}
			screen.run(t, "chathead.html", &got)
			if len(got.Limits.Seen) != 10 || len(got.Limits.Folds) != 2 {
				t.Fatalf("limit states were not exercised: %+v", got.Limits)
			}
			// One vocabulary: the card on either screen and the graph say the
			// state in the same words; the card says when a limit ends in its
			// corner, the head of the conversation in its word.
			for _, state := range got.Limits.Seen {
				tone, headTone := "limit", "limit"
				if state.Expected == "stopped by the limit" {
					tone, headTone = "wait", "waiting"
				}
				if state.Name == "unmarked" {
					tone, headTone = "idle", "idle"
				}
				head := state.Expected
				if state.Ends != "" {
					head = state.Expected + " · until " + state.Ends
				}
				if state.Phone != state.Expected || state.Desk != state.Expected || state.Head != head || state.Orbit != state.Expected {
					t.Errorf("%s: inconsistent status words: %+v", state.Name, state)
				}
				if state.Ends != "" && state.PhoneEnds != "→"+state.Ends {
					t.Errorf("%s: the card says the limit ends %q, want →%s", state.Name, state.PhoneEnds, state.Ends)
				}
				if state.PhoneTone != tone || state.DeskTone != tone || state.HeadTone != headTone || state.DeskGlyph != tone {
					t.Errorf("%s: inconsistent status tones: %+v", state.Name, state)
				}
			}
			// A child stopped by the limit under a closed fold: the fold says
			// it once the limit has reset and it waits for the person, and the
			// block rises to what needs the person.
			for _, fold := range got.Limits.Folds {
				first, rank, waits := "quiet", 2, ""
				if fold.Reset {
					first, rank, waits = "limited", 0, "child"
				}
				if fold.First != first || fold.FirstShown != first || fold.Rank != rank || fold.PhoneWaits != waits || fold.DeskWaits != waits {
					t.Errorf("a folded limit stopped session lost its state or priority: %+v", fold)
				}
			}
		})
	}
}

type chatHeadSeen struct {
	Deck         bool   `json:"deck"`
	HeadUse      bool   `json:"headUse"`
	HeadPath     string `json:"headPath"`
	DeckUse      bool   `json:"deckUse"`
	DeckUseFirst bool   `json:"deckUseFirst"`
	PathText     string `json:"pathText"`
	StripChips   int    `json:"stripChips"`
}

type headRenameSeen struct {
	Rename struct {
		Value        string   `json:"value"`
		Selected     bool     `json:"selected"`
		SameOff      bool     `json:"sameOff"`
		SameSent     int      `json:"sameSent"`
		BadOff       bool     `json:"badOff"`
		BadWhy       string   `json:"badWhy"`
		BadSent      int      `json:"badSent"`
		TakenOff     bool     `json:"takenOff"`
		TakenWhy     string   `json:"takenWhy"`
		EmptyOff     bool     `json:"emptyOff"`
		EmptyWhy     string   `json:"emptyWhy"`
		Escaped      bool     `json:"escaped"`
		Cancelled    bool     `json:"cancelled"`
		Busy         bool     `json:"busy"`
		Once         bool     `json:"once"`
		Refused      string   `json:"refused"`
		RefusedWhy   string   `json:"refusedWhy"`
		ErrorCleared bool     `json:"errorCleared"`
		Saved        string   `json:"saved"`
		Another      string   `json:"another"`
		Followed     bool     `json:"followed"`
		Sent         []string `json:"sent"`
		Blocked      []struct {
			Kind string `json:"kind"`
			Text bool   `json:"text"`
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
				if !blocked.Text {
					t.Errorf("%s name offers renaming", blocked.Kind)
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

// On a phone the header of a conversation says where the session works, the
// home as a tilde, and so do the tools; the tokens in and out lead the row
// under the composer.
func TestThePhoneToolsSayWhereTheSessionWorks(t *testing.T) {
	var got chatHeadSeen
	runFixture(t, "chathead.html", &got)
	if got.HeadPath != "~/Projects/Side/service/a-rather-long-group/and-a-subgroup/aacpanel" {
		t.Errorf("the header says the session works in %q — it is meant to say where, with the home as a tilde", got.HeadPath)
	}
	if got.HeadUse {
		t.Error("the tokens in and out are in the header again")
	}
	if !got.DeckUse || !got.DeckUseFirst {
		t.Errorf("the tokens in and out do not lead the row under the composer: shown %v, first %v", got.DeckUse, got.DeckUseFirst)
	}
	if got.PathText != "~/Projects/Side/service/a-rather-long-group/and-a-subgroup/aacpanel" {
		t.Errorf("the tools say the session works in %q — the home is meant to be a tilde", got.PathText)
	}
}

// A wide screen keeps what the session has in flight in the composer's band
// rather than in a row of its own under the frame; the tokens in and out stay
// in the session info its panel opens.
func TestTheWideScreenKeepsTheWorkInTheComposerBand(t *testing.T) {
	var got chatHeadSeen
	runWideFixture(t, "chathead.html", &got)
	if got.Deck {
		t.Error("the wide screen still draws a row under the composer — the work chips belong in its band")
	}
	if got.StripChips != 5 {
		t.Fatalf("the composer band holds %d work chips, expected the five: background, agents, workflows, pages, briefs", got.StripChips)
	}
	if got.DeckUse {
		t.Error("the tokens in and out came back on the wide screen, where the session info holds them")
	}
}
