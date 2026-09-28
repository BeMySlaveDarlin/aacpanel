package check

import (
	"math"
	"strings"
	"testing"
)

type cardBox struct {
	Left, Right, Width, Top float64
}

type feedCard struct {
	Cls       string   `json:"cls"`
	Box       *cardBox `json:"box"`
	Look      string   `json:"look"`
	Head      string   `json:"head"`
	Icon      bool     `json:"icon"`
	Label     string   `json:"label"`
	LabelFont string   `json:"labelFont"`
	At        *cardBox `json:"at"`
	Plate     *cardBox `json:"plate"`
	PlateLook string   `json:"plateLook"`
	Rows      []struct {
		Box    *cardBox `json:"box"`
		Radius string   `json:"radius"`
	} `json:"rows"`
}

type feedCardsShot struct {
	Error     string     `json:"error"`
	Wide      bool       `json:"wide"`
	Cards     []feedCard `json:"cards"`
	Questions []struct {
		Tag, TagCase, TagColour, Question, Answer string
	} `json:"questions"`
	Afk struct {
		Why, Answer string
	} `json:"afk"`
	Permits []struct {
		Tag, TagColour, Subject, Answer, AnswerColour string
	} `json:"permits"`
	Page struct {
		Title, Desc, Link string
	} `json:"page"`
	Again struct {
		Title, Desc, Meta string
	} `json:"again"`
	Brief struct {
		Title, Meta, Node string
	} `json:"brief"`
	Wake struct {
		Closed, More, Open, Fold string
	} `json:"wake"`
	Briefs []string `json:"briefs"`
}

func samePx(a, b float64) bool { return math.Abs(a-b) <= 0.5 }

// Every card of what arrives in the feed is built as the files sent to the
// person are: a plate of the same face, corner and inset from the left edge
// of the column to its right one; a head with the mark of the kind, its name
// in the code face and capitals, and the time at the right; and under it a
// plate of rows as wide as the one the files lie on. A card built otherwise is
// a second language in one column, and the owner reads it as clutter.
func testTheCardsOfTheFeedShareTheBuildOfFilesForYou(t *testing.T, run func(*testing.T, string, any)) {
	var got feedCardsShot
	run(t, "feedcards.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if len(got.Cards) != 8 {
		t.Fatalf("%d cards drawn, wanted eight", len(got.Cards))
	}
	files := got.Cards[0]
	if files.Label != "file for you" || files.Box == nil || files.Plate == nil || files.At == nil {
		t.Fatalf("the reference card is not the files sent to the person: %+v", files)
	}
	names := []string{"questions", "question", "permissions", "page", "page updated", "brief", "wake-up"}
	for i, card := range got.Cards[1:] {
		name := names[i]
		if card.Box == nil {
			t.Errorf("%s: no card drawn", name)
			continue
		}
		if !strings.Contains(" "+card.Cls+" ", " sent ") {
			t.Errorf("%s: the card is %q, not a card of the files' build", name, card.Cls)
		}
		if !samePx(card.Box.Left, files.Box.Left) || !samePx(card.Box.Width, files.Box.Width) {
			t.Errorf("%s: the card stands at %.1f and is %.1f wide, the files at %.1f and %.1f",
				name, card.Box.Left, card.Box.Width, files.Box.Left, files.Box.Width)
		}
		if card.Look != files.Look {
			t.Errorf("%s: the card is drawn %q against %q of the files", name, card.Look, files.Look)
		}
		if card.Head != "senthead" || !card.Icon {
			t.Errorf("%s: the card opens with %q and a mark %v: the head of the files' card is what it opens with", name, card.Head, card.Icon)
		}
		if card.Label != name {
			t.Errorf("the head of the %s card says %q", name, card.Label)
		}
		if card.LabelFont != files.LabelFont {
			t.Errorf("%s: the name on the head is set %q, the files' %q", name, card.LabelFont, files.LabelFont)
		}
		if card.At == nil || !samePx(card.At.Right, files.At.Right) {
			t.Errorf("%s: the time of the card is not at the right of its head, where the files have theirs: %+v against %+v",
				name, card.At, files.At)
		}
		if card.Plate == nil {
			t.Errorf("%s: no plate of rows under the head", name)
			continue
		}
		if !samePx(card.Plate.Left, files.Plate.Left) || !samePx(card.Plate.Width, files.Plate.Width) {
			t.Errorf("%s: the plate stands at %.1f and is %.1f wide, the files' at %.1f and %.1f",
				name, card.Plate.Left, card.Plate.Width, files.Plate.Left, files.Plate.Width)
		}
		if card.PlateLook != files.PlateLook {
			t.Errorf("%s: the plate is drawn %q against %q of the files", name, card.PlateLook, files.PlateLook)
		}
		for n, row := range card.Rows {
			if row.Box == nil || !samePx(row.Box.Left, card.Plate.Left) || !samePx(row.Box.Width, card.Plate.Width) || row.Radius != "0px" {
				t.Errorf("%s: row %d of the plate is %+v with corners %q: a row fills its plate edge to edge, square",
					name, n, row.Box, row.Radius)
			}
		}
	}
}

