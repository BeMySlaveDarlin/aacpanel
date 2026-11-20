package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/notify"
	personrequest "aacpanel/internal/request"
)

type chainExecutor struct {
	mu          sync.Mutex
	calls       []action.Request
	peek        action.TermPeekResult
	refuse      bool
	refuseClose bool
}

func (e *chainExecutor) Execute(_ context.Context, req action.Request) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, req)
	if req.Kind == action.SessionSend && e.refuse {
		return "", errors.New("the session is unavailable")
	}
	if req.Kind == action.TermClose && e.refuseClose {
		return "", errors.New("tmux did not answer")
	}
	if req.Kind == action.TermPeek {
		data, err := json.Marshal(e.peek)
		return string(data), err
	}
	return "accepted", nil
}
func (e *chainExecutor) captured(kind action.Kind) []action.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []action.Request{}
	for _, req := range e.calls {
		if req.Kind == kind {
			out = append(out, req)
		}
	}
	return out
}
func (e *chainExecutor) setExit(dead bool, code int, tail string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.peek = action.TermPeekResult{Dead: dead, Status: code, Tail: tail, Lines: 2}
}
func newChainServer(t *testing.T) (*Server, *chainExecutor) {
	t.Helper()
	probe := newPollProbe(t, t.Context(), t.Name())
	probe.srv.home = "/home/u"
	probe.srv.host = hostWith(t, strings.ReplaceAll(liveSnapshot, "/opt/x", "/srv/proj/shop/.worktrees/fix"))
	exec := &chainExecutor{}
	path := socketPath(t, "chain.sock")
	server := action.NewServer(path, exec, time.Second)
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); ln.Close(); <-done })
	probe.srv.exec = action.NewClient(path, time.Second)
	return probe.srv, exec
}
func chainAPI(t *testing.T, s *Server, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.apiAct(w, httptest.NewRequest(http.MethodPost, "/api/acts", strings.NewReader(string(data))))
	return w
}
func createChain(t *testing.T, s *Server, steps ...personrequest.Step) string {
	t.Helper()
	w := chainAPI(t, s, map[string]any{"conversation": requestConversation, "kind": "do", "title": "Release", "blocking": true, "body": map[string]any{"steps": steps}})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var reply struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &reply)
	pool, err := s.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM acts WHERE id=$1`, reply.ID) })
	row := chainRecord(t, s, reply.ID)
	if row.Step == nil || *row.Step != 1 || len(row.Outputs) != len(steps) || row.Outputs[0] != nil {
		t.Fatalf("new chain: %+v", row)
	}
	return reply.ID
}
func chainRecord(t *testing.T, s *Server, id string) act.Record {
	t.Helper()
	rows, err := s.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatal("the chain is absent")
	return act.Record{}
}
func chainPress(t *testing.T, s *Server, kind, id string, step int, answer string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"kind": kind, "target": "sentinel", "params": map[string]any{"act": id, "step": step, "answer": answer, "note": "No access"}})
	return post(t, s, string(data))
}
func chainPoll(t *testing.T, s *Server, id string) {
	t.Helper()
	if err := s.pollChain(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}
func chainReport(t *testing.T, s *Server, id string, step int, outcome string) *httptest.ResponseRecorder {
	t.Helper()
	return chainAPI(t, s, map[string]any{"conversation": requestConversation, "id": id, "step": step, "outcome": outcome, "note": "Checked"})
}

func TestChainRunsCommandsInConversationDirectoryAndWakesAtBoundariesPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Login", Command: "login --interactive"}, personrequest.Step{Who: "agent", Text: "Verify access"}, personrequest.Step{Who: "you", Text: "Approve deployment"}, personrequest.Step{Who: "you", Text: "Deploy", Command: "deploy"})
	if w := chainPress(t, s, "act.run", id, 2, ""); w.Code != http.StatusConflict {
		t.Fatal(w.Body.String())
	}
	for i := 0; i < 2; i++ {
		if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
			t.Fatalf("run: %d %s", w.Code, w.Body.String())
		}
	}
	starts := exec.captured(action.TermStart)
	if len(starts) != 1 || starts[0].Place != "/srv/proj/shop/.worktrees/fix" || starts[0].TermCommand != "login --interactive" || !action.TermID(starts[0].Target) {
		t.Fatalf("starts: %+v", starts)
	}
	if w := chainPress(t, s, "session.send", id, 1, "done"); w.Code != http.StatusConflict {
		t.Fatal("Done accepted a command")
	}
	if len(exec.captured(action.SessionSend)) != 0 {
		t.Fatal("starting a command woke the agent")
	}
	exec.setExit(true, 0, "signed in")
	chainPoll(t, s, id)
	chainPoll(t, s, id)
	row := chainRecord(t, s, id)
	sends := exec.captured(action.SessionSend)
	if *row.Step != 2 || row.Outputs[0].State != "done" || len(sends) != 1 || !strings.Contains(sends[0].Text, "Step 2 is yours") || !strings.Contains(sends[0].Text, id) {
		t.Fatalf("advance: %+v, sends %+v", row, sends)
	}
	if w := chainReport(t, s, id, 1, "done"); w.Code != http.StatusConflict {
		t.Fatal("wrong step accepted")
	}
	// The next human step pushes without sending another message to the model.
	pushStore := &pushStore{taken: make(chan time.Time, 4)}
	s.push = notify.New(pushStore, "")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.push.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	if w := chainReport(t, s, id, 2, "done"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "end the turn") {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	taken(t, pushStore, time.Second, "the next human step")
	if len(exec.captured(action.SessionSend)) != 1 {
		t.Fatal("a human step woke the agent")
	}
	if w := chainPress(t, s, "session.send", id, 3, "done"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	taken(t, pushStore, time.Second, "the following human step")
	if len(exec.captured(action.SessionSend)) != 1 {
		t.Fatal("consecutive human steps woke the agent")
	}
	if w := chainPress(t, s, "act.run", id, 4, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	exec.setExit(true, 0, "deployed")
	chainPoll(t, s, id)
	chainPoll(t, s, id)
	row = chainRecord(t, s, id)
	sends = exec.captured(action.SessionSend)
	if row.State != "answered" || row.Closed == nil || *row.Step != 4 || len(sends) != 2 || !strings.Contains(sends[1].Text, "The chain is done.") || strings.Contains(sends[1].Text, "1 you") || !strings.Contains(sends[1].Text, "2 agent") {
		t.Fatalf("end: %+v, sends %+v", row, sends)
	}
	if len(exec.captured(action.TermClose)) != 2 {
		t.Fatal("closed chain left terminals running")
	}
}

func TestChainFailureReopenStopAndCantPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Login", Command: "login"}, personrequest.Step{Who: "agent", Text: "Verify"})
	if w := chainReport(t, s, id, 1, "done"); w.Code != http.StatusConflict {
		t.Fatal("an agent reported a human step")
	}
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := chainPress(t, s, "session.send", id, 1, "cant"); w.Code != http.StatusConflict {
		t.Fatal("Can't accepted a running command")
	}
	exec.setExit(true, 3, "SSO denied")
	chainPoll(t, s, id)
	chainPoll(t, s, id)
	row := chainRecord(t, s, id)
	sends := exec.captured(action.SessionSend)
	if row.State != "failed" || row.Closed != nil || row.Outputs[0].Exit == nil || *row.Outputs[0].Exit != 3 || len(sends) != 1 || !strings.Contains(sends[0].Text, "SSO denied") || !strings.Contains(sends[0].Text, "Full log: /home/u/.local/state/aacpanel/acts/"+id+"/1.log") {
		t.Fatalf("failed: %+v %+v", row, sends)
	}
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusConflict {
		t.Fatal("failed step ran without reopen")
	}
	if w := chainAPI(t, s, map[string]any{"conversation": requestConversation, "id": id, "reopen": 2}); w.Code != http.StatusConflict {
		t.Fatal("wrong step reopened")
	}
	if w := chainAPI(t, s, map[string]any{"conversation": requestConversation, "id": id, "reopen": 1}); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if len(exec.captured(action.TermClose)) != 1 {
		t.Fatal("reopen left its terminal open")
	}
	chainPoll(t, s, id)
	if row := chainRecord(t, s, id); row.Outputs[0].State != "pending" || len(exec.captured(action.TermStart)) != 1 {
		t.Fatalf("reopen ran the step: %+v", row)
	}
	exec.setExit(false, 0, "waiting for login")
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := chainPress(t, s, "act.stop", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	row = chainRecord(t, s, id)
	if row.State != "open" || row.Outputs[0].State != "stopped" || row.Outputs[0].Tail != "waiting for login" || len(exec.captured(action.SessionSend)) != 1 {
		t.Fatalf("stop: %+v", row)
	}
	chainPoll(t, s, id)
	if len(exec.captured(action.TermStart)) != 2 {
		t.Fatal("Stop restarted the command")
	}
	if w := chainPress(t, s, "session.send", id, 1, "cant"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	row = chainRecord(t, s, id)
	sends = exec.captured(action.SessionSend)
	if row.State != "declined" || row.Closed == nil || len(sends) != 2 || !strings.Contains(sends[1].Text, "Can't: No access") {
		t.Fatalf("cant: %+v %+v", row, sends)
	}
}

func TestChainWithdrawClosesTerminalsAndKeepsLegacyActsPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Run", Command: "wait"})
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := chainAPI(t, s, map[string]any{"conversation": requestConversation, "id": id, "withdraw": true}); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if row := chainRecord(t, s, id); row.State != "withdrawn" || len(exec.captured(action.TermClose)) != 1 || len(exec.captured(action.SessionSend)) != 0 {
		t.Fatalf("withdraw: %+v", row)
	}
	legacy := newLegacyRequest(t, s)
	row := chainRecord(t, s, legacy)
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if row.Step != nil || row.Body.Steps != "Run reboot" || !strings.Contains(string(data), `"steps":"Run reboot"`) {
		t.Fatalf("legacy: %s", data)
	}
}

func TestChainNoticeRetriesAfterServiceRestartWithSameMessageIdentityPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Approve"}, personrequest.Step{Who: "agent", Text: "Check"}, personrequest.Step{Who: "agent", Text: "Finish"})
	exec.mu.Lock()
	exec.refuse = true
	exec.mu.Unlock()
	if w := chainPress(t, s, "session.send", id, 1, "done"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	before := chainRecord(t, s, id)
	if before.Notice == nil || *before.Step != 2 {
		t.Fatal("undelivered notice was lost")
	}
	exec.mu.Lock()
	exec.refuse = false
	exec.mu.Unlock()
	restarted := &Server{db: s.db, exec: s.exec, host: s.host, home: s.home}
	chainPoll(t, restarted, id)
	sends := exec.captured(action.SessionSend)
	if len(sends) != 2 || sends[0].MessageID != sends[1].MessageID || sends[0].Text != sends[1].Text || chainRecord(t, s, id).Notice != nil {
		t.Fatalf("retry: %+v", sends)
	}
	if w := chainReport(t, restarted, id, 2, "done"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Step 3 is yours") {
		t.Fatalf("consecutive agent step: %d %s", w.Code, w.Body.String())
	}
	if w := chainReport(t, restarted, id, 3, "done"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "The chain is done") {
		t.Fatal(w.Body.String())
	}
}

func TestChainTermPeekIsNeverABrowserAction(t *testing.T) {
	srv := &Server{}
	for _, term := range []bool{false, true} {
		w := httptest.NewRecorder()
		srv.runAction(w, httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(`{"kind":"term.peek","target":"t-1a2b3c4d"}`)), term)
		if w.Code != http.StatusForbidden {
			t.Fatalf("peek: %d %s", w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		srv.runAction(w, httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(fmt.Sprintf(`{"kind":"act.run","target":"sentinel","params":{"act":%q,"step":1}}`, strings.Repeat("a", 32)))), false)
		if w.Code != http.StatusNotFound {
			t.Fatalf("remote run: %d", w.Code)
		}
	}
}

func TestChainCurrentHumanCommandCanBeDeclinedBeforeRunPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Login", Command: "login"})
	if w := chainPress(t, s, "session.send", id, 1, "cant"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	row := chainRecord(t, s, id)
	if row.State != "declined" || len(exec.captured(action.TermStart)) != 0 || len(exec.captured(action.SessionSend)) != 1 {
		t.Fatalf("decline: %+v", row)
	}
}

func TestChainSavedPendingRunIsResumedOnceAfterServiceRestartPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Run", Command: "work"})
	lease, err := s.db.LockChain(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease.Record.Outputs[0] = &personrequest.Output{State: "pending", Term: "t-1234abcd", Started: &now}
	if err := lease.SaveChain(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted := &Server{db: s.db, exec: s.exec, host: s.host, home: s.home}
	if err := restarted.pollChains(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.pollChains(t.Context()); err != nil {
		t.Fatal(err)
	}
	starts := exec.captured(action.TermStart)
	if len(starts) != 1 || starts[0].ID != "t-1234abcd-start" || starts[0].Target != "t-1234abcd" || chainRecord(t, s, id).Outputs[0].State != "running" {
		t.Fatalf("resume: %+v", starts)
	}
}

// A command's terminal closes as soon as its exit status is read, while the
// chain goes on waiting on the agent's step or on the failed step.
func TestChainClosesACommandsTerminalOnceItsExitIsReadPG(t *testing.T) {
	for _, c := range []struct {
		name  string
		exit  int
		state string
		step  int
	}{
		{"exit zero", 0, "open", 2},
		{"nonzero exit", 3, "failed", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, exec := newChainServer(t)
			id := createChain(t, s, personrequest.Step{Who: "you", Text: "Login", Command: "login"}, personrequest.Step{Who: "agent", Text: "Verify"})
			if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
				t.Fatal(w.Body.String())
			}
			term := exec.captured(action.TermStart)[0].Target
			exec.setExit(true, c.exit, "signed in")
			chainPoll(t, s, id)
			chainPoll(t, s, id)
			row := chainRecord(t, s, id)
			out := row.Outputs[0]
			if row.State != c.state || *row.Step != c.step || row.Closed != nil || out.Exit == nil || *out.Exit != c.exit || out.Term != "" || out.Tail != "signed in" || out.Log == "" {
				t.Fatalf("after the exit: %+v %+v", row, out)
			}
			closes := exec.captured(action.TermClose)
			if len(closes) != 1 || closes[0].Target != term {
				t.Fatalf("closes of %s: %+v", term, closes)
			}
			// The capture is read before the pane goes.
			exec.mu.Lock()
			defer exec.mu.Unlock()
			var kinds []action.Kind
			for _, req := range exec.calls {
				if req.Kind == action.TermPeek || req.Kind == action.TermClose {
					kinds = append(kinds, req.Kind)
				}
			}
			if len(kinds) != 2 || kinds[0] != action.TermPeek || kinds[1] != action.TermClose {
				t.Fatalf("order: %v", kinds)
			}
		})
	}
}

// A terminal that does not close is tried again on the next poll, and the step
// it belonged to stays advanced: the agent is woken once.
func TestChainKeepsTheAdvanceWhenATerminalDoesNotClosePG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Login", Command: "login"}, personrequest.Step{Who: "agent", Text: "Verify"})
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	term := exec.captured(action.TermStart)[0].Target
	exec.setExit(true, 0, "signed in")
	exec.mu.Lock()
	exec.refuseClose = true
	exec.mu.Unlock()
	if err := s.pollChain(t.Context(), id); err == nil {
		t.Fatal("a terminal that did not close went unreported")
	}
	if row := chainRecord(t, s, id); *row.Step != 2 || row.Outputs[0].State != "done" || row.Outputs[0].Term != term {
		t.Fatalf("after the failed close: %+v %+v", row, row.Outputs[0])
	}
	exec.mu.Lock()
	exec.refuseClose = false
	exec.mu.Unlock()
	chainPoll(t, s, id)
	if row := chainRecord(t, s, id); row.Outputs[0].Term != "" || len(exec.captured(action.TermClose)) != 2 || len(exec.captured(action.SessionSend)) != 1 {
		t.Fatalf("the retry: %+v, sends %d", row.Outputs[0], len(exec.captured(action.SessionSend)))
	}
}

// Only a step that no longer runs gives its terminal up: a running command and
// a reserved terminal about to start keep theirs.
func TestChainKeepsTheTerminalOfAStepThatRunsOrIsAboutToStartPG(t *testing.T) {
	s, exec := newChainServer(t)
	exit := 0
	row := &act.Record{ID: strings.Repeat("a", 32), Outputs: []*personrequest.Output{
		{State: "done", Term: "t-1a1a1a1a", Exit: &exit},
		{State: "pending", Term: "t-2b2b2b2b"},
		{State: "running", Term: "t-3c3c3c3c"},
		{State: "pending"},
		nil,
	}}
	if err := s.closeEndedChainTerms(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	closes := exec.captured(action.TermClose)
	if len(closes) != 1 || closes[0].Target != "t-1a1a1a1a" {
		t.Fatalf("closes: %+v", closes)
	}
	if terms := []string{row.Outputs[0].Term, row.Outputs[1].Term, row.Outputs[2].Term}; terms[0] != "" || terms[1] != "t-2b2b2b2b" || terms[2] != "t-3c3c3c3c" {
		t.Fatalf("terms left: %q", terms)
	}
}

func chainCancel(t *testing.T, s *Server, target, id, note string) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"kind": "act.cancel", "target": target, "params": map[string]any{"act": id, "note": note}})
	return post(t, s, string(data))
}

// The person cancels a chain from its card on whatever step it stands: their
// own, the agent's, a command that runs, a step that failed. The chain closes
// as cancelled by the person, its terminals go, and the session is told once.
func TestChainCancelClosesItOnEveryStepAndTellsTheSessionPG(t *testing.T) {
	for _, c := range []struct {
		name  string
		steps []personrequest.Step
		reach func(*testing.T, *Server, *chainExecutor, string)
		check func(*testing.T, act.Record, *chainExecutor, string)
	}{
		{name: "the person's step", steps: []personrequest.Step{{Who: "you", Text: "Approve"}, {Who: "agent", Text: "Check"}},
			reach: func(*testing.T, *Server, *chainExecutor, string) {},
			check: func(t *testing.T, row act.Record, exec *chainExecutor, text string) {
				if row.Outputs[0] != nil || len(exec.captured(action.TermClose)) != 0 || strings.Contains(text, "1 you") {
					t.Fatalf("a step without output: %+v %q", row.Outputs, text)
				}
			}},
		{name: "the agent's step", steps: []personrequest.Step{{Who: "you", Text: "Approve"}, {Who: "agent", Text: "Check"}},
			reach: func(t *testing.T, s *Server, _ *chainExecutor, id string) {
				if w := chainPress(t, s, "session.send", id, 1, "done"); w.Code != http.StatusOK {
					t.Fatal(w.Body.String())
				}
			},
			check: func(t *testing.T, row act.Record, _ *chainExecutor, text string) {
				if *row.Step != 2 || row.Outputs[0].State != "done" || strings.Contains(text, "1 you") || strings.Contains(text, "2 agent") {
					t.Fatalf("the agent's step: %+v %q", row, text)
				}
			}},
		{name: "a running command", steps: []personrequest.Step{{Who: "you", Text: "Login", Command: "login"}, {Who: "you", Text: "Deploy", Command: "deploy"}},
			reach: func(t *testing.T, s *Server, exec *chainExecutor, id string) {
				// The first step's terminal does not close at its exit, so the
				// chain holds two terminals when it is cancelled.
				if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
					t.Fatal(w.Body.String())
				}
				exec.setExit(true, 0, "signed in")
				exec.mu.Lock()
				exec.refuseClose = true
				exec.mu.Unlock()
				if err := s.pollChain(t.Context(), id); err == nil {
					t.Fatal("a terminal that did not close went unreported")
				}
				exec.mu.Lock()
				exec.refuseClose = false
				exec.mu.Unlock()
				if w := chainPress(t, s, "act.run", id, 2, ""); w.Code != http.StatusOK {
					t.Fatal(w.Body.String())
				}
				exec.setExit(false, 0, "waiting for the rollout")
				exec.mu.Lock()
				exec.calls = nil
				exec.mu.Unlock()
			},
			check: func(t *testing.T, row act.Record, exec *chainExecutor, text string) {
				out := row.Outputs[1]
				if out.State != "stopped" || out.Ended == nil || out.Exit != nil || out.Term != "" || out.Tail != "waiting for the rollout" || out.Log == "" || row.Outputs[0].Term != "" {
					t.Fatalf("the stopped step: %+v %+v", row.Outputs[0], out)
				}
				exec.mu.Lock()
				var kinds []action.Kind
				var closed []string
				for _, req := range exec.calls {
					if req.Kind == action.TermPeek || req.Kind == action.TermClose {
						kinds = append(kinds, req.Kind)
					}
					if req.Kind == action.TermClose {
						closed = append(closed, req.Target)
					}
				}
				exec.mu.Unlock()
				// The capture is read before any pane goes, and both go.
				if len(kinds) != 3 || kinds[0] != action.TermPeek || len(closed) != 2 || closed[0] == closed[1] {
					t.Fatalf("terminal calls: %v closed %v", kinds, closed)
				}
				// Nothing woke the session since the chain began, so the
				// summary carries both steps, and the tail of the last one.
				if !strings.Contains(text, "1 you · Login · exit 0") || !strings.Contains(text, "2 you · Deploy · stopped") ||
					!strings.Contains(text, "waiting for the rollout") || strings.Contains(text, "signed in") {
					t.Fatalf("the summary of a stopped step: %q", text)
				}
			}},
		{name: "a failed step", steps: []personrequest.Step{{Who: "you", Text: "Login", Command: "login"}, {Who: "agent", Text: "Verify"}},
			reach: func(t *testing.T, s *Server, exec *chainExecutor, id string) {
				if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
					t.Fatal(w.Body.String())
				}
				exec.setExit(true, 3, "SSO denied")
				chainPoll(t, s, id)
				if row := chainRecord(t, s, id); row.State != "failed" {
					t.Fatalf("not failed: %+v", row)
				}
			},
			check: func(t *testing.T, row act.Record, exec *chainExecutor, text string) {
				if row.Outputs[0].State != "failed" || *row.Outputs[0].Exit != 3 || len(exec.captured(action.TermClose)) != 1 {
					t.Fatalf("the failed step: %+v", row.Outputs[0])
				}
				// The failure was told already; the cancel does not tell it again.
				if strings.Contains(text, "1 you") || strings.Contains(text, "SSO denied") {
					t.Fatalf("the failure told twice: %q", text)
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, exec := newChainServer(t)
			id := createChain(t, s, c.steps...)
			c.reach(t, s, exec, id)
			before := len(exec.captured(action.SessionSend))
			if w := chainCancel(t, s, "sentinel", id, "  No longer needed  "); w.Code != http.StatusOK {
				t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
			}
			row := chainRecord(t, s, id)
			if row.State != "withdrawn" || !row.Cancelled() || row.AnsweredBy != "person" || row.Closed == nil || row.Answer.Note != "No longer needed" || row.Notice != nil {
				t.Fatalf("cancelled: %+v %+v", row, row.Answer)
			}
			sends := exec.captured(action.SessionSend)
			if len(sends) != before+1 {
				t.Fatalf("the session was told %d times", len(sends)-before)
			}
			text := sends[len(sends)-1].Text
			if !strings.Contains(text, "The chain “Release” ("+id+")") || !strings.Contains(text, "The person cancelled the chain") ||
				!strings.HasSuffix(text, "\n\nTheir reason:\nNo longer needed") || strings.Contains(text, "is yours") {
				t.Fatalf("the summary: %q", text)
			}
			c.check(t, row, exec, text)
			// A closed chain is cancelled once.
			if w := chainCancel(t, s, "sentinel", id, ""); w.Code != http.StatusConflict {
				t.Fatalf("second cancel: %d %s", w.Code, w.Body.String())
			}
			chainPoll(t, s, id)
			if len(exec.captured(action.SessionSend)) != before+1 {
				t.Fatal("a cancelled chain told the session twice")
			}
		})
	}
}

// A summary the session has not taken yet is not overwritten by the cancel's:
// the cancel waits for it, and its own summary is retried as any other.
func TestChainCancelKeepsTheReasonOutOfTheJournalAndRetriesTheSummaryPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Approve"}, personrequest.Step{Who: "agent", Text: "Check"})
	refuse := func(on bool) {
		exec.mu.Lock()
		exec.refuse = on
		exec.mu.Unlock()
	}
	refuse(true)
	if w := chainPress(t, s, "session.send", id, 1, "done"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := chainCancel(t, s, "sentinel", id, ""); w.Code != http.StatusConflict || chainRecord(t, s, id).State != "open" {
		t.Fatalf("a cancel over an undelivered summary: %d %s", w.Code, w.Body.String())
	}
	refuse(false)
	chainPoll(t, s, id)
	refuse(true)
	if w := chainCancel(t, s, "sentinel", id, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	row := chainRecord(t, s, id)
	if !row.Cancelled() || row.Notice == nil || !strings.HasSuffix(row.Notice.Text, "report none of its steps.") {
		t.Fatalf("an undelivered summary: %+v", row.Notice)
	}
	refuse(false)
	chainPoll(t, s, id)
	sends := exec.captured(action.SessionSend)
	if len(sends) != 4 || !strings.Contains(sends[1].Text, "Step 2 is yours") || sends[2].MessageID != sends[3].MessageID ||
		!strings.Contains(sends[3].Text, "The person cancelled the chain") || chainRecord(t, s, id).Notice != nil {
		t.Fatalf("retry: %+v", sends)
	}
	pool, err := s.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	if err := pool.QueryRow(t.Context(), `SELECT params FROM actions WHERE kind='act.cancel' AND result='ok' AND params->>'act'=$1`, id).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 {
		t.Fatalf("journal: %+v", params)
	}
	second := createChain(t, s, personrequest.Step{Who: "you", Text: "Approve"})
	if w := chainCancel(t, s, "sentinel", second, "Secret words"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var raw string
	if err := pool.QueryRow(t.Context(), `SELECT params::text FROM actions WHERE kind='act.cancel' AND params->>'act'=$1`, second).Scan(&raw); err != nil || strings.Contains(raw, "Secret words") {
		t.Fatalf("the reason reached the journal: %s %v", raw, err)
	}
}

// Only an open chain of the session the press names is cancelled.
func TestChainCancelRefusesWhatItDoesNotOwnPG(t *testing.T) {
	s, exec := newChainServer(t)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Approve"})
	if w := chainCancel(t, s, "other", id, ""); w.Code == http.StatusOK {
		t.Fatal("another session cancelled the chain")
	}
	blocking := true
	foreign, err := s.db.CreateAct(t.Context(), "11111111-1111-4111-8111-111111111111", "elsewhere", act.Input{Kind: "do", Title: "Foreign", Blocking: &blocking, Body: act.Body{Chain: []personrequest.Step{{Who: "you", Text: "Approve"}}}})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := s.db.Pool()
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM acts WHERE id=$1`, foreign.ID) })
	if w := chainCancel(t, s, "sentinel", foreign.ID, ""); w.Code == http.StatusOK {
		t.Fatal("a chain of another conversation was cancelled")
	}
	legacy := newLegacyRequest(t, s)
	if w := chainCancel(t, s, "sentinel", legacy, ""); w.Code != http.StatusConflict {
		t.Fatalf("a plain request was cancelled: %d", w.Code)
	}
	if w := chainCancel(t, s, "sentinel", id, strings.Repeat("x", personrequest.NoteMax+1)); w.Code != http.StatusBadRequest {
		t.Fatalf("a long reason: %d", w.Code)
	}
	if row := chainRecord(t, s, id); row.State != "open" || len(exec.captured(action.SessionSend)) != 0 {
		t.Fatalf("a refused cancel changed the chain: %+v", row)
	}
	if w := chainAPI(t, s, map[string]any{"conversation": requestConversation, "id": id, "withdraw": true}); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if row := chainRecord(t, s, id); row.Cancelled() {
		t.Fatal("the session's withdrawal reads as the person's cancel")
	}
	if w := chainCancel(t, s, "sentinel", id, ""); w.Code != http.StatusConflict {
		t.Fatalf("a withdrawn chain was cancelled: %d", w.Code)
	}
}

