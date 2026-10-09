package check

import (
	"strings"
	"testing"
)

type secretCard struct {
	Title string `json:"title"`
	Meta  string `json:"meta"`
	Saved string `json:"saved"`
	Fill  string `json:"fill"`
}

type secretSent struct {
	Kind   string            `json:"kind"`
	Target string            `json:"target"`
	Params map[string]string `json:"params"`
}

type secretPad struct {
	Before    []secretCard `json:"before"`
	SavedWord string       `json:"savedWord"`
	Opened    struct {
		Title    string   `json:"title"`
		Value    string   `json:"value"`
		Path     string   `json:"path"`
		Replaces string   `json:"replaces"`
		Attrs    []string `json:"attrs"`
		Spelled  bool     `json:"spelled"`
		Font     string   `json:"font"`
	} `json:"opened"`
	Mask     []string `json:"mask"`
	MaskKept bool     `json:"maskKept"`
	Closed   struct {
		Gone   bool     `json:"gone"`
		Stored []string `json:"stored"`
		Sent   int      `json:"sent"`
		InDom  bool     `json:"inDom"`
	} `json:"closed"`
	Reopened string `json:"reopened"`
	Typed    string `json:"typed"`
	Saved    struct {
		Sent    []secretSent `json:"sent"`
		Confirm bool         `json:"confirm"`
		Closed  bool         `json:"closed"`
		Toast   string       `json:"toast"`
		Card    secretCard   `json:"card"`
		Stored  []string     `json:"stored"`
		InDom   bool         `json:"inDom"`
		Carried []string     `json:"carried"`
	} `json:"saved"`
	AfterSave string `json:"afterSave"`
	Fresh     struct {
		Value    string `json:"value"`
		Path     string `json:"path"`
		Replaces bool   `json:"replaces"`
	} `json:"fresh"`
	BlankOff bool   `json:"blankOff"`
	Error    string `json:"error"`
}

const (
	secretTemplate = "# https://github.com/settings/tokens — a classic token with repo\nGH_TOKEN=\n"
	secretDir      = "/home/u/.local/state/aacpanel/secrets"
	secretMark     = "ghp_Q7marker4f1d9e"
)

// TestSecretNotepad runs the card of a secret and its notepad in a real
// engine, on a phone and at a desk, and reads it part by part: the fixture
// walks the card, the notepad, a text dropped by closing and a text saved.
func TestSecretNotepad(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{
		{"phone", runFixture},
		{"desk", runWideFixture},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got secretPad
			screen.run(t, "secretpad.html", &got)
			if got.Error != "" {
				t.Fatalf("the fixture broke: %s", got.Error)
			}
			checkSecretNotepad(t, got)
		})
	}
}

func checkSecretNotepad(t *testing.T, got secretPad) {
	for _, part := range []struct {
		name  string
		check func(*testing.T, secretPad)
	}{
		{"cardSaysWhatIsAskedAndWhenItWasSaved", secretCardSaysWhatIsAsked},
		{"notepadOpensOnTheTemplate", secretNotepadOpensOnTheTemplate},
		{"hideDrawsDotsOverTheSameText", secretHideDrawsDots},
		{"closingDropsTheText", secretClosingDropsTheText},
		{"saveSendsTheTextOnceAndKeepsNothing", secretSaveSendsOnce},
		{"aNewNameReplacesNothing", secretNewNameReplacesNothing},
	} {
		t.Run(part.name, func(t *testing.T) { part.check(t, got) })
	}
}

// The card names what the session asks for and the file it gets, opens the
// notepad with Fill in, and says saved only for a file written after the
// question: an older file of the same name answered an earlier one.
func secretCardSaysWhatIsAsked(t *testing.T, got secretPad) {
	if len(got.Before) != 2 {
		t.Fatalf("the feed drew %d secret cards, not two", len(got.Before))
	}
	token := got.Before[0]
	if token.Title != "A token for the GitHub CLI" || !strings.Contains(token.Meta, "github-token") || token.Fill != "Fill in" {
		t.Errorf("the card of the token reads %+v", token)
	}
	if token.Saved != "" {
		t.Errorf("a file older than the question marks the card as saved: %q", token.Saved)
	}
	if got.Before[1].Saved != "" {
		t.Errorf("a name the host does not keep is marked as saved: %q", got.Before[1].Saved)
	}
	if got.Saved.Card.Saved != got.SavedWord {
		t.Errorf("after the save the card says %q, not %q", got.Saved.Card.Saved, got.SavedWord)
	}
	for _, card := range append(got.Before, got.Saved.Card) {
		if strings.Contains(card.Meta+card.Title, "GH_TOKEN") {
			t.Errorf("the card shows the template: %+v", card)
		}
	}
}

