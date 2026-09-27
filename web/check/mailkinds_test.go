package check

import (
	"strings"
	"testing"
)

type mailRow struct {
	Cls  string `json:"cls"`
	From string `json:"from"`
	Kind string `json:"kind"`
	Peek string `json:"peek"`
	Rail string `json:"rail"`
	Open bool   `json:"open"`
}

type mailShot struct {
	Rows   []mailRow `json:"rows"`
	Opened struct {
		Open bool   `json:"open"`
		Body string `json:"body"`
	} `json:"opened"`
}

// A session next door, a subagent of this session and a hook of it all arrive
// among the prompts wrapped in a preamble that says the same thing every time.
// The feed draws the three as letters — one line saying who, the words under
// it — so what is read is what was said rather than the wrapping.
func TestTheFeedDrawsEveryKindOfLetterAsALetter(t *testing.T) {
	var shot mailShot
	runFixture(t, "mailkinds.html", &shot)

	if len(shot.Rows) != 3 {
		t.Fatalf("%d letters drawn, wanted three: %+v", len(shot.Rows), shot.Rows)
	}
	for i, want := range []struct{ kind, from string }{
		{"session", "harness-rework"},
		{"agent", "aecca89632fbfe14e"},
		{"hook", "router"},
	} {
		got := shot.Rows[i]
		if got.Kind != want.kind {
			t.Errorf("letter %d is headed %q, wanted %q", i, got.Kind, want.kind)
		}
		if got.From != want.from {
			t.Errorf("letter %d comes from %q, wanted %q", i, got.From, want.from)
		}
		if !strings.Contains(got.Cls, want.kind) {
			t.Errorf("letter %d carries the classes %q, and %q is not among them — the rail cannot differ",
				i, got.Cls, want.kind)
		}
		if got.Peek == "" {
			t.Errorf("letter %d shows no peek: a closed letter says nothing about itself", i)
		}
	}

	// The hook is not a correspondent, and its rail says so.
	if shot.Rows[2].Rail == shot.Rows[0].Rail {
		t.Errorf("the hook and the neighbour session are drawn with the same rail (%q) — "+
			"a blocked turn reads as a letter", shot.Rows[2].Rail)
	}

	if !shot.Opened.Open {
		t.Fatal("a press did not open the letter")
	}
	if !strings.Contains(shot.Opened.Body, "adapter-contracts") {
		t.Errorf("the opened letter does not carry what was said: %q", shot.Opened.Body)
	}
}

type lostShot struct {
	Rows []struct {
		From      string `json:"from"`
		Mark      string `json:"mark"`
		MarkColor string `json:"markColor"`
		Peek      string `json:"peek"`
		Why       string `json:"why"`
	} `json:"rows"`
	QuietColor string `json:"quietColor"`
	Opened     struct {
		Why  string `json:"why"`
		Body string `json:"body"`
	} `json:"opened"`
}

// A letter is drawn when it is sent, and claude may answer that it reached
// nobody. The letter that went nowhere says so on its head, in the colour the
// panel warns in, and gives claude's reason once opened; the letter that went
// says nothing of the kind.
func TestALetterThatReachedNobodySaysSo(t *testing.T) {
	var shot lostShot
	runFixture(t, "maillost.html", &shot)

	if len(shot.Rows) != 2 {
		t.Fatalf("%d letters drawn, wanted two: %+v", len(shot.Rows), shot.Rows)
	}
	lost, sent := shot.Rows[0], shot.Rows[1]
	if lost.Mark != "not delivered" {
		t.Errorf("the refused letter is headed %q instead of saying it was not delivered — it reads as sent", lost.Mark)
	}
	if lost.MarkColor == "" || lost.MarkColor == shot.QuietColor {
		t.Errorf("the mark of the refused letter is drawn in %q, the colour of the quiet words beside it", lost.MarkColor)
	}
	if lost.Peek != "salta" {
		t.Errorf("the closed refused letter peeks %q instead of its words", lost.Peek)
	}
	if lost.Why != "" {
		t.Errorf("the reason stands on a closed letter: %q", lost.Why)
	}
	if sent.Mark != "" || sent.Why != "" {
		t.Errorf("the delivered letter is marked as lost: %+v", sent)
	}
	if !strings.Contains(shot.Opened.Why, "No agent named 'coordinator' is reachable.") {
		t.Errorf("the opened refused letter does not give claude's reason: %q", shot.Opened.Why)
	}
	if !strings.Contains(shot.Opened.Body, "salta") {
		t.Errorf("the opened refused letter lost its words: %q", shot.Opened.Body)
	}
}
