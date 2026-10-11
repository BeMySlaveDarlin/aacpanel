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

// A question codex asked without waiting is a card of the feed once it is
// answered, saying what it was answered with — the message that came after
// it — and offering nothing. The one that hangs stands over the composer with
// its options, as a question of claude's offers them, and its card is left
// out of the feed meanwhile: the question is offered in one place, wherever
// the person has scrolled. A tap on an option sends it as a message answering
// that question, by the id codex gave it, so it goes into the turn that runs.
// The head of the thread says codex asks and works on, not that it waits.
func TestAQuestionCodexDidNotWaitForIsACardAnsweredByATap(t *testing.T) {
	var got struct {
		Before []questionCardShot `json:"before"`
		Dock   *questionCardShot  `json:"dock"`
		Head   string             `json:"head"`
		Sent   []struct {
			Kind   string            `json:"kind"`
			Target string            `json:"target"`
			Params map[string]string `json:"params"`
		} `json:"sent"`
		After *questionCardShot `json:"after"`
		Cards int               `json:"cards"`
	}
	runFixture(t, "codexquestion.html", &got)

	if len(got.Before) != 1 || got.Before[0].Head != "codex asked" ||
		!reflect.DeepEqual(got.Before[0].Titles, []string{"Which theme should the page take?"}) ||
		got.Before[0].Answer != "answered Dark" || len(got.Before[0].Options) != 0 {
		t.Errorf("the feed holds %+v: the answered question alone, the hanging one stands over the composer", got.Before)
	}
	hanging := questionCardShot{Head: "codex asks, and goes on", Titles: []string{"Should the logo stay in the header?"},
		Options: []string{"Keep it", "Drop it"}}
	if got.Dock == nil || !reflect.DeepEqual(*got.Dock, hanging) {
		t.Errorf("the hanging question reads %+v, want %+v", got.Dock, hanging)
	}
	if !strings.Contains(got.Head, "asks you") {
		t.Errorf("the head of a thread that asked and works on reads %q", got.Head)
	}
	if len(got.Sent) != 1 || got.Sent[0].Kind != "session.send" || got.Sent[0].Target != "codex-0000abcd" ||
		!reflect.DeepEqual(got.Sent[0].Params, map[string]string{"text": "Drop it", "asked": "call_ask_logo"}) {
		t.Errorf("the tap sent %+v: the option as the answer to that question", got.Sent)
	}
	if got.After == nil || got.After.Head != "the answer is on its way" ||
		!reflect.DeepEqual(got.After.Options, []string{"Keep it", "Drop it (on)"}) || got.Cards != 1 {
		t.Errorf("the question after the tap reads %+v, with %d cards in the feed", got.After, got.Cards)
	}
}