// Fill in opens the notepad on the session's template, says where the file
// will lie and what it replaces, and the field is one the keyboard of a phone
// leaves alone: no suggestions, no capitals, no spelling.
func secretNotepadOpensOnTheTemplate(t *testing.T, got secretPad) {
	o := got.Opened
	if o.Title != "A token for the GitHub CLI" || o.Value != secretTemplate {
		t.Errorf("the notepad opened as %q on %q", o.Title, o.Value)
	}
	if o.Path != secretDir+"/github-token" {
		t.Errorf("the notepad says the file lies at %q", o.Path)
	}
	if !strings.HasPrefix(o.Replaces, "Replaces the secret saved ") {
		t.Errorf("a name the host keeps does not say what it replaces: %q", o.Replaces)
	}
	want := "autocomplete=off autocorrect=off autocapitalize=off spellcheck=false"
	if strings.Join(o.Attrs, " ") != want || o.Spelled {
		t.Errorf("the field is %v (spellcheck %v), not %s", o.Attrs, o.Spelled, want)
	}
	if !strings.Contains(o.Font, "Mono") && !strings.Contains(o.Font, "monospace") {
		t.Errorf("the field is set in %q, not in a monospace face", o.Font)
	}
}

// The text is seen by default; Hide draws it as dots and Show brings it back,
// the text itself untouched.
func secretHideDrawsDots(t *testing.T, got secretPad) {
	if strings.Join(got.Mask, ",") != "Hide:none,Show:disc,Hide:none" {
		t.Errorf("the toggle went %v", got.Mask)
	}
	if !got.MaskKept {
		t.Error("hiding and showing changed the text")
	}
}

// What is typed and dropped by closing the sheet goes nowhere and stays
// nowhere: the next opening starts from the template.
func secretClosingDropsTheText(t *testing.T, got secretPad) {
	c := got.Closed
	if !c.Gone || c.Sent != 0 {
		t.Errorf("closing the notepad left it open (%v) or sent %d actions", !c.Gone, c.Sent)
	}
	if len(c.Stored) > 0 || c.InDom {
		t.Errorf("the dropped text rests in the page: storage %v, in the document %v", c.Stored, c.InDom)
	}
	if got.Reopened != secretTemplate {
		t.Errorf("opened again the notepad holds %q, not the template", got.Reopened)
	}
	if got.AfterSave != secretTemplate {
		t.Errorf("opened after the save the notepad holds %q, not the template", got.AfterSave)
	}
}

// Save sends one secret.put to the session with the name and the text as
// typed, asks nothing more, closes, and the text is then nowhere in the page:
// it left in the body of that one request and in no address.
func secretSaveSendsOnce(t *testing.T, got secretPad) {
	s := got.Saved
	if len(s.Sent) != 1 {
		t.Fatalf("Save sent %d actions: %+v", len(s.Sent), s.Sent)
	}
	put := s.Sent[0]
	if put.Kind != "secret.put" || put.Target != "helios" || len(put.Params) != 2 ||
		put.Params["name"] != "github-token" || put.Params["text"] != got.Typed {
		t.Errorf("Save sent %+v, not secret.put to helios with the name and the typed text", put)
	}
	if !strings.Contains(got.Typed, secretMark) {
		t.Fatalf("the fixture did not type the marker: %q", got.Typed)
	}
	if s.Confirm {
		t.Error("the gate asked a second time over the notepad's own Save")
	}
	if !s.Closed {
		t.Error("the notepad stayed open after the save")
	}
	if s.Toast != "Saved to "+secretDir+"/github-token" {
		t.Errorf("the note after the save reads %q", s.Toast)
	}
	if len(s.Stored) > 0 || s.InDom {
		t.Errorf("the saved text rests in the page: storage %v, in the document %v", s.Stored, s.InDom)
	}
	if strings.Join(s.Carried, ",") != "POST /api/actions" {
		t.Errorf("the text left the page in %v, not in the one action", s.Carried)
	}
}

// A name the host does not keep starts on its own template and replaces
// nothing; whitespace alone is not saved.
func secretNewNameReplacesNothing(t *testing.T, got secretPad) {
	f := got.Fresh
	if f.Value != "DB_USER=\nDB_PASSWORD=\n" || f.Path != secretDir+"/evirma-db.env" {
		t.Errorf("the notepad of the database opened on %q at %q", f.Value, f.Path)
	}
	if f.Replaces {
		t.Error("a name the host does not keep says it replaces a secret")
	}
	if !got.BlankOff {
		t.Error("Save is on for a text of whitespace alone")
	}
}
