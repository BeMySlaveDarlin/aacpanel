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
