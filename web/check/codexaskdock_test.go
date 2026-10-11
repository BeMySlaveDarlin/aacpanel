package check

import (
	"reflect"
	"testing"
)

type askDockShot struct {
	Dock *struct {
		Head    string   `json:"head"`
		At      string   `json:"at"`
		Titles  []string `json:"titles"`
		Options []string `json:"options"`
		Hint    string   `json:"hint"`
		Line    string   `json:"line"`
	} `json:"dock"`
	Word        string `json:"word"`
	Placeholder string `json:"placeholder"`
	Cards       int    `json:"cards"`
}

// A question codex asked without waiting, hours before the end of a thread
// that worked on after it, is not among the rows the screen reads from the
// end of the feed. The row of the thread says it hangs, with its id and its
// options, and the screen offers it over the composer whatever the window
// holds: the head says the thread waits, the field says the words answer the
// question, and a tap on an option goes as the answer, naming the question,
// as the card's tap does. It folds into a line with its question and opens
// again, and it leaves once the row no longer says it hangs. A question with
// no options is answered with words from the composer, and leaves as they go.
func TestACodexQuestionAboveTheFeedStandsOverTheComposer(t *testing.T) {
	var got struct {
		Far, Folded, Opened, Sent, Gone, Plain, Words askDockShot
		Posted                                        []struct {
			Kind   string            `json:"kind"`
			Target string            `json:"target"`
			Params map[string]string `json:"params"`
		} `json:"posted"`
	}
	runFixture(t, "codexaskdock.html", &got)

	far := got.Far
	if far.Dock == nil {
		t.Fatalf("a question above the window of the feed stands nowhere: %+v", far)
	}
	if far.Dock.Head != "codex asks, and goes on" || far.Dock.At != "9 h ago" ||
		!reflect.DeepEqual(far.Dock.Titles, []string{"Which server should the router send from?"}) ||
		!reflect.DeepEqual(far.Dock.Options, []string{"The German one", "The Finnish one", "Both, by turns"}) ||
		far.Dock.Hint != "A tap answers; words of your own go from the composer." {
		t.Errorf("the question over the composer reads %+v", *far.Dock)
	}
	if far.Word != "waiting for you" || far.Cards != 0 {
		t.Errorf("the head says %q with %d cards of the question in the feed", far.Word, far.Cards)
	}
	if far.Placeholder != "Answer the question — it starts a turn" {
		t.Errorf("the field of a free thread over a hanging question says %q", far.Placeholder)
	}

	if got.Folded.Dock == nil || got.Folded.Dock.Line != "Which server should the router send from?" ||
		len(got.Folded.Dock.Options) != 0 {
		t.Errorf("the folded question reads %+v", got.Folded.Dock)
	}
	if got.Opened.Dock == nil || len(got.Opened.Dock.Options) != 3 {
		t.Errorf("the question opened again reads %+v", got.Opened.Dock)
	}

	if len(got.Posted) != 2 {
		t.Fatalf("the screen sent %+v, expected the tap and the words", got.Posted)
	}
	tap := got.Posted[0]
	if tap.Kind != "session.send" || tap.Target != "codex-79e1c76e" ||
		!reflect.DeepEqual(tap.Params, map[string]string{"text": "The German one", "asked": "call_ask_server"}) {
		t.Errorf("the tap went as %+v: the option as the answer to the question the row names", tap)
	}
	if got.Sent.Dock == nil || got.Sent.Dock.Head != "the answer is on its way" ||
		!reflect.DeepEqual(got.Sent.Dock.Options, []string{"The German one (on)", "The Finnish one (off)", "Both, by turns (off)"}) {
		t.Errorf("the question after the tap reads %+v", got.Sent.Dock)
	}
	if got.Gone.Dock != nil || got.Gone.Placeholder != "Write to codex-79e1c76e" || got.Gone.Word == "waiting for you" {
		t.Errorf("the question the row no longer names stays: %+v", got.Gone)
	}

	plain := got.Plain
	if plain.Dock == nil || !reflect.DeepEqual(plain.Dock.Titles, []string{"Should the logo stay?"}) ||
		len(plain.Dock.Options) != 0 || plain.Dock.Hint != "Answer from the composer: the next message is the answer." {
		t.Errorf("the question without options reads %+v", plain.Dock)
	}
	if plain.Placeholder != "Answer the question — it goes into the turn that runs" || plain.Word != "asks you" {
		t.Errorf("the busy thread over its question says %q, the head %q", plain.Placeholder, plain.Word)
	}
	words := got.Posted[1]
	if words.Kind != "session.send" || words.Target != "codex-0000ef01" || words.Params["text"] != "Drop it" ||
		words.Params["asked"] != "call_ask_logo" || words.Params["messageId"] == "" {
		t.Errorf("the words typed over the question went as %+v: the answer to it, named", words)
	}
	if got.Words.Dock != nil || got.Words.Placeholder != "Goes after the turn" {
		t.Errorf("the question answered with words stays: %+v", got.Words)
	}
}
