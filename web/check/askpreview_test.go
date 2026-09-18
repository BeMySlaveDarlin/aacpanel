package check

import "testing"

type askShown struct {
	Open        bool    `json:"open"`
	Cols        int     `json:"cols"`
	Widest      int     `json:"widest"`
	Lines       int     `json:"lines"`
	Client      int     `json:"client"`
	Scroll      int     `json:"scroll"`
	Size        float64 `json:"size"`
	PlainScroll int     `json:"plainScroll"`
	PlainSize   float64 `json:"plainSize"`
}

// A question whose options carry a drawing, read on a phone. A drawing is made
// of characters standing in columns: it is drawn at a width of its own, and the
// box it is dropped into is the width of the screen. Cut at the right edge it
// stops being a drawing — the half of the mockup that says what the option costs
// is the half that goes. The box has to fit it instead.
func TestADrawingOfAnOptionFitsTheBoxItIsPutIn(t *testing.T) {
	var got struct {
		Wide    askShown `json:"wide"`
		Narrow  askShown `json:"narrow"`
		Label   string   `json:"label"`
		Off     bool     `json:"off"`
		Waiting bool     `json:"waiting"`
		Sent    []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
	}
	runFixture(t, "askpreview.html", &got)

	if !got.Wide.Open {
		t.Fatal("the drawing of the first option did not open: the button beside it leads nowhere")
	}
	if got.Wide.Widest < 70 {
		t.Fatalf("the drawing is %d columns across: the fixture no longer carries a drawing wider than a "+
			"phone, and the check passes on a case that never had the fault", got.Wide.Widest)
	}
	// The box is fitted to the columns it was told to hold, so a wrong count is
	// a wrong fit: taken from elsewhere, or not taken at all.
	if got.Wide.Cols != got.Wide.Widest {
		t.Errorf("the box was told to hold %d columns and the drawing is %d across",
			got.Wide.Cols, got.Wide.Widest)
	}
	// Without the fit the drawing is wider than its box: that is the case being
	// checked, and a fixture where it is not has stopped checking anything.
	if got.Wide.PlainScroll <= got.Wide.Client {
		t.Fatalf("at the plain size the drawing takes %d px in a box of %d: it fits by itself, "+
			"and the fit below proves nothing", got.Wide.PlainScroll, got.Wide.Client)
	}
	if got.Wide.Scroll > got.Wide.Client {
		t.Errorf("the drawing takes %d px in a box of %d — %d px of it stand past the right edge, where a "+
			"finger cannot reach them: a sheet lets the hand pan up and down, not across",
			got.Wide.Scroll, got.Wide.Client, got.Wide.Scroll-got.Wide.Client)
	}

	// A drawing that already fits is shown as it is: fitting everything would
	// shrink the type of every mockup for the sake of the widest one.
	if !got.Narrow.Open {
		t.Fatal("the drawing of the second option did not open")
	}
	if got.Narrow.Size < got.Narrow.PlainSize {
		t.Errorf("a drawing of %d columns is drawn at %.2f px instead of %.2f: it fits the box as it is, "+
			"and shrinking it costs the reader for nothing", got.Narrow.Cols, got.Narrow.Size, got.Narrow.PlainSize)
	}

	// And the card still answers: a drawing beside an option is something to
	// read before picking, not instead of picking.
	if got.Off {
		t.Fatalf("the button under the options is dead with an option picked, and it reads %q", got.Label)
	}
	if len(got.Sent) != 1 {
		t.Fatalf("the picked option left %d requests behind: %+v", len(got.Sent), got.Sent)
	}
	if got.Sent[0].Kind != "session.answer" || got.Sent[0].Target != "evirma" {
		t.Errorf("the answer went out as %q to %q", got.Sent[0].Kind, got.Sent[0].Target)
	}
	if picks, _ := got.Sent[0].Params["picks"].([]any); len(picks) != 1 {
		t.Errorf("the answer carried %v: the question is one, and one round of picks is what it takes",
			got.Sent[0].Params["picks"])
	} else if first, _ := picks[0].([]any); len(first) != 1 || first[0] != float64(2) {
		t.Errorf("the second option was picked and %v went out", picks[0])
	}
	if !got.Waiting {
		t.Error("the card is still on the screen after the answer went out")
	}
}
