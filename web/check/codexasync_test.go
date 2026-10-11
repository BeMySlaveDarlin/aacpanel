package check

import (
	"reflect"
	"testing"
)

// askedCardShot is the card of an answered round as a fixture reads it.
type askedCardShot struct {
	Label string `json:"label"`
	Why   string `json:"why"`
	Rows  []struct {
		Tag   string `json:"tag"`
		Q     string `json:"q"`
		A     string `json:"a"`
		Note  string `json:"note"`
		Faint bool   `json:"faint"`
	} `json:"rows"`
	Stamp string `json:"stamp"`
}

// askScreen is what stands on the screen of a conversation around a question.
type askScreen struct {
	Cards    int    `json:"cards"`
	Bubbles  int    `json:"bubbles"`
	Sheet    bool   `json:"sheet"`
	Composer bool   `json:"composer"`
	Word     string `json:"word"`
}

type codexAsyncRun struct {
	Claude struct {
		Label    string   `json:"label"`
		Skeleton []string `json:"skeleton"`
		Foot     bool     `json:"foot"`
	} `json:"claude"`
	Codex struct {
		Label    string   `json:"label"`
		Skeleton []string `json:"skeleton"`
		Send     string   `json:"send"`
		SendOff  bool     `json:"sendOff"`
		Drop     string   `json:"drop"`
		askScreen
	} `json:"codex"`
	Picked struct {
		Posted  int    `json:"posted"`
		On      []bool `json:"on"`
		Send    string `json:"send"`
		SendOff bool   `json:"sendOff"`
	} `json:"picked"`
	Posted []sentAction `json:"posted"`
	Going  struct {
		Card *askedCardShot `json:"card"`
		askScreen
	} `json:"going"`
	Read struct {
		Card     *askedCardShot `json:"card"`
		Skeleton []string       `json:"skeleton"`
		askScreen
	} `json:"read"`
	ClaudeCard struct {
		Card     *askedCardShot `json:"card"`
		Skeleton []string       `json:"skeleton"`
	} `json:"claudeCard"`
	Two struct {
		Steps int    `json:"steps"`
		Label string `json:"label"`
	} `json:"two"`
	PutAway struct {
		Posted []sentAction   `json:"posted"`
		Card   *askedCardShot `json:"card"`
		askScreen
	} `json:"putAway"`
	Words string `json:"words"`
	Trip  any    `json:"trip"`
	Rows  struct {
		Sheet []string `json:"sheet"`
		Words []string `json:"words"`
		Hangs []string `json:"hangs"`
		Open  []string `json:"open"`
		Going []string `json:"going"`
	} `json:"rows"`
}

const (
	asyncTheme  = "Which theme should the page take?"
	asyncNote   = "keep the contrast high"
	asyncAnswer = "The answer to your question:\n\n" + asyncTheme + "\n   Dark\n   note: " + asyncNote
)

