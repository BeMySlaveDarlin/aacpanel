package check

import "testing"

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
