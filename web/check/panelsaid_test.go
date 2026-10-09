package check

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

// panelSamples are the messages the panel writes into a session on the
// person's behalf, as the writers are checked against them: the Go tests of
// secretMessage and briefReply rebuild each text from its input, and the
// fixture reads every text with the parser of the feed.
type panelSamples struct {
	Secret  []panelSample `json:"secret"`
	Brief   []panelSample `json:"brief"`
	Reading []struct {
		panelSample
		Input struct {
			Path  string `json:"path"`
			Notes int    `json:"notes"`
		} `json:"input"`
	} `json:"reading"`
	Bubbles []struct {
		Why  string `json:"why"`
		Text string `json:"text"`
	} `json:"bubbles"`
}

type panelSample struct {
	Text string `json:"text"`
	Said any    `json:"said"`
}

type panelBox struct {
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Width  float64 `json:"width"`
}

type panelCard struct {
	Kind      string   `json:"kind"`
	Label     string   `json:"label"`
	At        string   `json:"at"`
	Title     string   `json:"title"`
	Meta      []string `json:"meta"`
	Keys      []string `json:"keys"`
	Rows      []string `json:"rows"`
	Queued    bool     `json:"queued"`
	Withdrawn bool     `json:"withdrawn"`
	Stamp     string   `json:"stamp"`
	Side      string   `json:"side"`
}

type panelSaidRun struct {
	Parsed struct {
		Secret  []any `json:"secret"`
		Brief   []any `json:"brief"`
		Reading []any `json:"reading"`
	} `json:"parsed"`
	Bubbles []struct {
		Why  string `json:"why"`
		Said any    `json:"said"`
	} `json:"bubbles"`
	Signals     []string    `json:"signals"`
	Cards       []panelCard `json:"cards"`
	BubbleTexts []string    `json:"bubbleTexts"`
	BriefMark   string      `json:"briefMark"`
	SecretName  *struct {
		Text    string  `json:"text"`
		Width   float64 `json:"width"`
		Natural float64 `json:"natural"`
	} `json:"secretName"`
	SecretSaved string `json:"secretSaved"`
	SecretRaw   string `json:"secretRaw"`
	Answers     []struct {
		N        string `json:"n"`
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Note     string `json:"note"`
		Faint    bool   `json:"faint"`
	} `json:"answers"`
	FirstRow struct {
		Question panelBox `json:"question"`
		Answer   panelBox `json:"answer"`
	} `json:"firstRow"`
	BriefRows []string `json:"briefRows"`
	BriefRaw  string   `json:"briefRaw"`
	Notes     []struct {
		Tag   string `json:"tag"`
		Quote string `json:"quote"`
		Text  string `json:"text"`
	} `json:"notes"`
	Asked      []string `json:"asked"`
	ReadingRaw string   `json:"readingRaw"`
	Opened     bool     `json:"opened"`
	Error      string   `json:"error"`
}

func readPanelSamples(t *testing.T) panelSamples {
	t.Helper()
	raw, err := os.ReadFile("testdata/panel-said.json")
	if err != nil {
		t.Fatal(err)
	}
	var s panelSamples
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("the samples do not parse: %v", err)
	}
	if len(s.Secret) == 0 || len(s.Brief) < 2 || len(s.Reading) < 2 || len(s.Bubbles) == 0 {
		t.Fatalf("the samples are short of what the fixture draws: %d secrets, %d briefs, %d readings, %d bubbles",
			len(s.Secret), len(s.Brief), len(s.Reading), len(s.Bubbles))
	}
	return s
}

// TestPanelMessagesAreCards runs the messages the panel wrote on the person's
// behalf through the parser of the feed and the real conversation, on a phone
// and at a desk: every sample reads back as its writer meant it, a text one
// word away stays a bubble, and each message stands as a card of its own whose
// rows open the answers, the notes and the text the session got.
func TestPanelMessagesAreCards(t *testing.T) {
	samples := readPanelSamples(t)
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{
		{"phone", runFixture},
		{"desk", runWideFixture},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got panelSaidRun
			screen.run(t, "panelsaid.html", &got)
			if got.Error != "" {
				t.Fatalf("the fixture broke: %s", got.Error)
			}
			desk := screen.name == "desk"
			for _, part := range []struct {
				name  string
				check func(*testing.T, panelSamples, panelSaidRun)
			}{
				{"theParserReadsEverySample", panelParserReadsTheSamples},
				{"aTextOffTheShapeStaysABubble", panelOffShapeStaysABubble},
				{"theSignalIsTheSample", panelSignalIsTheSample},
				{"eachKindIsACardOnThePersonsSide", panelEachKindIsACard},
				{"theCardsOpenWhatTheSessionGot", panelCardsOpenTheText},
				{"theBriefOpensItsAnswers", panelBriefOpensItsAnswers},
				{"theReadingOpensItsNotes", panelReadingOpensItsNotes},
				{"theBriefOfTheFeedSaysSent", panelBriefSaysSent},
				{"theNameOfASecretDoesNotBreak", panelSecretNameDoesNotBreak},
			} {
				t.Run(part.name, func(t *testing.T) { part.check(t, samples, got) })
			}
			t.Run("theAnswerStandsWhereTheScreenHasRoom", func(t *testing.T) { panelAnswerPlace(t, got, desk) })
		})
	}
}

