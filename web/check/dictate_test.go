package check

import (
	"strings"
	"testing"
)

// Dictation hangs off the send button and nothing else: no second button
// appears in the composer. A press sends as it always did; a press held past
// the threshold opens the microphone until the finger comes off, and the words
// land in the field rather than in the session — a phrase heard wrong must be
// readable before it goes.
// Dictation hangs off the send button and nothing else: no second button
// appears in the composer. An empty field has nothing to send, so the button is
// a switch there — one press starts listening, the next stops it, and what was
// heard stays in the field. A field with words in it is the send button it has
// always been: a press sends, and only a press held past the threshold talks,
// putting the new words after the old.
func TestTheSendButtonTalksWhenThereIsNothingToSend(t *testing.T) {
	type shot struct {
		Icon  string `json:"icon"`
		Cls   string `json:"cls"`
		Off   bool   `json:"off"`
		Title string `json:"title"`
	}
	var got struct {
		Empty   shot `json:"empty"`
		Started struct {
			Live   bool   `json:"live"`
			Starts int    `json:"starts"`
			Cls    string `json:"cls"`
			Sent   int    `json:"sent"`
		} `json:"started"`
		Midway    string `json:"midway"`
		Heard     string `json:"heard"`
		WhileLive shot   `json:"whileLive"`
		Stopped   struct {
			Live bool   `json:"live"`
			Text string `json:"text"`
			Sent int    `json:"sent"`
			Cls  string `json:"cls"`
		} `json:"stopped"`
		WithText shot `json:"withText"`
		Tapped   struct {
			Sent   int    `json:"sent"`
			Last   string `json:"last"`
			Text   string `json:"text"`
			Starts int    `json:"starts"`
		} `json:"tapped"`
		Held struct {
			Live   bool `json:"live"`
			Starts int  `json:"starts"`
		} `json:"held"`
		After struct {
			Live bool   `json:"live"`
			Text string `json:"text"`
			Sent int    `json:"sent"`
		} `json:"after"`
		Slow struct {
			Live   bool `json:"live"`
			Starts int  `json:"starts"`
		} `json:"slow"`
	}
	runFixture(t, "dictate.html", &got)

	if got.Empty.Off {
		t.Error("with dictation on and the field empty the button is disabled — a disabled button takes no press, " +
			"and an empty field is exactly when a person wants to talk")
	}
	if !strings.Contains(got.Empty.Title, "talk") {
		t.Errorf("the empty button says %q — nothing tells the person it listens", got.Empty.Title)
	}

	if !got.Started.Live || got.Started.Starts != 1 {
		t.Fatalf("one press on the empty button started listening %d times, live=%v — the switch does not switch on",
			got.Started.Starts, got.Started.Live)
	}
	if got.Started.Sent != 0 {
		t.Errorf("the press that starts listening also sent %d messages", got.Started.Sent)
	}
	if !strings.Contains(got.Started.Cls, "hearing") {
		t.Errorf("while listening the button wears %q — an open microphone that looks shut is the worst state it can be in", got.Started.Cls)
	}

	if got.Midway != "one" {
		t.Errorf("a piece still being made out landed in the field as %q — what is written changes under the "+
			"thumb while the person is still speaking", got.Midway)
	}
	if got.Heard != "one two three four five" {
		t.Errorf("a count of five said once landed as %q — a phone restates the whole sentence each time it "+
			"hears more of it, and a reader that adds the restatements up writes the beginning over and over",
			got.Heard)
	}
	if !strings.Contains(got.WhileLive.Title, "stop") {
		t.Errorf("while listening the button says %q — it does not say how to stop", got.WhileLive.Title)
	}

	if got.Stopped.Live {
		t.Error("the second press did not stop the microphone — the switch only switches one way")
	}
	if got.Stopped.Text != got.Heard {
		t.Errorf("stopping left %q in the field instead of %q — what was dictated has to stay when the "+
			"microphone closes", got.Stopped.Text, got.Heard)
	}
	if got.Stopped.Sent != 0 {
		t.Errorf("the press that stops listening also sent %d messages", got.Stopped.Sent)
	}
	if strings.Contains(got.Stopped.Cls, "hearing") {
		t.Errorf("the button still looks like it is listening: %q", got.Stopped.Cls)
	}

	if got.WithText.Icon == got.Empty.Icon {
		t.Error("the button draws the same glyph empty and with words in the field — the microphone and the arrow are the same picture")
	}
	if got.Tapped.Starts != 1 {
		t.Errorf("a press with words in the field started listening (%d starts in all) — with something to send, "+
			"a press sends", got.Tapped.Starts)
	}
	if got.Tapped.Sent != 1 || got.Tapped.Last != got.Heard {
		t.Errorf("a press with words in the field sent %d messages, the last %q — ordinary sending broke",
			got.Tapped.Sent, got.Tapped.Last)
	}

	if !got.Held.Live || got.Held.Starts != 2 {
		t.Fatalf("holding the button with words in the field did not talk: live=%v, %d starts in all",
			got.Held.Live, got.Held.Starts)
	}
	if got.After.Live {
		t.Error("letting go left the microphone open")
	}
	if got.After.Text != "written by hand and then said" {
		t.Errorf("a held dictation left %q — the new words go after the ones already written", got.After.Text)
	}
	if got.After.Sent != 1 {
		t.Errorf("letting go after a dictation sent a message (%d in all) — the click that follows the release "+
			"must not send, a phrase heard wrong has to be readable first", got.After.Sent)
	}

	if !got.Slow.Live || got.Slow.Starts != 3 {
		t.Errorf("a slow press on the empty button left listening=%v after %d starts in all — an empty field is "+
			"a switch however long the press is held, and a hold that starts and stops under the finger makes it "+
			"unreliable for anyone who presses slowly", got.Slow.Live, got.Slow.Starts)
	}
}

