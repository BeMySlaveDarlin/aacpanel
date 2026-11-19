package act

import (
	"strings"
	"testing"

	"aacpanel/internal/question"
)

func TestAskRequiresTheWholeAnswerAndBuildsOneLetter(t *testing.T) {
	row := Record{ID: strings.Repeat("a", 32), Kind: "ask", Body: Body{Source: "prose", Questions: []question.Question{
		{Question: "Which checks?", Multi: true, Options: []question.Option{{Label: "Unit"}, {Label: "Integration"}}},
		{Question: "Which branch?"},
		{Question: "Deploy?", Options: []question.Option{{Label: "Yes"}, {Label: "No"}}},
	}}}
	valid := []question.Answer{{Picks: []string{"Unit", "Integration"}, Text: " include smoke "}, {Text: "main"}, {Picks: []string{"Yes"}}}
	if err := row.ValidateAnswer("", "", valid...); err != nil {
		t.Fatal(err)
	}
	for _, answers := range [][]question.Answer{
		nil, valid[:2], append(append([]question.Answer{}, valid...), question.Answer{Text: "extra"}),
		{{Picks: []string{"Unit"}}, {Text: " "}, {Picks: []string{"Yes"}}},
		{{Picks: []string{"Unit", "Other"}}, {Text: "main"}, {Picks: []string{"Yes"}}},
		{{Picks: []string{"Unit", "Unit"}}, {Text: "main"}, {Picks: []string{"Yes"}}},
		{{Picks: []string{"Unit"}}, {Picks: []string{"main"}}, {Picks: []string{"Yes"}}},
		{{Picks: []string{"Unit"}}, {Text: "main"}, {Picks: []string{"Yes", "No"}}},
	} {
		if err := row.ValidateAnswer("", "", answers...); err == nil {
			t.Errorf("invalid answers accepted: %+v", answers)
		}
	}
	want := "The person answered the questions of your last message (" + row.ID + "):\n1. Which checks?\n   Unit, Integration; include smoke\n2. Which branch?\n   main\n3. Deploy?\n   Yes"
	if got := row.Reply("", "", valid...); got != want {
		t.Fatalf("letter: %q, want %q", got, want)
	}
	row.Body.Source = "tool"
	if got := row.Reply("", "", valid...); !strings.HasPrefix(got, "The person answered your ask (") {
		t.Fatalf("tool letter: %q", got)
	}
}

// A question put away carries no answer and tells the session it will be
// answered in the conversation, naming each question.
func TestAnAskPutAwayCarriesNoAnswerAndNamesItsQuestions(t *testing.T) {
	row := Record{ID: strings.Repeat("b", 32), Kind: "ask", Body: Body{Source: "tool", Questions: []question.Question{
		{Question: "Who fixes it?", Options: []question.Option{{Label: "Max"}, {Label: "infra"}}},
		{Question: "When?"},
	}}}
	if err := row.ValidateAnswer("decline", ""); err != nil {
		t.Fatalf("a question put away is refused: %v", err)
	}
	if err := row.ValidateAnswer("decline", "", question.Answer{Picks: []string{"Max"}}, question.Answer{Text: "now"}); err == nil {
		t.Error("a question put away went with answers")
	}
	want := "The person put your ask (" + row.ID + ") away and will answer in the conversation:\n1. Who fixes it?\n2. When?"
	if got := row.Reply("decline", ""); got != want {
		t.Fatalf("letter: %q, want %q", got, want)
	}
}