type capableExecutor []action.Kind

func (capableExecutor) Execute(context.Context, action.Request) (string, error) {
	return "accepted", nil
}
func (e capableExecutor) Kinds() []action.Kind { return e }

// Cancel is offered only by an executor that can read and close a chain's
// terminals and deliver the summary.
func TestExecStatusOffersCancelWithWhatItNeeds(t *testing.T) {
	for _, c := range []struct {
		kinds []action.Kind
		want  bool
	}{
		{[]action.Kind{action.TermPeek, action.TermClose, action.SessionSend}, true},
		{[]action.Kind{action.TermClose, action.SessionSend}, false},
		{[]action.Kind{action.TermPeek, action.SessionSend}, false},
		{[]action.Kind{action.TermPeek, action.TermClose}, false},
	} {
		path := socketPath(t, "status.sock")
		server := action.NewServer(path, capableExecutor(c.kinds), time.Second)
		ln, err := server.Listen()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); server.Run(ctx, ln) }()
		srv := &Server{exec: action.NewClient(path, time.Second)}
		w := httptest.NewRecorder()
		srv.apiExecStatus(w, httptest.NewRequest(http.MethodGet, "/api/exec", nil))
		cancel()
		ln.Close()
		<-done
		var status struct {
			Kinds []action.Kind `json:"kinds"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(status.Kinds, action.ActCancel); got != c.want {
			t.Errorf("%v: act.cancel offered %v, want %v", c.kinds, got, c.want)
		}
	}
}

// A chain on the agent's step, or on a failed step the session reviews, waits
// on no person: the summary marks it, and keeps the person's steps unmarked.
func TestWaitingMarksAChainOnTheAgentsTurn(t *testing.T) {
	chain := []personrequest.Step{{Who: "you", Text: "Merge"}, {Who: "agent", Text: "Wait for the release"}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func(n int) *int { return &n }
	rows := []act.Record{
		{ID: "yours", Kind: "do", State: "open", Step: step(1), Body: act.Body{Chain: chain}, Created: at},
		{ID: "agents", Kind: "do", State: "open", Step: step(2), Body: act.Body{Chain: chain}, Created: at.Add(time.Second)},
		{ID: "failed", Kind: "do", State: "failed", Step: step(1), Body: act.Body{Chain: chain}, Created: at.Add(2 * time.Second)},
		{ID: "plain", Kind: "do", State: "open", Created: at.Add(3 * time.Second)},
	}
	want := map[string]bool{"yours": false, "agents": true, "failed": true, "plain": false}
	got := waitingFor(rows, nil, nil, nil)
	if len(got) != len(want) {
		t.Fatalf("waiting summary: %+v", got)
	}
	for _, item := range got {
		if item.Agent != want[item.ID] {
			t.Errorf("%s marked agent=%v, want %v", item.ID, item.Agent, want[item.ID])
		}
	}
}

func TestWaitingMarksOnlyALaunchedHumanCommandRunning(t *testing.T) {
	step := 1
	row := act.Record{ID: "command", Kind: "do", State: "open", Step: &step,
		Body: act.Body{Chain: []personrequest.Step{{Who: "you", Text: "Login", Command: "login"}}}, Outputs: make([]*personrequest.Output, 1)}
	for _, c := range []struct {
		name   string
		state  string
		output *personrequest.Output
		want   bool
	}{
		{"before Run", "open", nil, false},
		{"pending", "open", &personrequest.Output{State: "pending"}, false},
		{"reserved", "open", &personrequest.Output{State: "pending", Term: "t-login"}, true},
		{"running", "open", &personrequest.Output{State: "running", Term: "t-login"}, true},
		{"exited", "open", &personrequest.Output{State: "done", Term: "t-login"}, false},
		{"stopped", "open", &personrequest.Output{State: "stopped", Term: "t-login"}, false},
		{"failed", "failed", &personrequest.Output{State: "failed", Term: "t-login"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			row.State, row.Outputs[0] = c.state, c.output
			got := waitingFor([]act.Record{row}, nil, nil, nil)
			if len(got) != 1 || got[0].Running != c.want || got[0].Agent != (c.state == "failed") {
				t.Fatalf("waiting summary: %+v, want running=%v and agent=%v", got, c.want, c.state == "failed")
			}
			wire, err := json.Marshal(got[0])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), `"running":true`) != c.want {
				t.Fatalf("running mark on the wire: %s", wire)
			}
		})
	}
}

func TestWaitingMarksACommandBeforeItsOpening(t *testing.T) {
	now := time.Now().UTC()
	from, until := now.Add(time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339)
	step := 1
	row := act.Record{ID: "scheduled", Kind: "do", State: "open", Step: &step,
		Body: act.Body{Chain: []personrequest.Step{{Who: "you", Text: "Deploy", Command: "deploy", From: from, Until: until}}}, Outputs: make([]*personrequest.Output, 1)}
	got := waitingFor([]act.Record{row}, nil, nil, nil)
	if len(got) != 1 || !got[0].Scheduled || got[0].From != from || got[0].Until != until || got[0].Agent || got[0].Running {
		t.Fatalf("scheduled waiting summary: %+v", got)
	}
	wire, err := json.Marshal(got[0])
	if err != nil || !strings.Contains(string(wire), `"scheduled":true`) || !strings.Contains(string(wire), `"from":"`+from+`"`) {
		t.Fatalf("window on the wire: %s %v", wire, err)
	}
	row.Body.Chain[0].From = now.Add(-time.Hour).Format(time.RFC3339)
	if got := waitingFor([]act.Record{row}, nil, nil, nil); got[0].Scheduled {
		t.Fatal("the opening did not hand the step to the person")
	}
	row.Body.Chain[0].From = from
	row.Outputs[0] = &personrequest.Output{State: "running"}
	if got := waitingFor([]act.Record{row}, nil, nil, nil); got[0].Scheduled || !got[0].Running {
		t.Fatal("a running step is scheduled")
	}
}

func setChainWindow(t *testing.T, s *Server, id, from, until string) {
	t.Helper()
	row := chainRecord(t, s, id)
	row.Body.Chain[*row.Step-1].From, row.Body.Chain[*row.Step-1].Until = from, until
	pool, err := s.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE acts SET body=$2 WHERE id=$1`, id, row.Body); err != nil {
		t.Fatal(err)
	}
}

