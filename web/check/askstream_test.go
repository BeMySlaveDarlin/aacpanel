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

// Inputs that come faster than the card is drawn all reach the answer: two
// options of a multiple choice tapped in one go stay picked, and a third tap,
// a note and Send in one go send the three picks with the note. An input
// built on the values of the last drawing drops the one before it.
func TestInputsFasterThanTheCardAllReachTheAnswer(t *testing.T) {
	var got struct {
		First []int `json:"first"`
		Sent  []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
	}
	runFixture(t, "askfast.html", &got)

	if !reflect.DeepEqual(got.First, []int{1, 3}) {
		t.Errorf("after two taps in one go the card holds %v picked, not 1 and 3", got.First)
	}
	if len(got.Sent) != 1 || got.Sent[0].Kind != "session.answer" || got.Sent[0].Target != "acme" {
		t.Fatalf("the panel was asked %+v", got.Sent)
	}
	want := map[string]any{
		"ask":   "toolu_ask_fast",
		"picks": []any{[]any{1.0, 3.0, 2.0}},
		"notes": []any{"and the shell scripts"},
	}
	if !reflect.DeepEqual(got.Sent[0].Params, want) {
		t.Errorf("the answer went as %v, want %v", got.Sent[0].Params, want)
	}
}