// TestACodexQuestionItDidNotWaitForIsClaudesQuestion runs a question codex
// asked without waiting through the whole screen, beside the question of
// claude's: it stands in the same sheet, built of the same parts, with the
// button to send under it — a pick of codex's question does not go at the
// tap, a note goes beside it — and none of a card or a dock of its own. The
// thread asked nine hours of work before the end of the feed the screen
// holds, and the sheet stands all the same. A pick, a note and Send go as one
// message naming the question, in words codex reads as the answer; the
// message stands where it was sent as the card of the round answered, built
// as claude's answered round is, on its way and once the thread has read it,
// and the composer is back. Two questions put away tell codex the person will
// answer in the conversation, and stand as a round rejected. The words of an
// answer read back as they were written, and the card of a question gives way
// to the answer the sheet wrote, says a message answered it in words of its
// own, goes into the sheet while it hangs and says nobody answered where no
// sheet offers it.
func TestACodexQuestionItDidNotWaitForIsClaudesQuestion(t *testing.T) {
	var got codexAsyncRun
	runFixture(t, "codexasync.html", &got)

	if got.Claude.Label != "Theme" || len(got.Claude.Skeleton) < 10 || got.Claude.Foot {
		t.Fatalf("claude's question reads %q, %d parts, a button under it %v: the fixture shows the wrong sheet",
			got.Claude.Label, len(got.Claude.Skeleton), got.Claude.Foot)
	}
	c := got.Codex
	if !reflect.DeepEqual(c.Skeleton, got.Claude.Skeleton) {
		t.Errorf("codex's question is built otherwise than claude's:\n%q\nwant\n%q", c.Skeleton, got.Claude.Skeleton)
	}
	if c.Label != "codex asks" || c.Send != "Pick an option" || !c.SendOff {
		t.Errorf("codex's sheet is headed %q with %q under it (off %v)", c.Label, c.Send, c.SendOff)
	}
	if c.Drop != "codex hears that you will answer in the conversation" {
		t.Errorf("putting codex's question away says %q", c.Drop)
	}
	if !c.Sheet || c.Composer || c.Cards != 0 || c.Word != "asks you" {
		t.Errorf("a question above the end of the feed: %+v — the sheet stands in the composer's place, "+
			"the head says the thread asks and works on", c.askScreen)
	}

	p := got.Picked
	if p.Posted != 0 || !reflect.DeepEqual(p.On, []bool{false, true}) || p.Send != "Send" || p.SendOff {
		t.Errorf("after a tap on Dark: %+v — the pick stands, nothing goes until Send", p)
	}

	want := []sentAction{{Kind: "session.send", Target: "codex-0000abcd",
		Params: map[string]any{"text": asyncAnswer, "asked": "call_ask_theme"}}}
	if !reflect.DeepEqual(got.Posted, want) {
		t.Errorf("Send went as %+v, want one message naming the question: %+v", got.Posted, want)
	}

	answered := func(what string, card *askedCardShot, stamp string) {
		t.Helper()
		if card == nil || card.Label != "question" || card.Why != "" || len(card.Rows) != 1 || card.Stamp != stamp {
			t.Fatalf("%s: the card reads %+v", what, card)
		}
		r := card.Rows[0]
		if r.Tag != "" || r.Q != asyncTheme || r.A != "Dark" || r.Note != asyncNote || r.Faint {
			t.Errorf("%s: the row reads %+v", what, r)
		}
	}
	answered("the answer on its way", got.Going.Card, "went as the answer")
	if g := got.Going.askScreen; g.Sheet || !g.Composer || g.Cards != 1 || g.Bubbles != 0 {
		t.Errorf("the answer on its way: %+v — the card stands, the sheet is gone, the composer is back", g)
	}
	answered("the answer read", got.Read.Card, "")
	if r := got.Read.askScreen; r.Sheet || !r.Composer || r.Cards != 1 || r.Bubbles != 0 {
		t.Errorf("the answer read: %+v — one card and no bubble of its words", r)
	}

	cc := got.ClaudeCard.Card
	if cc == nil || len(cc.Rows) != 1 || cc.Rows[0].Tag != "Theme" || cc.Rows[0].A != "Dark" {
		t.Fatalf("claude's answered round reads %+v", cc)
	}
	if !reflect.DeepEqual(got.Read.Skeleton, got.ClaudeCard.Skeleton) {
		t.Errorf("codex's answered round is built otherwise than claude's:\n%q\nwant\n%q",
			got.Read.Skeleton, got.ClaudeCard.Skeleton)
	}

	if got.Two.Steps != 2 || got.Two.Label != "codex asks" {
		t.Errorf("two questions of codex read %+v: a round of two steps", got.Two)
	}
	away := []sentAction{{Kind: "session.send", Target: "codex-0000ef01", Params: map[string]any{
		"text":  "I put your 2 questions away and will answer in the conversation:\n\nAnd the footer?\n\nShould the logo stay?",
		"asked": "call_ask_logo"}}}
	if !reflect.DeepEqual(got.PutAway.Posted, away) {
		t.Errorf("putting the questions away went as %+v, want %+v", got.PutAway.Posted, away)
	}
	if card := got.PutAway.Card; card == nil || card.Label != "questions" || card.Why != "rejected" || len(card.Rows) != 2 ||
		card.Rows[0].A != "" || !card.Rows[0].Faint || card.Rows[1].Q != "Should the logo stay?" {
		t.Errorf("the questions put away read %+v", card)
	}
	if a := got.PutAway.askScreen; a.Sheet || !a.Composer {
		t.Errorf("the questions put away leave %+v", a)
	}

	wantWords := "The answers to your 2 questions:\n\n" +
		"And the footer?\n   Dark\n   note: keep it plain\n   on every page\n\n" +
		"Should the logo stay?\n   Drop it, the title\n   \n   is enough"
	if got.Words != wantWords {
		t.Errorf("the answer of two questions is written\n%q\nwant\n%q", got.Words, wantWords)
	}
	trip := map[string]any{"kind": "asked", "rows": []any{
		map[string]any{"text": "And the footer?", "answer": []any{"Dark"}, "note": "keep it plain\non every page"},
		map[string]any{"text": "Should the logo stay?", "answer": []any{"Drop it, the title\n\nis enough"}},
	}}
	if !reflect.DeepEqual(got.Trip, trip) {
		t.Errorf("the answer reads back as %v, want %v", got.Trip, trip)
	}

	for _, c := range []struct {
		what      string
		got, want []string
	}{
		{"answered from the sheet", got.Rows.Sheet, []string{"me"}},
		{"answered in words of a message", got.Rows.Words, []string{"question:said", "me"}},
		{"hanging in the sheet", got.Rows.Hangs, []string{"ai"}},
		{"hanging where no sheet offers it", got.Rows.Open, []string{"question:open", "ai"}},
		{"answered from the sheet, on its way", got.Rows.Going, []string{}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("a question %s leaves the feed %q, want %q", c.what, c.got, c.want)
		}
	}
}
