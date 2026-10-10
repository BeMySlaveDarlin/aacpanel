package check

import (
	"reflect"
	"testing"
)

type secondCardShot struct {
	Label  string           `json:"label"`
	Agents []spawnAgentShot `json:"agents"`
	Task   bool             `json:"task"`
}

type sideLineShot struct {
	From string `json:"from"`
	Said string `json:"said"`
	Tone string `json:"tone"`
}

// An agent a codex thread started with the second set of codex's tools stands
// in its feed as the card of its start: by the name its path gives it, its
// role where the call asked for one, the model it runs on and how it stands
// after everything that happened to it — the one that finished its turn
// finished, the one interrupted and given a task more at work again — with no
// task to unfold, since the second set sends the task encrypted. The calls to
// the agents stand in one run among the calls, a wait the person stopped the
// turn in failed, and an agent interrupted and an agent finished are lines of
// the feed; how the agents stand is drawn nowhere of its own.
func TestAnAgentOfTheSecondSetOfCodexIsACardAndItsCallsAreCalls(t *testing.T) {
	var got struct {
		Cards  []secondCardShot `json:"cards"`
		Lines  []sideLineShot   `json:"lines"`
		Hidden int              `json:"hidden"`
		Runs   [][][]string     `json:"runs"`
	}
	runFixture(t, "codexagents.html", &got)

	want := []secondCardShot{
		{Label: "agent started", Agents: []spawnAgentShot{
			{Name: "lexer", Note: "explorer · gpt-6-astra low · finished", Tag: "✓", Tone: "s-ok", Opens: true}}},
		{Label: "agent started", Agents: []spawnAgentShot{
			{Name: "tests", Note: "gpt-6-astra medium · at work", Tag: "•", Opens: true}}},
	}
	if !reflect.DeepEqual(got.Cards, want) {
		t.Errorf("the starts of the agents read\n%+v\nwant\n%+v", got.Cards, want)
	}
	wantLines := []sideLineShot{{From: "agent tests", Said: "was interrupted", Tone: "s-warn"},
		{From: "agent lexer", Said: "finished", Tone: "s-ok"}}
	if !reflect.DeepEqual(got.Lines, wantLines) {
		t.Errorf("the lines of the agents read %+v, want %+v", got.Lines, wantLines)
	}
	if got.Hidden != 0 {
		t.Errorf("how the agents stand is drawn as %d rows of its own", got.Hidden)
	}
	wantRuns := [][][]string{{{"SendMessage lexer", "Wait", "InterruptAgent tests", "ListAgents", "FollowupTask tests", "Wait ✗"}}}
	if !reflect.DeepEqual(got.Runs, wantRuns) {
		t.Errorf("the calls fold into the runs %v, want %v", got.Runs, wantRuns)
	}
}
