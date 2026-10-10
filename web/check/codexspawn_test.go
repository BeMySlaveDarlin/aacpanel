package check

import (
	"reflect"
	"strings"
	"testing"
)

type spawnAgentShot struct {
	Name  string `json:"name"`
	Note  string `json:"note"`
	Tag   string `json:"tag"`
	Tone  string `json:"tone"`
	Opens bool   `json:"opens"`
}

type spawnCardShot struct {
	Label  string           `json:"label"`
	Agents []spawnAgentShot `json:"agents"`
	More   string           `json:"more"`
	Task   string           `json:"task"`
}

// An agent codex started stands in the feed of the thread that started it as
// a card of its start: the agent by its nickname, its role, the model it runs
// on and how it stands as the last call to it said — a wait that heard it
// finish, one that heard it fail — with the task folded under it until a tap;
// a start that failed says so. The waits stand among the calls of their run,
// the command after them in the same run, and how the agents stand is drawn
// nowhere of its own. The agent opens the feed of its own thread, asked for by
// the id of that thread, and an agent it started opens over it the same way.
func TestAnAgentOfCodexIsACardThatOpensItsThread(t *testing.T) {
	const (
		worker = "01a12346-0000-7000-8000-0000000000a1"
		helper = "01a12347-0000-7000-8000-0000000000b1"
		parent = "01a12345-0000-7000-8000-00000000abcd"
	)
	var got struct {
		Cards  []spawnCardShot `json:"cards"`
		Hidden int             `json:"hidden"`
		Runs   [][]string      `json:"runs"`
		Opened struct {
			Expanded string `json:"expanded"`
			spawnCardShot
		} `json:"opened"`
		Agent struct {
			Name string   `json:"name"`
			Sub  string   `json:"sub"`
			Word string   `json:"word"`
			Feed []string `json:"feed"`
		} `json:"agent"`
		Nested string   `json:"nested"`
		Asked  []string `json:"asked"`
	}
	runFixture(t, "codexspawn.html", &got)

	want := []spawnCardShot{
		{Label: "agent started", More: "The task | 61 chars", Agents: []spawnAgentShot{
			{Name: "Euclid", Note: "worker · gpt-6-astra medium · finished", Tag: "✓", Tone: "s-ok", Opens: true}}},
		{Label: "agent started", More: "The task | 28 chars", Agents: []spawnAgentShot{
			{Name: "Hopper", Note: "checker · gpt-6-astra low · failed", Tag: "✗", Tone: "s-crit", Opens: true}}},
		{Label: "agent did not start", More: "The task | 29 chars", Agents: []spawnAgentShot{}},
	}
	if !reflect.DeepEqual(got.Cards, want) {
		t.Errorf("the starts of the agents read\n%+v\nwant\n%+v", got.Cards, want)
	}
	if got.Hidden != 0 {
		t.Errorf("how the agents stand is drawn as %d rows of its own", got.Hidden)
	}
	if !reflect.DeepEqual(got.Runs, [][]string{{"agents", "bash"}}) {
		t.Errorf("the calls fold into the runs %v: the waits and the command after them are one run", got.Runs)
	}
	if got.Opened.Expanded != "true" || got.Opened.More != "Fold the task | 61 chars" ||
		got.Opened.Task != "Read src/parser and list what it misses. | Answer in one line." {
		t.Errorf("the task opened reads %+v", got.Opened)
	}

	if got.Agent.Name != "Euclid" || got.Agent.Word != "codex" || !strings.Contains(got.Agent.Sub, "gpt-6-astra") {
		t.Errorf("the head of the feed of the agent reads %+v", got.Agent)
	}
	if !reflect.DeepEqual(got.Agent.Feed, []string{"The parser misses escapes."}) {
		t.Errorf("the feed of the agent reads %v", got.Agent.Feed)
	}
	if got.Nested != "Noether" {
		t.Errorf("the agent of the agent opened as %q", got.Nested)
	}
	wantAsked := []string{parent, worker, helper}
	var ids []string
	for _, id := range got.Asked {
		if len(ids) == 0 || ids[len(ids)-1] != id {
			ids = append(ids, id)
		}
	}
	if !reflect.DeepEqual(ids, wantAsked) {
		t.Errorf("the feeds were asked for by %v, want the threads %v by their ids", got.Asked, wantAsked)
	}
}

type pastHeadShot struct {
	Desk bool   `json:"desk"`
	Sub  string `json:"sub"`
	Bar  bool   `json:"bar"`
}

// A thread of codex from the archive heads its conversation with what it
// spent, as its row in the archive says it, and with no fill of a context the
// archive of codex never kept — no share, no peak, no bar; the sheet of its
// tools says the same. A conversation of claude keeps its peak.
func TestAPastThreadOfCodexIsHeadedByWhatItSpent(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Codex  pastHeadShot `json:"codex"`
				Claude pastHeadShot `json:"claude"`
				Tools  []string     `json:"tools"`
			}
			screen.run(t, "codexpasthead.html", &got)

			if got.Codex.Desk != (screen.name == "desk") {
				t.Fatalf("the %s drew the head of the %s: %+v", screen.name, map[bool]string{true: "desk", false: "phone"}[got.Codex.Desk], got.Codex)
			}
			if !strings.Contains(got.Codex.Sub, "29.4m tokens") || strings.Contains(got.Codex.Sub, "%") ||
				strings.Contains(got.Codex.Sub, "peak") || got.Codex.Bar {
				t.Errorf("the head of the thread of codex reads %+v: what it spent, and no fill", got.Codex)
			}
			if !strings.Contains(got.Claude.Sub, "40") || !strings.Contains(got.Claude.Sub, "%") || !got.Claude.Bar {
				t.Errorf("the head of the conversation of claude lost its peak: %+v", got.Claude)
			}
			if screen.name == "phone" && !reflect.DeepEqual(got.Tools, []string{"Spent: 29.4m tokens"}) {
				t.Errorf("the sheet of the tools of the thread says %v", got.Tools)
			}
		})
	}
}