// sameJSON compares two values as JSON reads them: the parser answers in
// JavaScript, the samples are written by hand.
func sameJSON(t *testing.T, got, want any) bool {
	t.Helper()
	norm := func(v any) any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	return reflect.DeepEqual(norm(got), norm(want))
}

func panelParserReadsTheSamples(t *testing.T, s panelSamples, got panelSaidRun) {
	reads := func(kind string, parsed []any, want []any) {
		if len(parsed) != len(want) {
			t.Fatalf("%s: the parser read %d of %d samples", kind, len(parsed), len(want))
		}
		for i := range want {
			if !sameJSON(t, parsed[i], want[i]) {
				g, _ := json.Marshal(parsed[i])
				w, _ := json.Marshal(want[i])
				t.Errorf("%s sample %d reads as\n%s\nwant\n%s", kind, i, g, w)
			}
		}
	}
	saids := func(list []panelSample) []any {
		out := make([]any, len(list))
		for i, one := range list {
			out[i] = one.Said
		}
		return out
	}
	readings := make([]any, len(s.Reading))
	for i, one := range s.Reading {
		readings[i] = one.Said
	}
	reads("secret", got.Parsed.Secret, saids(s.Secret))
	reads("brief", got.Parsed.Brief, saids(s.Brief))
	reads("reading", got.Parsed.Reading, readings)
}

func panelOffShapeStaysABubble(t *testing.T, s panelSamples, got panelSaidRun) {
	if len(got.Bubbles) != len(s.Bubbles) {
		t.Fatalf("the parser read %d of %d bubbles", len(got.Bubbles), len(s.Bubbles))
	}
	for _, b := range got.Bubbles {
		if b.Said != nil {
			t.Errorf("%s, and the feed still takes it for the panel's: %v", b.Why, b.Said)
		}
	}
	want := []string{"check the notepad live", s.Bubbles[0].Text}
	if !reflect.DeepEqual(got.BubbleTexts, want) {
		t.Errorf("the bubbles of the person are\n%q\nwant\n%q", got.BubbleTexts, want)
	}
}

func panelSignalIsTheSample(t *testing.T, s panelSamples, got panelSaidRun) {
	if len(got.Signals) != len(s.Reading) {
		t.Fatalf("signal() wrote %d of %d samples", len(got.Signals), len(s.Reading))
	}
	for i, one := range s.Reading {
		if got.Signals[i] != one.Text {
			t.Errorf("signal(%q, %d) says\n%q\nand the feed reads\n%q", one.Input.Path, one.Input.Notes, got.Signals[i], one.Text)
		}
	}
}

func panelEachKindIsACard(t *testing.T, _ panelSamples, got panelSaidRun) {
	kinds := make([]string, len(got.Cards))
	for i, c := range got.Cards {
		kinds[i] = c.Kind
	}
	if want := []string{"secret", "brief", "reading", "brief", "reading"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("the feed drew the cards %v, want %v", kinds, want)
	}
	for _, c := range got.Cards {
		if c.Side != "flex-end" {
			t.Errorf("the %s card stands on the %s side, not on the person's", c.Kind, c.Side)
		}
		if c.At == "" {
			t.Errorf("the %s card does not say when it was sent", c.Kind)
		}
	}

	secret := got.Cards[0]
	if secret.Label != "secret saved" || secret.Title != "aacpanel-live-check" {
		t.Errorf("the secret card reads %+v", secret)
	}
	if len(secret.Keys) != 2 || secret.Keys[0] != "CHECK_VALUE" || secret.Keys[1] != "LEFT_EMPTYempty" {
		t.Errorf("the keys of the secret read %q: CHECK_VALUE, and LEFT_EMPTY marked empty", secret.Keys)
	}
	if want := []string{"5 lines · 0600 | /home/u/.local/state/aacpanel/secrets/"}; !reflect.DeepEqual(secret.Meta, want) {
		t.Errorf("the secret card says %q", secret.Meta)
	}

	brief := got.Cards[1]
	if brief.Label != "brief answers" || brief.Title != "Messages of the panel in the feed" ||
		len(brief.Meta) != 1 || brief.Meta[0] != "Answered 5 of 7 | 1 skipped · 2 without an answer" {
		t.Errorf("the brief card reads %+v", brief)
	}
	if want := []string{"The answers", "What the session got", "Open the brief"}; !reflect.DeepEqual(brief.Rows, want) {
		t.Errorf("the rows of the brief card are %q, want %q", brief.Rows, want)
	}

	reading := got.Cards[2]
	if reading.Label != "notes on the branch" || reading.Title != "4 notes on the lines of the branch" ||
		len(reading.Meta) != 1 || reading.Meta[0] != "/var/lib/aacpanel/reviews/helios-20260101-090000.json" {
		t.Errorf("the reading card reads %+v", reading)
	}
	if want := []string{"The notes", "What the session got"}; !reflect.DeepEqual(reading.Rows, want) {
		t.Errorf("the rows of the reading card are %q, want %q", reading.Rows, want)
	}

	// A message on its way is faded and says so under the card, as a bubble
	// does; one taken back says that.
	if queued := got.Cards[3]; !queued.Queued || queued.Withdrawn || queued.Stamp != "queued" {
		t.Errorf("a brief in the queue reads %+v", queued)
	}
	if gone := got.Cards[4]; !gone.Withdrawn || gone.Queued || gone.Stamp != "taken back — the session did not read it" {
		t.Errorf("a reading taken back reads %+v", gone)
	}
	for _, c := range got.Cards[:3] {
		if c.Queued || c.Withdrawn || c.Stamp != "" {
			t.Errorf("a %s card that went out reads as on its way: %+v", c.Kind, c)
		}
	}
}

