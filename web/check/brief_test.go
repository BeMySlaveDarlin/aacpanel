package check

import (
	"strings"
	"testing"
)

type briefShot struct {
	Before struct {
		Title        string   `json:"title"`
		Questions    []string `json:"questions"`
		Facts        int      `json:"facts"`
		Flagged      int      `json:"flagged"`
		Sources      []string `json:"sources"`
		Options      []string `json:"options"`
		Costs        int      `json:"costs"`
		Reading      int      `json:"reading"`
		Chips        []string `json:"chips"`
		Summary      []string `json:"summary"`
		Lineage      int      `json:"lineage"`
		Closing      int      `json:"closing"`
		Captures     int      `json:"captures"`
		Pressed      []string `json:"pressed"`
		Say          string   `json:"say"`
		Again        string   `json:"again"`
		SendDisabled bool     `json:"sendDisabled"`
	} `json:"before"`
	After struct {
		Pressed      []string                  `json:"pressed"`
		Saved        map[string]map[string]any `json:"saved"`
		Say          string                    `json:"say"`
		SendDisabled bool                      `json:"sendDisabled"`
	} `json:"after"`
	Asked    []string `json:"asked"`
	SentMark bool     `json:"sentMark"`
	Reply    string   `json:"reply"`
	Exit     struct {
		There    bool   `json:"there"`
		Stuck    string `json:"stuck"`
		OnScreen bool   `json:"onScreen"`
		Left     int    `json:"left"`
	} `json:"exit"`
}

// The half of a brief that is not questions is the half that earns it: the
// facts with their sources, the cost under every option and the reading marked
// as a proposal. A screen that draws the questions and drops the rest turns a
// document into a form, and nothing about that is loud.
func TestBriefDrawsWhatTheAnswerRestsOn(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	if got.Before.Title != "Seven questions after twelve" {
		t.Errorf("the masthead reads %q", got.Before.Title)
	}
	if len(got.Before.Questions) != 3 {
		t.Errorf("questions drawn: %v", got.Before.Questions)
	}
	if got.Before.Facts != 4 {
		t.Errorf("%d facts drawn, the document carries four", got.Before.Facts)
	}
	if got.Before.Flagged != 1 {
		t.Errorf("%d facts marked as the one that changes the answer", got.Before.Flagged)
	}
	if len(got.Before.Sources) != 4 || got.Before.Sources[0] != "stack 5" {
		t.Errorf("the sources are not beside their facts: %v", got.Before.Sources)
	}
	if got.Before.Costs != 5 {
		t.Errorf("%d options carry what they cost, the document gives five", got.Before.Costs)
	}
	if got.Before.Reading == 0 {
		t.Error("the reading of the session is not on the screen at all")
	}
	if len(got.Before.Chips) != 2 {
		t.Errorf("chips drawn: %v", got.Before.Chips)
	}
	if len(got.Before.Summary) != 3 || got.Before.Lineage == 0 || got.Before.Closing == 0 {
		t.Errorf("the summary, the lineage or the closing went missing: %+v", got.Before)
	}
}

// A question that asks nothing takes no answer: a field under it would invite
// one the document has nowhere to put.
func TestBriefGivesNoFieldToABlockThatAsksNothing(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	if got.Before.Captures != 2 {
		t.Errorf("%d answer fields for two questions and one block that asks nothing", got.Before.Captures)
	}
}

// What the session already knew was decided comes back pressed, so a reissued
// document does not ask again for what is settled.
func TestBriefShowsTheAnswerItAlreadyHad(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	if len(got.Before.Pressed) != 1 || got.Before.Pressed[0] != "B" {
		t.Errorf("the answer already given is not shown as given: %v", got.Before.Pressed)
	}
	if !strings.Contains(got.Before.Say, "1") {
		t.Errorf("the dock does not count the answer already given: %q", got.Before.Say)
	}
}

// A pick is saved by the panel and not kept in the tab: a brief is answered
// over hours and from more than one device.
func TestBriefSavesAPickToThePanel(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	if len(got.After.Pressed) != 2 {
		t.Errorf("after a pick the screen shows %v pressed", got.After.Pressed)
	}
	if got.After.Saved == nil || got.After.Saved["r1"] == nil {
		t.Fatalf("the pick never reached the panel: %+v", got.After.Saved)
	}
	sent := false
	for _, call := range got.Asked {
		if strings.HasPrefix(call, "PUT /api/briefs/") {
			sent = true
		}
	}
	if !sent {
		t.Errorf("no draft was saved: %v", got.Asked)
	}
}

