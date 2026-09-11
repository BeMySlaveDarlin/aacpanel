package executor

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

func TestSessionCloseFindsNodeInstalledClaude(t *testing.T) {
	ctx := context.Background()
	cli := []string{"node", "/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js",
		"-n", "probe", "--remote-control", "probe"}

	t.Run("the session closes", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 3001, comm: "node", args: cli, ppid: 1,
				cwd: "/srv/proj/Beta/rnd/probe", start: "5150"},
		)
		sessionFiles(t, fakeSession{pid: 3001, name: "probe", start: "5150"})
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{3001: true}, map[int]int{3001: 1})

		if _, err := e.Execute(ctx, req(action.SessionClose, "probe")); err != nil {
			t.Fatalf("the session of an npm install was not found: %v", err)
		}
		if len(log.sent) == 0 {
			t.Error("no signal went out to anyone — the session was found but not closed")
		}
	})

	t.Run("a file left by a dead namesake gives no signal", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 3002, comm: "node", args: []string{"node", "other.js"}, ppid: 1,
				cwd: "/srv/proj/other", start: "9999"},
		)
		sessionFiles(t, fakeSession{pid: 3002, name: "probe", start: "1111"})
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{3002: true}, map[int]int{3002: 1})

		_, err := e.Execute(ctx, req(action.SessionClose, "probe"))
		if err == nil {
			t.Fatal("an unrelated node was closed as a claude session")
		}
		if len(log.sent) > 0 {
			t.Errorf("signals %v went out — to a process that reused the number", log.sent)
		}
	})

	t.Run("a one-shot run did not become a session", func(t *testing.T) {
		procFS(t,
			fakeProc{pid: 3003, comm: "node", args: []string{"node", "cli.js", "-p", "ask"},
				ppid: 1, cwd: "/srv/proj/Beta/rnd/probe", start: "4242"},
		)
		sessionFiles(t, fakeSession{pid: 3003, name: "tmp-8b", start: "4242"})
		e, _ := newTest(t, "")
		log := withSignals(t, e, map[int]bool{3003: true}, map[int]int{3003: 1})

		_, err := e.Execute(ctx, req(action.SessionClose, "tmp-8b"))
		if err == nil {
			t.Fatal("a one-shot run was closed as a session")
		}
		if strings.Contains(err.Error(), "tmp-8b,") || strings.Contains(err.Error(), ", tmp-8b") {
			t.Errorf("a one-shot run got into the list of running sessions: %v", err)
		}
		if len(log.sent) > 0 {
			t.Errorf("signals %v went out — to a one-shot run", log.sent)
		}
	})
}
