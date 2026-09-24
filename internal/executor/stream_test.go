package executor

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// holdStream stands a holder's state file for a conversation, with this test
// as the live holder of claude at pid.
func holdStream(t *testing.T, sessionID string, pid int) {
	t.Helper()
	run, err := os.MkdirTemp("/tmp", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run) })
	t.Setenv("XDG_RUNTIME_DIR", run)
	if err := os.MkdirAll(stream.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(stream.Summary{Protocol: stream.Protocol, SessionID: sessionID, PID: pid, Holder: os.Getpid()})
	if err := os.WriteFile(stream.StatePath(sessionID), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAStreamSessionCountsOnlyWhileItsHolderKeepsIt(t *testing.T) {
	ctx := context.Background()
	const id = "44444444-4444-4444-8444-444444444444"
	args := []string{"claude", "-p", "--input-format", "stream-json", "-n", "held", "--session-id", id}

	t.Run("held: a session like any other", func(t *testing.T) {
		procFS(t, fakeProc{pid: 3001, comm: "claude", args: args, ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "4242"})
		sessionFiles(t, fakeSession{pid: 3001, name: "held", start: "4242"})
		holdStream(t, id, 3001)
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{3001: true}, map[int]int{3001: 1})
		if _, err := e.Execute(ctx, req(action.SessionClose, "held")); err != nil {
			t.Fatalf("a held stream session was not found: %v", err)
		}
	})

	t.Run("not held: somebody else's run", func(t *testing.T) {
		procFS(t, fakeProc{pid: 3002, comm: "claude", args: args, ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "4243"})
		sessionFiles(t, fakeSession{pid: 3002, name: "held", start: "4243"})
		e, _ := newTest(t, "")
		withSignals(t, e, map[int]bool{3002: true}, map[int]int{3002: 1})
		if _, err := e.Execute(ctx, req(action.SessionClose, "held")); err == nil {
			t.Fatal("a stream run nobody holds — an SDK reviewer, a script — was taken for a session and closed")
		}
	})
}
