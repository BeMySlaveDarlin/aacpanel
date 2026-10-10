package check

import (
	"reflect"
	"testing"
)

// A question with drawings asked by a session on the stream: the answer is
// structure there, so a free answer and the "discuss" item are open in a
// layout a terminal dialog closes them in, and a note beside the pick goes to
// the panel together with it.
func TestAStreamQuestionOpensWhatATerminalCannot(t *testing.T) {
	var got struct {
		Own  bool `json:"own"`
		Drop bool `json:"drop"`
		Note bool `json:"note"`
		Sent []struct {
			Kind   string         `json:"kind"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
	}
	runFixture(t, "askstream.html", &got)

	if !got.Own {
		t.Error("the free answer is shut for a stream session: the limit of a terminal dialog was carried over")
	}
	if !got.Drop {
		t.Error("the discuss item is shut for a stream session")
	}
	if !got.Note {
		t.Fatal("no field for a note appeared beside the pick")
	}
	if len(got.Sent) != 1 || got.Sent[0].Kind != "session.answer" {
		t.Fatalf("the panel was asked %+v", got.Sent)
	}
	notes, _ := got.Sent[0].Params["notes"].([]any)
	if len(notes) != 1 || notes[0] != "but keep the search in it" {
		t.Errorf("the note did not go with the answer: %+v", got.Sent[0].Params)
	}
}

// fastCard is what a question card sent when it was answered faster than it
// was drawn, and how many times it said it was answered.
type fastCard struct {
	Sent []struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	} `json:"sent"`
	Answered int `json:"answered"`
}

// answer checks that a card sent one answer, and that it is want.
func (c fastCard) answer(t *testing.T, what string, want map[string]any) {
	t.Helper()
	if len(c.Sent) != 1 || c.Answered != 1 {
		t.Errorf("%s: the panel was asked %d times and the card answered %d: %+v", what, len(c.Sent), c.Answered, c.Sent)
		return
	}
	if c.Sent[0].Kind != "session.answer" || c.Sent[0].Target != "acme" {
		t.Errorf("%s: the panel was asked %+v", what, c.Sent[0])
	}
	if !reflect.DeepEqual(c.Sent[0].Params, want) {
		t.Errorf("%s: the answer went as %v, want %v", what, c.Sent[0].Params, want)
	}
}

// Inputs that come faster than a question card is drawn all reach the answer,
// and the card sends it once. Two options of a multiple choice tapped in one
// go stay picked, and a third tap, a note and Send in one go send the three
// picks with the note; Send tapped twice in one go sends one answer. A form of
// an MCP server keeps two picks of one field, a switch flipped twice and words
// typed in the same go as Send, and sends them once. An input built on the
// values of the last drawing drops the one before it, and a Send that reads
// whether an answer is going from the last drawing sends a second.
func TestInputsFasterThanTheCardAllReachTheAnswer(t *testing.T) {
	var got struct {
		First []int    `json:"first"`
		Round fastCard `json:"round"`
		Twice fastCard `json:"twice"`
		Form  fastCard `json:"form"`
	}
	runFixture(t, "askfast.html", &got)

	if !reflect.DeepEqual(got.First, []int{1, 3}) {
		t.Errorf("after two taps in one go the card holds %v picked, not 1 and 3", got.First)
	}
	got.Round.answer(t, "the round", map[string]any{
		"ask":   "toolu_ask_fast",
		"picks": []any{[]any{1.0, 3.0, 2.0}},
		"notes": []any{"and the shell scripts"},
	})
	got.Twice.answer(t, "Send twice", map[string]any{
		"ask":   "toolu_ask_twice",
		"picks": []any{[]any{1.0}},
	})
	got.Form.answer(t, "the form", map[string]any{
		"ask":   "elicit-fast",
		"picks": []any{[]any{1.0, 3.0}, []any{}, []any{2.0}},
		"texts": []any{"", "it fails one run in ten", ""},
	})
}