// Sending goes through the action gate and is marked afterwards, so a brief
// already answered does not come back looking untouched.
func TestBriefSendsThroughTheGateAndRemembersGoing(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	gate := false
	for _, call := range got.Asked {
		if strings.HasPrefix(call, "POST /api/actions") {
			gate = true
		}
	}
	if !gate {
		t.Errorf("the answers went somewhere other than through the gate: %v", got.Asked)
	}
	if !got.SentMark {
		t.Error("a brief that went into the session does not remember going")
	}
}

type standInShot struct {
	First struct {
		Say          string   `json:"say"`
		SendDisabled bool     `json:"sendDisabled"`
		Offered      []string `json:"offered"`
		Elsewhere    bool     `json:"elsewhere"`
	} `json:"first"`
	Sent []struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
		Params struct {
			Text string `json:"text"`
		} `json:"params"`
	} `json:"sent"`
	Jumped []string `json:"jumped"`
	After  string   `json:"after"`
}

// The answers are a message, and a message starts a conversation: the screen
// follows them into the session instead of leaving the person on a document
// that has nothing left to do.
func TestBriefFollowsTheAnswersIntoTheSession(t *testing.T) {
	var got standInShot
	runFixture(t, "briefstandin.html", &got)

	if len(got.Jumped) == 0 {
		t.Fatalf("the answers went and the screen stayed on the document: %+v", got)
	}
	if got.Jumped[0] != "carrying-on" {
		t.Errorf("the screen went into %q, not into the session the answers went to", got.Jumped[0])
	}
}

// A brief is read over hours and answered days later, by which time the session
// that wrote it is usually gone. The answers go to a session still working in
// the same directory, because that is who carries the work the brief asks
// about — a dead author must not turn an answered document into a dead end.
func TestBriefAnswersReachASessionStandingInForTheAuthor(t *testing.T) {
	var got standInShot
	runFixture(t, "briefstandin.html", &got)

	if got.First.SendDisabled {
		t.Error("the author is gone and the send button is dead, so the answers have nowhere to go")
	}
	if len(got.Sent) == 0 {
		t.Fatalf("nothing was sent: %+v", got)
	}
	if got.Sent[0].Target != "carrying-on" {
		t.Errorf("the answers went to %q, not to a session in the directory of the brief", got.Sent[0].Target)
	}
	if !strings.Contains(got.Sent[0].Params.Text, "The ceilings") {
		t.Errorf("what went is not the answers: %q", got.Sent[0].Params.Text)
	}
	if !strings.Contains(got.First.Say, "carrying-on") {
		t.Errorf("the screen does not say where the answers go: %q", got.First.Say)
	}
}

// Where the answers go is the person's to change: a directory can hold more
// than one session, and only the person knows which one is theirs.
func TestBriefLetsThePersonPickAmongTheSessionsInTheDirectory(t *testing.T) {
	var got standInShot
	runFixture(t, "briefstandin.html", &got)

	if len(got.First.Offered) != 2 {
		t.Errorf("sessions offered: %v", got.First.Offered)
	}
	if got.First.Elsewhere {
		t.Error("a session working in another directory is offered the answers of this brief")
	}
	if len(got.Sent) < 2 {
		t.Fatalf("the second send never happened: %+v", got.Sent)
	}
	if got.Sent[1].Target != "also-here" {
		t.Errorf("the pick was ignored: the answers went to %q", got.Sent[1].Target)
	}
}

// A document published a second time under the same name replaces the text the
// person already answered. They are owed the fact and the time; what changed in
// the text, only they can judge.
func TestAReissuedBriefSaysSoAboveTheAnswers(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	if got.Before.Again == "" {
		t.Fatal("the document was republished under the given answers and the head says nothing")
	}
	if !strings.Contains(got.Before.Again, "republished") {
		t.Errorf("the mark does not say what happened: %q", got.Before.Again)
	}
	if !strings.Contains(got.Before.Again, "first published") {
		t.Errorf("the mark does not say when the document first went out: %q", got.Before.Again)
	}
	if !strings.Contains(got.Before.Again, "answers are where you left them") {
		t.Errorf("the mark leaves the person guessing what happened to their answers: %q", got.Before.Again)
	}
}

// A brief is read for as long as it takes, and putting it down is part of
// reading it. The way out rides with the page: a document of a dozen screens
// whose only exit is at the top is a document the reader scrolls back through
// to leave.
func TestABriefCanBePutDown(t *testing.T) {
	var got briefShot
	runFixture(t, "brief.html", &got)

	if !got.Exit.There {
		t.Fatal("the document has no way out of it")
	}
	if got.Exit.Stuck != "sticky" {
		t.Errorf("the way out is %q and scrolls away with the text", got.Exit.Stuck)
	}
	if !got.Exit.OnScreen {
		t.Error("the way out is off the screen in the middle of the document")
	}
	if got.Exit.Left != 1 {
		t.Errorf("pressing it left the reader on the page %d times out of one", got.Exit.Left)
	}
}
