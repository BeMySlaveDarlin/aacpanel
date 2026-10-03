package check

import (
	"strings"
	"testing"
)

type mailRow struct {
	Cls    string `json:"cls"`
	Label  string `json:"label"`
	From   string `json:"from"`
	Peek   string `json:"peek"`
	Icon   string `json:"icon"`
	More   bool   `json:"more"`
	Tables int    `json:"tables"`
}

type mailShot struct {
	Error  string    `json:"error"`
	Rows   []mailRow `json:"rows"`
	Opened struct {
		Open   bool   `json:"open"`
		Body   string `json:"body"`
		Tables int    `json:"tables"`
	} `json:"opened"`
}

// A session next door, a subagent of this session and a hook of it all arrive
// among the prompts wrapped in a preamble that says the same thing every time.
// The feed draws the three as cards of the build of the files sent to the
// person — who and when on the head, the first lines of the letter under it,
// the rest behind a row — so what is read is what was said rather than the
// wrapping.
func TestTheFeedDrawsEveryKindOfLetterAsALetter(t *testing.T) {
	var shot mailShot
	runFixture(t, "mailkinds.html", &shot)
	if shot.Error != "" {
		t.Fatal(shot.Error)
	}

	if len(shot.Rows) != 3 {
		t.Fatalf("%d letters drawn, wanted three: %+v", len(shot.Rows), shot.Rows)
	}
	for i, want := range []struct{ kind, label, from, peek string }{
		{"session", "from session", "infra-rework", "Four sets green"},
		{"agent", "from agent", "aecca89632fbfe14e", "Both commands went through, nothing skipped."},
		{"hook", "stop hook", "", "the router did not pass"},
	} {
		got := shot.Rows[i]
		if !strings.Contains(got.Cls, "sent") || !strings.Contains(got.Cls, "k-"+want.kind) {
			t.Errorf("letter %d carries the classes %q: not a card, or its kind is lost", i, got.Cls)
		}
		if got.Label != want.label {
			t.Errorf("letter %d is headed %q, wanted %q", i, got.Label, want.label)
		}
		if got.From != want.from {
			t.Errorf("letter %d is signed %q, wanted %q", i, got.From, want.from)
		}
		if !strings.HasPrefix(got.Peek, want.peek) {
			t.Errorf("letter %d closed says %q: a closed letter says what it is about", i, got.Peek)
		}
	}
	if report := shot.Rows[1]; !report.More || report.Tables != 0 || strings.Contains(report.Peek, "Result") {
		t.Errorf("the closed report shows %q with %d tables, a row to open it: %v — its lead is the first paragraph after the heading",
			report.Peek, report.Tables, report.More)
	}
	if shot.Rows[0].More {
		t.Error("a letter said whole on its card still offers a row to open it")
	}

	// The hook is not a correspondent, and its mark says so.
	if shot.Rows[2].Icon == shot.Rows[0].Icon {
		t.Errorf("the hook and the neighbour session are marked in the same colour (%q) — "+
			"a blocked turn reads as a letter", shot.Rows[2].Icon)
	}

	if !shot.Opened.Open {
		t.Fatal("a press on the row did not open the letter")
	}
	if !strings.Contains(shot.Opened.Body, "45s") || shot.Opened.Tables != 1 {
		t.Errorf("the opened report shows %q with %d tables: the indent it came with broke its table",
			shot.Opened.Body, shot.Opened.Tables)
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
	if lost.Peek != "salta" || lost.From != "coordinator" {
		t.Errorf("the closed refused letter to %q says %q instead of its words", lost.From, lost.Peek)
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