func TestChainRunIsAdmittedOnlyInsideItsWindowPG(t *testing.T) {
	s, exec := newChainServer(t)
	now := time.Now().UTC()
	future, end := now.Add(time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339)
	past, closed := now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Deploy", Command: "deploy", From: future, Until: end})
	if row := chainRecord(t, s, id); row.Body.Chain[0].From != future || !row.CommandScheduled(now) {
		t.Fatal("the stored window was lost")
	}
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "opens at "+personrequest.WindowTime(future)) {
		t.Fatalf("before the opening: %d %s", w.Code, w.Body.String())
	}
	setChainWindow(t, s, id, past, closed)
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "closed at "+personrequest.WindowTime(closed)) {
		t.Fatalf("after the closing: %d %s", w.Code, w.Body.String())
	}
	if len(exec.captured(action.TermStart)) != 0 || chainRecord(t, s, id).Outputs[0] != nil {
		t.Fatal("an out-of-window Run reserved or started a terminal")
	}
	setChainWindow(t, s, id, past, end)
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	setChainWindow(t, s, id, past, closed)
	// A repeated Run is idempotent even when its admitted command has outlived the window.
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	chainPoll(t, s, id)
	if row := chainRecord(t, s, id); !row.CommandRunning() || len(exec.captured(action.TermClose)) != 0 {
		t.Fatal("closing the window stopped its command")
	}
	exec.setExit(true, 0, "deployed")
	chainPoll(t, s, id)
	if row := chainRecord(t, s, id); row.State != "answered" || row.Outputs[0].Tail != "deployed" || len(exec.captured(action.TermStart)) != 1 {
		t.Fatalf("completion: %+v", row)
	}
	for _, from := range []string{future, ""} {
		id := createChain(t, s, personrequest.Step{Who: "you", Text: "Deploy", Command: "deploy", From: from, Until: end})
		setChainWindow(t, s, id, from, closed)
		if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusConflict {
			t.Fatalf("closing-only window admitted Run: %d", w.Code)
		}
		if w := chainPress(t, s, "session.send", id, 1, "cant"); w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
	}
}

