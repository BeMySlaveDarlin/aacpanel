package check

import (
	"reflect"
	"testing"
)

type answerCardShot struct {
	Head    string   `json:"head"`
	Options []string `json:"options"`
	Answer  string   `json:"answer"`
}

// Words of one's own typed in the composer while a question codex asked
// without waiting hangs are its answer, as a tap on an option is: they go
// naming the question, so the host gives them to the turn that runs rather
// than to the panel's queue behind it. The field says so before they are
// typed, the card closes with them as they leave, and the row under them says
// they went as the answer, with no way back from a queue they never entered.
// The question answered, the composer is as it always is: the next message
// goes as a message, into the queue and with its way back.
func TestWordsTypedWhileACodexQuestionHangsGoAsItsAnswer(t *testing.T) {
	var got struct {
		Asks struct {
			Card        answerCardShot `json:"card"`
			Placeholder string         `json:"placeholder"`
		} `json:"asks"`
		Answered struct {
			Card        answerCardShot `json:"card"`
			Placeholder string         `json:"placeholder"`
			Stamp       string         `json:"stamp"`
			TakeBack    int            `json:"takeBack"`
		} `json:"answered"`
		After struct {
			Card     answerCardShot `json:"card"`
			Stamp    string         `json:"stamp"`
			TakeBack int            `json:"takeBack"`
		} `json:"after"`
		Sent []struct {
			Kind   string            `json:"kind"`
			Target string            `json:"target"`
			Params map[string]string `json:"params"`
		} `json:"sent"`
	}
	runFixture(t, "codexanswer.html", &got)

	if got.Asks.Card.Head != "codex asks, and goes on" ||
		!reflect.DeepEqual(got.Asks.Card.Options, []string{"Keep it", "Drop it"}) {
		t.Errorf("the hanging question reads %+v", got.Asks.Card)
	}
	if got.Asks.Placeholder != "Answer the question — it goes into the turn that runs" {
		t.Errorf("the field over a hanging question says %q", got.Asks.Placeholder)
	}

	if len(got.Sent) != 2 {
		t.Fatalf("the composer sent %+v, expected the answer and a message after it", got.Sent)
	}
	answer := got.Sent[0]
	if answer.Kind != "session.send" || answer.Target != "codex-0000abcd" ||
		answer.Params["text"] != "Drop it, the title is enough" || answer.Params["asked"] != "call_ask_logo" ||
		answer.Params["messageId"] == "" || len(answer.Params) != 3 {
		t.Errorf("the words typed over the hanging question went as %+v: the answer to that question, named", answer)
	}
	if got.Answered.Card.Head != "codex asked" || len(got.Answered.Card.Options) != 0 ||
		got.Answered.Card.Answer != "answered Drop it, the title is enough" {
		t.Errorf("the question once the answer left reads %+v", got.Answered.Card)
	}
	if got.Answered.Stamp != "went as the answer" || got.Answered.TakeBack != 0 {
		t.Errorf("the answer on its way says %q with %d ways back from the queue", got.Answered.Stamp, got.Answered.TakeBack)
	}
	if got.Answered.Placeholder != "Goes after the turn" {
		t.Errorf("the field once the question is answered says %q", got.Answered.Placeholder)
	}

	next := got.Sent[1]
	if next.Kind != "session.send" || next.Params["text"] != "And make the footer dark" ||
		next.Params["asked"] != "" || next.Params["messageId"] == "" {
		t.Errorf("the message after the answer went as %+v: a message of the queue", next)
	}
	if got.After.Stamp != "queued" || got.After.TakeBack != 1 {
		t.Errorf("the message after the answer says %q with %d ways back", got.After.Stamp, got.After.TakeBack)
	}
	if got.After.Card.Answer != "answered Drop it, the title is enough" {
		t.Errorf("a second message answered the question again: %+v", got.After.Card)
	}
}
