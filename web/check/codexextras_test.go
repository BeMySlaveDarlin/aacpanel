package check

import (
	"reflect"
	"testing"
)

// What else codex does in a turn — a picture looked at, web searches, a wait
// it set itself, what a hook put in, messages exchanged with an agent — stands
// in one run among the calls, each of its kind; the last answer of an agent
// is a letter from it, with its words.
func TestTheOtherToolsOfCodexAreCallsAndTheLastAnswerOfAnAgentALetter(t *testing.T) {
	var got struct {
		Runs   [][]string `json:"runs"`
		Letter struct {
			Label string `json:"label"`
			Who   string `json:"who"`
			Body  string `json:"body"`
		} `json:"letter"`
	}
	runFixture(t, "codexextras.html", &got)

	if want := [][]string{{"files", "web", "time", "hook", "agents"}}; !reflect.DeepEqual(got.Runs, want) {
		t.Errorf("the calls fold into the runs %v, want %v", got.Runs, want)
	}
	if got.Letter.Label != "from agent" || got.Letter.Who != "checker" ||
		got.Letter.Body != "The login screen keeps the token in the page." {
		t.Errorf("the last answer of the agent reads %+v", got.Letter)
	}
}