func chainPushQueue(t *testing.T, s *Server) *pushStore {
	t.Helper()
	queue := &pushStore{taken: make(chan time.Time, 16)}
	s.push = notify.New(queue, "")
	return queue
}

func drainChainPushes(t *testing.T, s *Server, queue *pushStore, count int) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.push.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for range count {
		taken(t, queue, time.Second, "the command window")
	}
	nothingTaken(t, queue, 30*time.Millisecond, "more than once for a window boundary")
}

func TestChainWindowPushesOnceAcrossDatabaseReadsAndServiceRestartPG(t *testing.T) {
	s, _ := newChainServer(t)
	queue := chainPushQueue(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	from, until := now.Add(10*time.Minute), now.Add(20*time.Minute)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Deploy", Command: "deploy", From: from.Format(time.RFC3339), Until: until.Format(time.RFC3339)})
	active := func(want bool) {
		t.Helper()
		rows, err := s.db.ActiveChains(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		found := slices.ContainsFunc(rows, func(row act.Record) bool { return row.ID == id })
		if found != want {
			t.Fatalf("the window is polled=%v, want %v", found, want)
		}
	}
	active(true)
	push := func(server *Server, at time.Time) {
		t.Helper()
		if err := server.pushChainWindow(t.Context(), id, at); err != nil {
			t.Fatal(err)
		}
	}
	push(s, from.Add(-5*time.Minute-time.Nanosecond))
	if chainRecord(t, s, id).Outputs[0] != nil {
		t.Fatal("a reminder was spent before it was due")
	}
	push(s, from.Add(-5*time.Minute))
	if out := chainRecord(t, s, id).Outputs[0]; out == nil || !out.WindowSoon || out.WindowOpened {
		t.Fatalf("advance push marks: %+v", out)
	}
	restarted := &Server{db: s.db, exec: s.exec, host: s.host, home: s.home, push: s.push}
	push(restarted, from.Add(-time.Minute))
	push(restarted, from)
	push(restarted, from.Add(time.Second))
	if out := chainRecord(t, s, id).Outputs[0]; !out.WindowSoon || !out.WindowOpened {
		t.Fatalf("opening push marks: %+v", out)
	}
	active(false)
	// A new Run and a later Stop keep the marks, so neither boundary repeats.
	setChainWindow(t, s, id, now.Add(-time.Minute).Format(time.RFC3339), until.Format(time.RFC3339))
	if w := chainPress(t, s, "act.run", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := chainPress(t, s, "act.stop", id, 1, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	push(restarted, from.Add(time.Minute))
	drainChainPushes(t, s, queue, 2)
}

func TestChainWindowSkipsLateRemindersAndSuppressesOrdinaryStepPushesPG(t *testing.T) {
	s, _ := newChainServer(t)
	queue := chainPushQueue(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	for _, from := range []time.Time{now.Add(4 * time.Minute), now.Add(-time.Minute)} {
		id := createChain(t, s, personrequest.Step{Who: "you", Text: "Deploy", Command: "deploy", From: from.Format(time.RFC3339)})
		if from.Before(now) {
			for range 2 {
				if err := s.pollChains(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if out := chainRecord(t, s, id).Outputs[0]; out == nil || !out.WindowOpened {
				t.Fatal("the poll did not announce an already open window")
			}
			continue
		}
		for _, at := range []time.Time{from.Add(-time.Second), from, from.Add(time.Minute)} {
			if err := s.pushChainWindow(t.Context(), id, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	from := now.Add(time.Hour)
	id := createChain(t, s, personrequest.Step{Who: "you", Text: "Approve"}, personrequest.Step{Who: "you", Text: "Deploy", Command: "deploy", From: from.Format(time.RFC3339), Until: from.Add(time.Hour).Format(time.RFC3339)})
	if w := chainPress(t, s, "session.send", id, 1, "done"); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if err := s.pushChainWindow(t.Context(), id, from.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if out := chainRecord(t, s, id).Outputs[1]; out == nil || !out.WindowSoon || !out.WindowOpened {
		t.Fatalf("expired window marks: %+v", out)
	}
	// Two late windows push at opening only; the manual first step pushes as usual.
	drainChainPushes(t, s, queue, 3)
}
