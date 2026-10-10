package check

import (
	"reflect"
	"strings"
	"testing"
)

type questionCardShot struct {
	Head    string   `json:"head"`
	Titles  []string `json:"titles"`
	Options []string `json:"options"`
	Answer  string   `json:"answer"`
}

// A question codex asked without waiting is a card of the feed with its
// options, as a question of claude's offers them. The one the person answered
// says what it was answered with — the message that came after it — and
// offers nothing; the one that hangs offers its options, and a tap on one
// sends it as a message answering that question, by the id codex gave it, so
// it goes into the turn that runs. The head of the thread says codex asks and
// works on, not that it waits.
func TestAQuestionCodexDidNotWaitForIsACardAnsweredByATap(t *testing.T) {
	var got struct {
		Before []questionCardShot `json:"before"`
		Head   string             `json:"head"`
		Sent   []struct {
			Kind   string            `json:"kind"`
			Target string            `json:"target"`
			Params map[string]string `json:"params"`
		} `json:"sent"`
		After questionCardShot `json:"after"`
	}
	runFixture(t, "codexquestion.html", &got)

	want := []questionCardShot{
		{Head: "codex asked", Titles: []string{"Which theme should the page take?"}, Answer: "answered Dark"},
		{Head: "codex asks, and goes on", Titles: []string{"Should the logo stay in the header?"},
			Options: []string{"Keep it", "Drop it"}},
	}
	if len(got.Before) != 2 || !reflect.DeepEqual(got.Before[0].Titles, want[0].Titles) ||
		got.Before[0].Head != want[0].Head || got.Before[0].Answer != want[0].Answer || len(got.Before[0].Options) != 0 {
		t.Errorf("the answered question reads %+v", got.Before)
	} else if !reflect.DeepEqual(got.Before[1], want[1]) {
		t.Errorf("the hanging question reads %+v, want %+v", got.Before[1], want[1])
	}
	if !strings.Contains(got.Head, "asks you") {
		t.Errorf("the head of a thread that asked and works on reads %q", got.Head)
	}
	if len(got.Sent) != 1 || got.Sent[0].Kind != "session.send" || got.Sent[0].Target != "codex-0000abcd" ||
		!reflect.DeepEqual(got.Sent[0].Params, map[string]string{"text": "Drop it", "asked": "call_ask_logo"}) {
		t.Errorf("the tap sent %+v: the option as the answer to that question", got.Sent)
	}
	if got.After.Head != "the answer is on its way" ||
		!reflect.DeepEqual(got.After.Options, []string{"Keep it", "Drop it (on)"}) {
		t.Errorf("the question after the tap reads %+v", got.After)
	}
}
