package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	legacy := newPersonRequest(t, s)
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
