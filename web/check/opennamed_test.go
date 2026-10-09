package check

import "testing"

// A New of a project whose agent is codex brings up a thread named after its
// id, not after the project: the wait for it follows the name the executor
// answered with, or the screen would say "opening" until the ceiling.
func TestANewWaitsForTheSessionTheExecutorNamed(t *testing.T) {
	var got struct {
		NamedUp     bool `json:"namedUp"`
		NamedNotYet bool `json:"namedNotYet"`
		PlainUp     bool `json:"plainUp"`
		PlainNotYet bool `json:"plainNotYet"`
	}
	runFixture(t, "opennamed.html", &got)
	if !got.NamedUp {
		t.Error("the thread the executor named came up and the wait for it goes on")
	}
	if got.NamedNotYet {
		t.Error("a session of the project's name settled the wait for a thread named otherwise")
	}
	if !got.PlainUp {
		t.Error("without a name in the answer the new session of the project does not settle the wait")
	}
	if got.PlainNotYet {
		t.Error("without a name in the answer a session of another name settled the wait")
	}
}
