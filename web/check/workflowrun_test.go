package check

import (
	"strings"
	"testing"
)

type flowShot struct {
	Before struct {
		Chips     int    `json:"chips"`
		Label     string `json:"label"`
		Num       string `json:"num"`
		Icon      bool   `json:"icon"`
		TaskChip  string `json:"taskChip"`
		AgentChip string `json:"agentChip"`
	} `json:"before"`
	List struct {
		Head   string   `json:"head"`
		Sub    string   `json:"sub"`
		Rows   []string `json:"rows"`
		States []string `json:"states"`
	} `json:"list"`
	Run struct {
		Title   string   `json:"title"`
		State   string   `json:"state"`
		Nums    []string `json:"nums"`
		Labels  []string `json:"labels"`
		Phases  []string `json:"phases"`
		Details []string `json:"details"`
		Logs    []string `json:"logs"`
		Result  string   `json:"result"`
		Script  string   `json:"script"`
	} `json:"run"`
	Back struct {
		Rows int `json:"rows"`
	} `json:"back"`
	LiveRun struct {
		State  string   `json:"state"`
		Phases []string `json:"phases"`
		Hint   string   `json:"hint"`
		Nums   []string `json:"nums"`
	} `json:"liveRun"`
}

// A workflow is a third thing under the composer, beside the background work
// and the subagents. Ninety agents of one run in the list of subagents bury
// the two the session started itself; one line in the list of background jobs
// says that something is running and nothing else.
func TestWorkflowRunsGetAChipOfTheirOwn(t *testing.T) {
	var got flowShot
	runFixture(t, "workflowrun.html", &got)

	if got.Before.Chips != 3 {
		t.Fatalf("chips under the composer: %d, the runs have no button of their own", got.Before.Chips)
	}
	if !got.Before.Icon {
		t.Error("the chip of the runs carries no icon")
	}
	if got.Before.Num != "1" {
		t.Errorf("the chip counts %q running, one of the two runs is going", got.Before.Num)
	}
	if !strings.Contains(got.Before.Label, "workflow") {
		t.Errorf("the chip does not name what it opens: %q", got.Before.Label)
	}
	for _, chip := range []string{got.Before.TaskChip, got.Before.AgentChip} {
		if strings.Contains(chip, "workflow") {
			t.Errorf("a run is counted among something else: %q", chip)
		}
	}
	if !strings.Contains(got.Before.TaskChip, "none") || !strings.Contains(got.Before.AgentChip, "none") {
		t.Errorf("the runs leaked into the lists beside them: %q, %q", got.Before.TaskChip, got.Before.AgentChip)
	}
}

func TestWorkflowListSaysWhereEachRunStands(t *testing.T) {
	var got flowShot
	runFixture(t, "workflowrun.html", &got)

	if !strings.Contains(got.List.Head, "workflow") {
		t.Errorf("the list does not name itself: %q", got.List.Head)
	}
	if !strings.Contains(got.List.Sub, "1 running") || !strings.Contains(got.List.Sub, "1 over") {
		t.Errorf("the head of the list does not say what is going and what is done: %q", got.List.Sub)
	}
	if len(got.List.Rows) != 2 {
		t.Fatalf("rows drawn: %v", got.List.Rows)
	}
	// The row names the workflow, not the run id: an id says nothing to
	// the person who asked for the work.
	if !strings.Contains(got.List.Rows[0], "review-changes") {
		t.Errorf("the first row does not name its workflow: %q", got.List.Rows[0])
	}
	if !strings.Contains(got.List.Rows[0], "7 agents") {
		t.Errorf("the row of a live run does not say how wide it has fanned out: %q", got.List.Rows[0])
	}
	if want := []string{"running", "completed"}; len(got.List.States) != 2 ||
		got.List.States[0] != want[0] || got.List.States[1] != want[1] {
		t.Errorf("the rows say %v, the runs are %v", got.List.States, want)
	}
}

func TestWorkflowScreenShowsThePhasesTheLogAndTheResult(t *testing.T) {
	var got flowShot
	runFixture(t, "workflowrun.html", &got)

	if !strings.Contains(got.Run.Title, "audit-skills") {
		t.Errorf("the screen of the run does not name it: %q", got.Run.Title)
	}
	if got.Run.State != "completed" {
		t.Errorf("the state of the finished run reads %q", got.Run.State)
	}
	if want := []string{"Audit Agents", "Verify", "Synthesize"}; strings.Join(got.Run.Phases, "|") != strings.Join(want, "|") {
		t.Errorf("phases drawn: %v, the script declares %v", got.Run.Phases, want)
	}
	if len(got.Run.Details) == 0 || !strings.Contains(got.Run.Details[0], "per-agent") {
		t.Errorf("a phase is drawn without what it is for: %v", got.Run.Details)
	}
	if len(got.Run.Logs) != 2 || !strings.Contains(got.Run.Logs[0], "stalled") {
		t.Errorf("the log of the run is not on the screen: %v", got.Run.Logs)
	}
	if !strings.Contains(got.Run.Result, "confirmed_high") {
		t.Errorf("what the run returned is not on the screen: %q", got.Run.Result)
	}
	if !strings.Contains(got.Run.Script, "audit-skills.js") {
		t.Errorf("the screen does not say which script ran: %q", got.Run.Script)
	}
	// The counters of the run, in the words of the panel: 95 agents, the
	// tokens they read, the calls they made and how long it all took.
	if len(got.Run.Nums) != 4 || got.Run.Nums[0] != "95" {
		t.Errorf("the figures of the run: %v", got.Run.Nums)
	}
	if strings.Join(got.Run.Labels, " ") != "agents read tool calls long" {
		t.Errorf("the figures are not labelled: %v", got.Run.Labels)
	}
	if got.Back.Rows != 2 {
		t.Errorf("the way back from a run does not land on the list: %d rows", got.Back.Rows)
	}
}

// A run in flight writes no record of itself: the harness leaves the snapshot
// when it is over. The phases still come — they are in the script it was
// launched with — and the screen says outright that where it stands is not
// something the run will tell.
func TestALiveRunShowsItsPhasesAndAdmitsWhatItCannotSay(t *testing.T) {
	var got flowShot
	runFixture(t, "workflowrun.html", &got)

	if got.LiveRun.State != "running" {
		t.Errorf("the live run reads %q", got.LiveRun.State)
	}
	if strings.Join(got.LiveRun.Phases, "|") != "Review|Verify" {
		t.Errorf("the phases of the live run: %v", got.LiveRun.Phases)
	}
	if !strings.Contains(got.LiveRun.Hint, "until it is over") {
		t.Errorf("the screen does not say that the phase in flight is unknown: %q", got.LiveRun.Hint)
	}
	if len(got.LiveRun.Nums) == 0 || got.LiveRun.Nums[0] != "7" {
		t.Errorf("the live run does not count its agents: %v", got.LiveRun.Nums)
	}
}
