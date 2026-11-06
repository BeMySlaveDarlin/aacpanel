package check

import "testing"

type askShown struct {
	Open bool `json:"open"`
}

// A question whose options carry a drawing, read on a phone: the button
// beside an option opens its drawing, and the card still answers.
func TestADrawingOfAnOptionOpensBesideItAndTheCardStillAnswers(t *testing.T) {
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

	if !got.Wide.Open || !got.Narrow.Open {
		t.Fatalf("the drawings of the options did not open (first %v, second %v): the button beside them leads nowhere",
			got.Wide.Open, got.Narrow.Open)
	}

	// And the card still answers: a drawing beside an option is something to
	// read before picking, not instead of picking.
	if got.Off {
		t.Fatalf("the button under the options is dead with an option picked, and it reads %q", got.Label)
	}
	if len(got.Sent) != 1 {
		t.Fatalf("the picked option left %d requests behind: %+v", len(got.Sent), got.Sent)
	}
	if got.Sent[0].Kind != "session.answer" || got.Sent[0].Target != "acme" {
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
