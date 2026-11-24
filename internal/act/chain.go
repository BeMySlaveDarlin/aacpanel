package act

import (
	"errors"
	"fmt"
	"time"

	"aacpanel/internal/request"
)

// Notice survives a service restart between saving a step and waking its agent.
type Notice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (r Record) Chain() bool { return r.Kind == "do" && r.Step != nil && len(r.Body.Chain) > 0 }

func (r Record) Current(step int, who string) (request.Step, error) {
	if !r.Chain() || r.State != "open" || *r.Step != step || step < 1 || step > len(r.Body.Chain) || r.Body.Chain[step-1].Who != who {
		return request.Step{}, errors.New("this is not the current open step for this executor")
	}
	return r.Body.Chain[step-1], nil
}

func (r *Record) Advance(outcome, note string, now time.Time) {
	index := *r.Step - 1
	out := r.Outputs[index]
	if out == nil {
		out = &request.Output{}
		r.Outputs[index] = out
	}
	out.State, out.Note, out.Ended = outcome, note, &now
	if outcome == "cant" {
		r.State, r.Closed = "declined", &now
		return
	}
	if outcome == "failed" {
		r.State = "failed"
		return
	}
	if index+1 == len(r.Body.Chain) {
		r.State, r.Closed = "answered", &now
		return
	}
	next := index + 2
	r.Step = &next
}

// AgentsMove reports whether an open chain stands on the agent's turn: a step
// of the agent's, or a step that failed and that the session reviews. The
// person's move it is not.
func (r Record) AgentsMove() bool {
	if !r.Chain() || !r.Open() {
		return false
	}
	if r.State == "failed" {
		return true
	}
	step := *r.Step
	return step >= 1 && step <= len(r.Body.Chain) && r.Body.Chain[step-1].Who == "agent"
}

// CommandRunning keeps a launched command off the person's waiting work:
// reserving its terminal already hands the step over to the executor.
func (r Record) CommandRunning() bool {
	if !r.Chain() || r.State != "open" {
		return false
	}
	index := *r.Step - 1
	return index >= 0 && index < len(r.Body.Chain) && index < len(r.Outputs) &&
		r.Body.Chain[index].Who == "you" && r.Body.Chain[index].Command != "" && r.Outputs[index].Running()
}

// Cancel is the outcome of a chain the person cancelled from its card.
const Cancel = "cancel"

// Cancelled reports whether the person cancelled the chain. It is withdrawn,
// as a chain its session takes back is, and its answer says the person did
// it: answered_by alone holds a session's name as well.
func (r Record) Cancelled() bool {
	return r.State == "withdrawn" && r.Answer != nil && r.Answer.Outcome == Cancel
}

func (r Record) WakesAgent() bool {
	return r.State != "open" || r.Body.Chain[*r.Step-1].Who == "agent"
}

func (r Record) NextMessage(closed int) string {
	if r.State == "answered" {
		return "The chain is done."
	}
	if r.State == "failed" {
		return "It failed; reopen, withdraw or start another chain. End the turn if no independent work remains."
	}
	s := r.Body.Chain[*r.Step-1]
	if s.Who == "agent" {
		return fmt.Sprintf("Step %d is yours: %s", *r.Step, request.Sentence(s.Text))
	}
	return fmt.Sprintf("Step %d closed. Step %d is the person's — end the turn.", closed, *r.Step)
}
