package check

import (
	"reflect"
	"strings"
	"testing"
)

// sentAction is a request a fixture caught on its way to /api/actions.
type sentAction struct {
	Kind   string         `json:"kind"`
	Target string         `json:"target"`
	Params map[string]any `json:"params"`
}

// A round of plan mode on a codex thread that lives in tmux is answered by
// codex's protocol, not by keys: the free answer and the "discuss" item are
// open, a note goes beside a pick, a question with no options is a field of
// its own, and the words of a secret one are typed out of sight and stay out
// of sight on the review. All of it reaches the host in one answer.
func TestACodexRoundIsAnsweredByItsProtocolInTmux(t *testing.T) {
	var got struct {
		Round struct {
			Label  string `json:"label"`
			Own    bool   `json:"own"`
			Drop   bool   `json:"drop"`
			Second struct {
				Tag     string `json:"tag"`
				Type    string `json:"type"`
				Options int    `json:"options"`
				Label   string `json:"label"`
			} `json:"second"`
			SecondButton string       `json:"secondButton"`
			Review       string       `json:"review"`
			SendLabel    string       `json:"sendLabel"`
			Sent         []sentAction `json:"sent"`
		} `json:"round"`
	}
	runFixture(t, "codexask.html", &got)
	r := got.Round

	if r.Label != "Clock · codex asks" {
		t.Errorf("the question is headed %q: the card does not say codex asks", r.Label)
	}
	if !r.Own {
		t.Error("the free answer is shut for a codex thread in tmux: the limits of a terminal dialog were carried over")
	}
	if !r.Drop {
		t.Error("the discuss item is shut for a codex thread")
	}
	if r.Second.Tag != "input" || r.Second.Type != "password" {
		t.Errorf("the secret question is answered in a %s of type %q: its words are shown as they are typed",
			r.Second.Tag, r.Second.Type)
	}
	if r.Second.Options != 0 {
		t.Errorf("a question with no options shows %d rows to pick", r.Second.Options)
	}
	if r.Second.Label != "Token · codex asks" {
		t.Errorf("the second question is headed %q", r.Second.Label)
	}
	if strings.Contains(r.Review, "s3cret-token") || !strings.Contains(r.Review, "hidden") {
		t.Errorf("the review shows the secret words: %q", r.Review)
	}
	if r.SendLabel != "Send" {
		t.Errorf("the button of the review reads %q", r.SendLabel)
	}
	if len(r.Sent) != 1 || r.Sent[0].Kind != "session.answer" || r.Sent[0].Target != "shop-checkout" {
		t.Fatalf("the host was asked %+v", r.Sent)
	}
	p := r.Sent[0].Params
	want := map[string]any{
		"ask":   "call_plan_1",
		"picks": []any{[]any{2.0}, []any{}},
		"texts": []any{"", "s3cret-token"},
		"notes": []any{"and keep the old helper", ""},
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("the answer went as %v, want %v", p, want)
	}
}

// A question of codex that takes no words of the person's own offers no row
// for them, and a single question is answered at the tap of an option. Put
// away, it is dismissed by its id. And codex is answered by its protocol even
// where the screen says nothing of the stream: a note goes beside a pick.
func TestACodexQuestionOffersOnlyWhatCodexTakes(t *testing.T) {
	var got struct {
		Closed struct {
			Own        bool         `json:"own"`
			Foot       bool         `json:"foot"`
			Answer     []sentAction `json:"answer"`
			Dismiss    []sentAction `json:"dismiss"`
			NoteInTmux bool         `json:"noteInTmux"`
		} `json:"closed"`
	}
	runFixture(t, "codexask.html", &got)
	c := got.Closed

	if c.Own {
		t.Error("a question codex asks without a free answer offers one: codex would get words it did not ask for")
	}
	if c.Foot {
		t.Error("a single question stands with a button under it: a tap on an option is the answer")
	}
	if len(c.Answer) != 1 || !reflect.DeepEqual(c.Answer[0].Params, map[string]any{"ask": "call_plan_2", "picks": []any{[]any{1.0}}}) {
		t.Errorf("the tap went as %+v", c.Answer)
	}
	if len(c.Dismiss) != 1 || !reflect.DeepEqual(c.Dismiss[0].Params, map[string]any{"ask": "call_plan_3"}) {
		t.Errorf("putting the question away went as %+v", c.Dismiss)
	}
	if !c.NoteInTmux {
		t.Error("a codex question mounted without the stream has no field for a note: it is answered as a terminal dialog")
	}
}