func TestTheCardsOfTheFeedShareTheBuildOfFilesForYouOnAPhone(t *testing.T) {
	testTheCardsOfTheFeedShareTheBuildOfFilesForYou(t, runFixture)
}

func TestTheCardsOfTheFeedShareTheBuildOfFilesForYouAtADesk(t *testing.T) {
	testTheCardsOfTheFeedShareTheBuildOfFilesForYou(t, runWideFixture)
}

// Built anew, each card still says what it said: every question with its
// header, what was asked and what was picked, a question nobody answered told
// apart at a glance by its dimmed tag; a round nobody answered says so on its
// head; a permission names its tool, its call and the answer, a refusal in a
// tone of its own; a page and a brief are the row that opens them, a page
// published again saying so on its head rather than as a word among its meta;
// a wake-up shows its first paragraph and opens the rest by a row under it.
func TestTheCardsOfTheFeedStillSayWhatTheySaid(t *testing.T) {
	var got feedCardsShot
	runFixture(t, "feedcards.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}

	if len(got.Questions) != 3 {
		t.Fatalf("question rows: %+v", got.Questions)
	}
	for i, want := range []struct{ tag, question, answer string }{
		{"Pushes", "Which screen do we build?", "The second one"},
		{"Plan: where", "Where is the plan drawn?", "skipped"},
		{"Plan: nudge", "Nudge the session about an open plan?", "At the end of a turn · By a hook"},
	} {
		q := got.Questions[i]
		if q.Tag != want.tag || q.Question != want.question || q.Answer != want.answer {
			t.Errorf("question %d reads %q / %q / %q, wanted %q / %q / %q", i, q.Tag, q.Question, q.Answer, want.tag, want.question, want.answer)
		}
		if q.TagCase != "uppercase" {
			t.Errorf("question %d: its header is set %q, not in capitals as the tags of the files are", i, q.TagCase)
		}
	}
	if got.Questions[1].TagColour == got.Questions[0].TagColour {
		t.Errorf("a question left without an answer is tagged in the colour of an answered one: %s", got.Questions[1].TagColour)
	}
	if got.Afk.Why != "nobody answered" || got.Afk.Answer != "" {
		t.Errorf("the round nobody answered says %q on its head and %q under its question", got.Afk.Why, got.Afk.Answer)
	}

	if len(got.Permits) != 2 {
		t.Fatalf("permission rows: %+v", got.Permits)
	}
	allow, deny := got.Permits[0], got.Permits[1]
	if allow.Tag != "Bash" || allow.Subject != "docker compose up -d" || allow.Answer != "Allowed" ||
		deny.Subject != "rm -rf web/dist" || deny.Answer != "Denied" {
		t.Errorf("the permissions read %+v and %+v", allow, deny)
	}
	if allow.TagColour == deny.TagColour || deny.TagColour != deny.AnswerColour {
		t.Errorf("a refused call is tagged in %s, an allowed one in %s and the refusal is said in %s: "+
			"the tag of a refusal takes the tone of the refusal", deny.TagColour, allow.TagColour, deny.AnswerColour)
	}

	if got.Page.Title != "The round of the feed" || got.Page.Desc != "now | A | B on a live conversation" ||
		got.Page.Link != "https://claude.ai/public/artifacts/9f3c2e1a" {
		t.Errorf("the page reads %+v", got.Page)
	}
	if got.Again.Title != "The round, again" || got.Again.Desc != "the desk added" {
		t.Errorf("the page published again reads %+v: its own words for the update", got.Again)
	}
	if strings.Contains(got.Again.Meta, "update") {
		t.Errorf("the page published again still says so among its meta (%q) as well as on its head", got.Again.Meta)
	}

	if got.Brief.Title != "What stays on the head of a card" || !strings.Contains(got.Brief.Meta, "7 questions") {
		t.Errorf("the brief reads %+v", got.Brief)
	}
	if strings.Contains(got.Brief.Meta, "brief") {
		t.Errorf("the row of the brief names it a brief (%q) under a head that already does", got.Brief.Meta)
	}
	if got.Brief.Node != "BUTTON" || len(got.Briefs) != 1 || got.Briefs[0] != "cards-q" {
		t.Errorf("the row of the brief is a %s and a tap on it opened %v", got.Brief.Node, got.Briefs)
	}

	if got.Wake.Closed != "Watch tick: is make check done?" || got.Wake.More != "The whole prompt" {
		t.Errorf("the closed wake-up shows %q with a row %q", got.Wake.Closed, got.Wake.More)
	}
	if !strings.Contains(got.Wake.Open, "Red: read the log and fix it first.") || got.Wake.Fold != "Fold the prompt" {
		t.Errorf("the opened wake-up shows %q with a row %q", got.Wake.Open, got.Wake.Fold)
	}
}
