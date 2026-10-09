package executor

import (
	"syscall"
	"testing"
	"time"
)

// A process that has ended waits in the table until its parent collects it,
// and a signal still finds it there: a close that took that for a live agent
// would wait out its bound and say the session did not close.
func TestAnEndedAgentIsGoneBeforeItsParentCollectsIt(t *testing.T) {
	procFS(t,
		fakeProc{pid: 4242, comm: "claude", ppid: 4200, state: "Z"},
		fakeProc{pid: 4343, comm: "claude", ppid: 4200},
	)
	e := &Executor{poll: time.Millisecond}
	e.sendSignal = func(int, syscall.Signal) error { return nil }

	if !e.waitGone(t.Context(), 4242, time.Second) {
		t.Error("an agent that ended and waits to be collected was taken for a live one")
	}
	if e.waitGone(t.Context(), 4343, 20*time.Millisecond) {
		t.Error("a running agent was taken for one that ended")
	}
}