// A form an MCP server asks through codex: the server and its words over the
// fields, a field of each kind drawn as its kind — words, hidden words, a list,
// segments, a switch and several picks — no note anywhere and no "discuss".
// A required field holds the form back and says so; filled, the form goes as
// a pick or words a field, and declined it is dismissed by its id.
func TestACodexFormIsFilledAndDeclined(t *testing.T) {
	var got struct {
		Form struct {
			Label    string       `json:"label"`
			Message  string       `json:"message"`
			Notes    int          `json:"notes"`
			Drop     bool         `json:"drop"`
			Own      bool         `json:"own"`
			Fields   []string     `json:"fields"`
			Required []string     `json:"required"`
			Inputs   []string     `json:"inputs"`
			Select   int          `json:"select"`
			Segments int          `json:"segments"`
			Switches int          `json:"switches"`
			HeldBack bool         `json:"heldBack"`
			Held     string       `json:"held"`
			Free     bool         `json:"free"`
			After    string       `json:"after"`
			Sent     []sentAction `json:"sent"`
			Decline  []sentAction `json:"decline"`
		} `json:"form"`
	}
	runFixture(t, "codexask.html", &got)
	f := got.Form

	if f.Label != "linear · MCP server asks" {
		t.Errorf("the form is headed %q: it does not name the server", f.Label)
	}
	if f.Message != "Create an issue for the flaky test?" {
		t.Errorf("the words of the server read %q", f.Message)
	}
	if f.Notes != 0 || f.Drop || f.Own {
		t.Errorf("the form offers %d notes, discuss %v, own words %v: the server takes none of them", f.Notes, f.Drop, f.Own)
	}
	if len(f.Fields) != 6 {
		t.Errorf("the form shows the fields %q", f.Fields)
	}
	if !reflect.DeepEqual(f.Required, []string{"Title"}) {
		t.Errorf("the fields marked required are %q", f.Required)
	}
	if !reflect.DeepEqual(f.Inputs, []string{"text", "password"}) {
		t.Errorf("the fields of words are of the types %q: the secret one shows what is typed", f.Inputs)
	}
	if f.Select != 7 || f.Segments != 4 || f.Switches != 1 {
		t.Errorf("the list has %d entries, the segments %d, the switches %d", f.Select, f.Segments, f.Switches)
	}
	if !f.HeldBack || !strings.Contains(f.Held, "Title") {
		t.Errorf("an empty required field does not hold the form back (held %v): %q", f.HeldBack, f.Held)
	}
	if !f.Free || strings.Contains(f.After, "Title") {
		t.Errorf("the filled form is still held back: %q", f.After)
	}
	if len(f.Sent) != 1 || f.Sent[0].Kind != "session.answer" {
		t.Fatalf("the form went as %+v", f.Sent)
	}
	want := map[string]any{
		"ask":   "elicit-7",
		"picks": []any{[]any{}, []any{}, []any{3.0}, []any{2.0}, []any{1.0}, []any{1.0, 3.0}},
		"texts": []any{"TestCartTotal flakes on a second boundary", "lin_api_123", "", "", "", ""},
	}
	if !reflect.DeepEqual(f.Sent[0].Params, want) {
		t.Errorf("the form went as %v, want %v", f.Sent[0].Params, want)
	}
	if len(f.Decline) != 1 || !reflect.DeepEqual(f.Decline[0].Params, map[string]any{"ask": "elicit-8"}) {
		t.Errorf("the decline went as %+v", f.Decline)
	}
}
