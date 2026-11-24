package act

import (
	"testing"

	"aacpanel/internal/request"
)

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
