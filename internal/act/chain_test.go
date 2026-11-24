package act

import (
	"testing"
	"time"

	"aacpanel/internal/request"
)

func TestCommandScheduledKeepsOnlyAnUnstartedHumanCommandQuiet(t *testing.T) {
	now := time.Now().UTC()
	from := now.Add(time.Hour).Format(time.RFC3339)
	until := now.Add(2 * time.Hour).Format(time.RFC3339)
	step := 1
	row := Record{Kind: "do", State: "open", Step: &step,
		Body: Body{Chain: []request.Step{{Who: "you", Command: "deploy", From: from, Until: until}}}, Outputs: make([]*request.Output, 1)}
	if !row.CommandScheduled(now) {
		t.Fatal("the future command is the person's move")
	}
	if gotFrom, gotUntil := row.CommandWindow(); gotFrom != from || gotUntil != until {
		t.Fatal("the window was lost")
	}
	opening, _ := time.Parse(time.RFC3339, from)
	if row.CommandScheduled(opening) || row.CommandScheduled(opening.Add(3*time.Hour)) {
		t.Fatal("an open or closed window is still scheduled")
	}
	row.Outputs[0] = &request.Output{State: "running"}
	if row.CommandScheduled(now) {
		t.Fatal("a launched command waits for its opening")
	}
	row.Outputs[0] = nil
	for _, state := range []string{"failed", "answered", "withdrawn"} {
		row.State = state
		if row.CommandScheduled(now) {
			t.Fatalf("a %s chain is scheduled", state)
		}
	}
	row.State = "open"
	row.Body.Chain[0].Command = ""
	if row.CommandScheduled(now) {
		t.Fatal("a manual step is scheduled")
	}
	row.Step = nil
	if row.CommandScheduled(now) {
		t.Fatal("a plain act is scheduled")
	}
}

func TestCommandRunningHandsOnlyTheCurrentHumanCommandToTheExecutor(t *testing.T) {
	for _, c := range []struct {
		name    string
		state   string
		step    int
		who     string
		command string
		output  *request.Output
		want    bool
	}{
		{"before Run", "open", 1, "you", "login", nil, false},
		{"pending without terminal", "open", 1, "you", "login", &request.Output{State: "pending"}, false},
		{"reserved terminal", "open", 1, "you", "login", &request.Output{State: "pending", Term: "t-login"}, true},
		{"running", "open", 1, "you", "login", &request.Output{State: "running"}, true},
		{"exited", "open", 1, "you", "login", &request.Output{State: "done", Term: "t-login"}, false},
		{"stopped", "open", 1, "you", "login", &request.Output{State: "stopped", Term: "t-login"}, false},
		{"failed output", "open", 1, "you", "login", &request.Output{State: "failed", Term: "t-login"}, false},
		{"failed chain", "failed", 1, "you", "login", &request.Output{State: "running"}, false},
		{"closed chain", "answered", 1, "you", "login", &request.Output{State: "running"}, false},
		{"manual step", "open", 1, "you", "", &request.Output{State: "running"}, false},
		{"agent step", "open", 1, "agent", "", &request.Output{State: "running"}, false},
		{"zero step", "open", 0, "you", "login", &request.Output{State: "running"}, false},
		{"step beyond chain", "open", 2, "you", "login", &request.Output{State: "running"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			row := Record{Kind: "do", State: c.state, Step: &c.step,
				Body: Body{Chain: []request.Step{{Who: c.who, Command: c.command}}}, Outputs: []*request.Output{c.output}}
			if got := row.CommandRunning(); got != c.want {
				t.Fatalf("command running = %v, want %v", got, c.want)
			}
			row.Outputs = nil
			if row.CommandRunning() {
				t.Fatal("a step without output is running")
			}
			row.Step = nil
			if row.CommandRunning() {
				t.Fatal("a plain act is running")
			}
		})
	}
}