func panelCardsOpenTheText(t *testing.T, s panelSamples, got panelSaidRun) {
	for _, c := range []struct{ kind, got, want string }{
		{"secret", got.SecretRaw, s.Secret[0].Text},
		{"brief", got.BriefRaw, s.Brief[0].Text},
		{"reading", got.ReadingRaw, s.Reading[0].Text},
	} {
		if c.got != c.want {
			t.Errorf("the %s card opens the text\n%q\nand the session got\n%q", c.kind, c.got, c.want)
		}
	}
}

func panelBriefOpensItsAnswers(t *testing.T, _ panelSamples, got panelSaidRun) {
	if len(got.Answers) != 7 {
		t.Fatalf("the brief opened %d answers, not seven: %+v", len(got.Answers), got.Answers)
	}
	first := got.Answers[0]
	if first.N != "01" || first.Question != "Where the answer to a brief stands" ||
		first.Answer != "B · A card where the message stands" || first.Note != "opens on a tap, like a letter" || first.Faint {
		t.Errorf("the first answer reads %+v", first)
	}
	if skipped := got.Answers[2]; skipped.Answer != "skipped" || !skipped.Faint {
		t.Errorf("a skipped question reads %+v", skipped)
	}
	if none := got.Answers[5]; none.Answer != "no answer" || !none.Faint {
		t.Errorf("a question without an answer reads %+v", none)
	}
	if want := []string{"Fold the answers", "What the session got", "Open the brief"}; !reflect.DeepEqual(got.BriefRows, want) {
		t.Errorf("the rows of the open brief card are %q, want %q", got.BriefRows, want)
	}
	if !got.Opened {
		t.Error("Open the brief did not open the document the answers went to")
	}
}

func panelReadingOpensItsNotes(t *testing.T, _ panelSamples, got panelSaidRun) {
	if want := []string{"/api/reviews/helios-20260101-090000"}; !reflect.DeepEqual(got.Asked, want) {
		t.Errorf("the reading asked the panel %q, want %q", got.Asked, want)
	}
	if len(got.Notes) != 4 {
		t.Fatalf("the reading opened %d notes, not four: %+v", len(got.Notes), got.Notes)
	}
	first := got.Notes[0]
	if first.Tag != "rows.js:112" || first.Quote != "${render(said, { breaks: mine })}" ||
		first.Text != "a path breaks in the middle of a word here" {
		t.Errorf("the first note reads %+v", first)
	}
}

func panelBriefSaysSent(t *testing.T, _ panelSamples, got panelSaidRun) {
	if got.BriefMark != "sent" {
		t.Errorf("the card of the brief in the feed says %q: its answers are marked sent on the shelf", got.BriefMark)
	}
}

// The card a session asked a secret with names the file beside the time it
// was saved. Squeezed onto one line the two broke the name in the middle; the
// line of the name is to be as wide as the name.
func panelSecretNameDoesNotBreak(t *testing.T, _ panelSamples, got panelSaidRun) {
	if got.SecretSaved == "" {
		t.Fatal("the card of the secret is not marked saved: the name has nothing beside it to be measured against")
	}
	name := got.SecretName
	if name == nil || name.Text != "aacpanel-live-check" {
		t.Fatalf("the name on the card of the secret reads %+v", name)
	}
	if name.Width+0.5 < name.Natural {
		t.Errorf("the name of the secret has %.1fpx for %.1fpx of text: it breaks inside the word", name.Width, name.Natural)
	}
}

// On a phone the answer stands under its question; at a desk beside it, a
// column to the right.
func panelAnswerPlace(t *testing.T, got panelSaidRun, desk bool) {
	q, a := got.FirstRow.Question, got.FirstRow.Answer
	if q.Width == 0 || a.Width == 0 {
		t.Fatalf("the first answer was not measured: %+v", got.FirstRow)
	}
	if desk {
		if math.Abs(a.Top-q.Top) > 4 || a.Left < q.Right {
			t.Errorf("at a desk the answer %+v does not stand beside its question %+v", a, q)
		}
		return
	}
	if a.Top < q.Bottom-1 {
		t.Errorf("on a phone the answer %+v does not stand under its question %+v", a, q)
	}
}
