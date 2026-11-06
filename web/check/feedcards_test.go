package check

import (
	"strings"
	"testing"
)

type feedCard struct {
	Cls   string `json:"cls"`
	Head  string `json:"head"`
	Icon  bool   `json:"icon"`
	Label string `json:"label"`
	At    bool   `json:"at"`
	Plate bool   `json:"plate"`
}

type feedCardsShot struct {
	Error     string     `json:"error"`
	Wide      bool       `json:"wide"`
	Cards     []feedCard `json:"cards"`
	Questions []struct {
		Tag, Tone, Question, Answer string
	} `json:"questions"`
	Afk struct {
		Why, Answer string
	} `json:"afk"`
	Permits []struct {
		Tag, Tone, Subject, Answer string
		Denied                     bool
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
	Calls  []struct {
		Text, Why string
		Lost      bool
	} `json:"calls"`
}

// Every card of what arrives in the feed is built as the files sent to the
// person are: a card of the same build, a head with the mark of the kind, its
// name and the time, and under it a plate of rows. A card built otherwise is a
// second language in one column. So on a phone as at a desk.
func TestTheCardsOfTheFeedShareTheBuildOfFilesForYou(t *testing.T) {
	parallel(t)
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
		wide bool
	}{
		{"phone", runFixture, false},
		{"desk", runWideFixture, true},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got feedCardsShot
			screen.run(t, "feedcards.html", &got)
			if got.Error != "" {
				t.Fatal(got.Error)
			}
			if got.Wide != screen.wide {
				t.Fatalf("the fixture drew the feed of a desk %v on the screen of a %s", got.Wide, screen.name)
			}
			checkFeedCardsBuild(t, got)
		})
	}
}

func checkFeedCardsBuild(t *testing.T, got feedCardsShot) {
	t.Helper()
	if len(got.Cards) != 10 {
		t.Fatalf("%d cards drawn, wanted ten", len(got.Cards))
	}
	files := got.Cards[0]
	if files.Label != "file for you" || !files.Plate || !files.At {
		t.Fatalf("the reference card is not the files sent to the person: %+v", files)
	}
	names := []string{"questions", "question", "permissions", "page", "page updated", "brief", "alarm",
		"called you", "not called"}
	// A call is a line said, as a letter is: its words stand under the head,
	// with no plate of rows to open.
	said := map[string]bool{"called you": true, "not called": true}
	for i, card := range got.Cards[1:] {
		name := names[i]
		if !strings.Contains(" "+card.Cls+" ", " sent ") {
			t.Errorf("%s: the card is %q, not a card of the files' build", name, card.Cls)
		}
		if card.Head != "senthead" || !card.Icon {
			t.Errorf("%s: the card opens with %q and a mark %v: the head of the files' card is what it opens with", name, card.Head, card.Icon)
		}
		if card.Label != name {
			t.Errorf("the head of the %s card says %q", name, card.Label)
		}
		if !card.At {
			t.Errorf("%s: the head of the card has no time, where the files have theirs", name)
		}
		if card.Plate == said[name] {
			t.Errorf("%s: a plate of rows under the head %v, expected %v", name, card.Plate, !said[name])
		}
	}
}

// Built anew, each card still says what it said: every question with its
// header, what was asked and what was picked, a question nobody answered told
// apart by the tone of its row; a round nobody answered says so on its head; a
// permission names its tool, its call and the answer, a refusal in a tone of
// its own; a page and a brief are the row that opens them, a page
// published again saying so on its head rather than as a word among its meta;
// a wake-up shows its first paragraph and opens the rest by a row under it; a
// call nobody got is marked apart from one that went.
func TestTheCardsOfTheFeedStillSayWhatTheySaid(t *testing.T) {
	var got feedCardsShot
	runFixture(t, "feedcards.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}

	if len(got.Questions) != 3 {
		t.Fatalf("question rows: %+v", got.Questions)
	}
	for i, want := range []struct{ tag, tone, question, answer string }{
		{"Pushes", "", "Which screen do we build?", "The second one"},
		{"Plan: where", "s-faint", "Where is the plan drawn?", "skipped"},
		{"Plan: nudge", "", "Nudge the session about an open plan?", "At the end of a turn · By a hook"},
	} {
		q := got.Questions[i]
		if q.Tag != want.tag || q.Question != want.question || q.Answer != want.answer {
			t.Errorf("question %d reads %q / %q / %q, wanted %q / %q / %q", i, q.Tag, q.Question, q.Answer, want.tag, want.question, want.answer)
		}
		if q.Tone != want.tone {
			t.Errorf("question %d is in the tone %q, wanted %q: a question left without an answer is told apart", i, q.Tone, want.tone)
		}
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
	if allow.Tone != "s-ok" || allow.Denied || deny.Tone != "s-crit" || !deny.Denied {
		t.Errorf("an allowed call is in the tone %q (its answer a refusal %v) and a refused one in %q (%v): "+
			"the row of a refusal takes the tone of the refusal", allow.Tone, allow.Denied, deny.Tone, deny.Denied)
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

	if len(got.Calls) != 2 {
		t.Fatalf("calls of the person: %+v", got.Calls)
	}
	called, refused := got.Calls[0], got.Calls[1]
	if called.Text != "The migration waits for you: prod or stage?" || called.Why != "" {
		t.Errorf("the call reads %q with %q under it: the line the phone got, and nothing else", called.Text, called.Why)
	}
	if refused.Text != "Still waiting on the migration" ||
		refused.Why != "Nobody was called: this session called 12 s ago: one call a minute, 48 s left." {
		t.Errorf("the refused call reads %q with %q under it: its line, and why nobody was called", refused.Text, refused.Why)
	}
	if called.Lost || !refused.Lost {
		t.Errorf("the call that went is marked lost %v and the one nobody got %v", called.Lost, refused.Lost)
	}
}
