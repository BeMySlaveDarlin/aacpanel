package check

import (
	"strings"
	"testing"
)

type permittedRow struct {
	Tool    string `json:"tool"`
	Subject string `json:"subject"`
	Mono    string `json:"mono"`
	Answer  string `json:"answer"`
	Colour  string `json:"colour"`
}

type permittedShot struct {
	Cards []struct {
		Asked   bool           `json:"asked"`
		Label   string         `json:"label"`
		Stamped bool           `json:"stamped"`
		Rows    []permittedRow `json:"rows"`
	} `json:"cards"`
}

// A permission answered on the stream leaves nothing in the transcript but the
// call and its result. The feed shows the answer the way it shows an answered
// question: what was asked about, and what the person said.
func TestAnAnsweredPermissionReadsLikeAnAnsweredQuestion(t *testing.T) {
	var got permittedShot
	runFixture(t, "permittedrow.html", &got)

	if len(got.Cards) != 3 {
		t.Fatalf("cards drawn: %+v", got.Cards)
	}
	for i, card := range got.Cards {
		if !card.Asked {
			t.Errorf("card %d is not drawn as an answered question", i)
		}
		if !card.Stamped {
			t.Errorf("card %d does not say when it was answered", i)
		}
	}
	if got.Cards[0].Label != "permission" || got.Cards[1].Label != "permissions" {
		t.Errorf("labels: %q, %q", got.Cards[0].Label, got.Cards[1].Label)
	}

	once := got.Cards[0].Rows
	if len(once) != 1 || once[0].Tool != "Bash" || once[0].Subject != "git push origin main" || once[0].Answer != "Allowed" {
		t.Errorf("a call allowed once reads %+v", once)
	}
	if !strings.Contains(strings.ToLower(once[0].Mono), "mono") {
		t.Errorf("what the call was about is not set as code: %q", once[0].Mono)
	}

	together := got.Cards[1].Rows
	if len(together) != 2 {
		t.Fatalf("calls answered together: %+v", together)
	}
	if together[0].Answer != "Allowed, not asked again" {
		t.Errorf("an answer for good reads %q", together[0].Answer)
	}
	if together[1].Answer != "Denied" {
		t.Errorf("a refusal reads %q", together[1].Answer)
	}
	if together[1].Colour == together[0].Colour {
		t.Errorf("a refusal is drawn in the colour of a permission: %s", together[1].Colour)
	}

	bare := got.Cards[2].Rows
	if len(bare) != 1 || bare[0].Tool != "WebFetch" || bare[0].Subject != "" || bare[0].Answer != "Allowed" {
		t.Errorf("a call known only by its tool reads %+v", bare)
	}
}