// A dictated piece joins what is already written the way a person would type
// it: one space between words, no space in front of an empty field, and
// nothing at all for a piece the recognition returned empty.
func TestADictatedPieceJoinsWhatIsWritten(t *testing.T) {
	got := runModuleJS(t, "src/screens/chat/dictate.js", "join", [][]any{
		{"", "one word"},
		{"already written", "and dictated"},
		{"with a tail   ", "then more"},
		{"nothing was said", "   "},
		{"", ""},
		{nil, "from an empty field"},
	})
	want := []string{
		"one word",
		"already written and dictated",
		"with a tail then more",
		"nothing was said",
		"",
		"from an empty field",
	}
	for i, expected := range want {
		if s, _ := got[i].(string); s != expected {
			t.Errorf("case %d joined to %q, expected %q", i, got[i], expected)
		}
	}
}

// A phone does not hand over the pieces of a sentence: it hands over the
// sentence again each time it hears more of it, and sometimes hears it worse
// than the time before. What the dictation keeps has to survive both.
func TestTheDictationKeepsTheSentenceItHeard(t *testing.T) {
	got := runModuleJS(t, "src/screens/chat/dictate.js", "merge", [][]any{
		{"", "one"},                  // the first piece
		{"one", "one two"},           // the same sentence, heard further
		{"one two", "one two"},       // the same sentence, again
		{"one two three", "one two"}, // the same sentence, heard worse
		{"one", "two"},               // words of its own
		{"one", ""},                  // nothing was made out
		{"one", "oneself speaks"},    // a word that merely starts the same
	})
	want := []string{
		"one",
		"one two",
		"one two",
		"one two three",
		"one two",
		"one",
		"one oneself speaks",
	}
	for i, expected := range want {
		if s, _ := got[i].(string); s != expected {
			t.Errorf("case %d kept %q, expected %q", i, got[i], expected)
		}
	}
}
