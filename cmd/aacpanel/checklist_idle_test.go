package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/checklist"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func TestChecklistIdleWork(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, work, alarm, pause string
		processes                int
		want                     bool
	}{
		{name: "no background work", want: true},
		{name: "native agents", work: `{"agents":1}`},
		{name: "background command", work: `{"tasks":1}`},
		{name: "workflow", work: `{"workflows":1}`},
		{name: "observer", work: `{"observers":1}`},
		{name: "codex terminal", processes: 1},
		{name: "native wake alone", work: `{"tasks":1,"wakes":1}`, want: true},
		{name: "overdue alarm", alarm: "2026-01-01T11:59:00Z"},
		{name: "near alarm", alarm: "2026-01-01T12:29:59Z"},
		{name: "alarm at the boundary", alarm: "2026-01-01T12:30:00Z"},
		{name: "later alarm", alarm: "2026-01-01T12:30:01Z", want: true},
		{name: "pause alarm", pause: `{"name":"pause"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := idleChecklistRow{Work: json.RawMessage(tc.work), Pause: json.RawMessage(tc.pause), Processes: tc.processes}
			if tc.alarm != "" {
				row.NextAlarm = &struct {
					Next string `json:"next"`
				}{Next: tc.alarm}
			}
			if got := checklistWaiting(row, now); got != tc.want {
				t.Fatalf("waiting %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChecklistIdleChildren(t *testing.T) {
	children := []action.ChildSession{{Key: "process-42-71", Conversation: "child"}, {Key: "codex-thread", Conversation: "codex-thread"}}
	for _, tc := range []struct {
		name string
		row  idleChecklistRow
		want bool
	}{
		{name: "working claude", row: idleChecklistRow{OpenedKey: "process-42-71", Status: "busy"}, want: true},
		{name: "working codex", row: idleChecklistRow{LiveSession: host.LiveSession{SessionID: "codex-thread", Agent: "codex"}, Status: "busy"}, want: true},
		{name: "idle child", row: idleChecklistRow{OpenedKey: "process-42-71", Status: "idle"}},
		{name: "turn ended", row: idleChecklistRow{OpenedKey: "process-42-71", Status: "busy", TurnOver: true}},
		{name: "child background work", row: idleChecklistRow{OpenedKey: "process-42-71", Status: "idle", Work: json.RawMessage(`{"agents":1}`)}, want: true},
		{name: "child codex terminal", row: idleChecklistRow{OpenedKey: "process-42-71", Processes: 1}, want: true},
		{name: "legacy conversation match", row: idleChecklistRow{LiveSession: host.LiveSession{SessionID: "child"}, Status: "busy"}, want: true},
		{name: "reused name", row: idleChecklistRow{LiveSession: host.LiveSession{Name: "child", SessionID: "replacement"}, OpenedKey: "process-42-72", Status: "busy"}},
		{name: "visual child only", row: idleChecklistRow{LiveSession: host.LiveSession{SessionID: "other", Parent: &host.SessionRef{Session: "parent"}}, Status: "busy"}},
		{name: "closed child"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checklistChildWorking(children, []idleChecklistRow{tc.row}); got != tc.want {
				t.Fatalf("working child %v, want %v", got, tc.want)
			}
		})
	}
}

type idleChecklistExecutor struct {
	calls    atomic.Int32
	verdict  action.ChecklistVerdict
	failed   bool
	children []action.ChildSession
	started  chan struct{}
	release  <-chan struct{}
	steps    []checklist.Item
}

func (*idleChecklistExecutor) Execute(context.Context, action.Request) (string, error) {
	return "", errors.New("no actions")
}
func (e *idleChecklistExecutor) Children(context.Context, string, string) ([]action.ChildSession, error) {
	return e.children, nil
}
func (e *idleChecklistExecutor) ChecklistCurrent(ctx context.Context, target, conversation string, input action.ChecklistInput) (action.ChecklistVerdict, error) {
	e.calls.Add(1)
	if target != "sentinel" || conversation != requestConversation || input.Log != "CALL Edit: main.go" || !reflect.DeepEqual(input.Checklist.Items, e.steps) {
		return action.ChecklistVerdict{}, errors.New("wrong review input")
	}
	if e.started != nil {
		e.started <- struct{}{}
	}
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return action.ChecklistVerdict{}, ctx.Err()
		}
	}
	if e.failed {
		return action.ChecklistVerdict{}, errors.New("classifier refused")
	}
	return e.verdict, nil
}

func idleChecklistProbe(t *testing.T) (*Server, *idleChecklistExecutor) {
	t.Helper()
	p := newPollProbe(t, t.Context(), "idle-checklist")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM session_restarts WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM session_pauses WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1", requestConversation)
	})
	e := &idleChecklistExecutor{steps: []checklist.Item{{Text: "Fix the hook", Status: checklist.Active}}}
	path := socketPath(t, "idle.sock")
	server := action.NewServer(path, e, time.Second)
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	p.srv.exec = action.NewClient(path, time.Second)
	return p.srv, e
}

func checklistSnapshot(t *testing.T, s *Server, agent, extra string) {
	t.Helper()
	s.host = hostWith(t, fmt.Sprintf(`{"sessions":[{"session":"sentinel","sessionId":%q,"agent":%q,"checklist":{"items":[{"text":"Fix the hook","status":"active"}]}%s}]}`, requestConversation, agent, extra))
}

func idleChecklistCall(t *testing.T, s *Server, message string, codex bool, steps ...checklist.Item) bool {
	t.Helper()
	body := map[string]any{"conversation": requestConversation, "session": "sentinel", "message": message, "codex": codex}
	if !codex {
		body["span"] = "CALL Edit: main.go"
		if steps == nil {
			steps = []checklist.Item{{Text: "Fix the hook", Status: checklist.Active}}
		}
		body["checklist"] = steps
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.apiChecklistIdle(w, httptest.NewRequest(http.MethodPost, "/api/checklist/idle", strings.NewReader(string(raw))))
	var reply struct {
		Block *bool `json:"block"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Block == nil {
		t.Fatalf("idle review: %d %s", w.Code, w.Body.String())
	}
	return *reply.Block
}

func TestChecklistIdleUsesRequestPG(t *testing.T) {
	s, e := idleChecklistProbe(t)
	checklistSnapshot(t, s, "", "")
	e.steps = []checklist.Item{{Text: "Fix the hook", Status: checklist.Done}, {Text: "Verify the change", Status: checklist.Active}}
	e.verdict = action.ChecklistVerdict{Current: true, Confidence: 0.9}
	if idleChecklistCall(t, s, "12345678-1234-1234-1234-000000000001", false, e.steps...) || e.calls.Load() != 1 {
		t.Fatal("the classifier did not receive the request's checklist instead of the host snapshot's")
	}
}

func TestChecklistIdleNeedsSteps(t *testing.T) {
	s := &Server{}
	for _, steps := range []string{"", `,"checklist":null`, `,"checklist":[]`} {
		body := fmt.Sprintf(`{"conversation":%q,"session":"sentinel","message":"12345678-1234-1234-1234-000000000001","span":"CALL Edit: main.go"%s}`, requestConversation, steps)
		w := httptest.NewRecorder()
		s.apiChecklistIdle(w, httptest.NewRequest(http.MethodPost, "/api/checklist/idle", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing checklist steps: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestChecklistIdleVerdictPG(t *testing.T) {
	s, e := idleChecklistProbe(t)
	checklistSnapshot(t, s, "", "")
	for i, tc := range []struct {
		name          string
		current       bool
		confidence    float64
		failed, block bool
	}{
		{name: "current at the threshold", current: true, confidence: 0.7},
		{name: "current above the threshold", current: true, confidence: 0.9},
		{name: "uncertain", current: true, confidence: 0.699, block: true},
		{name: "stale", current: false, confidence: 1, block: true},
		{name: "error", failed: true, block: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.verdict, e.failed = action.ChecklistVerdict{Current: tc.current, Confidence: tc.confidence}, tc.failed
			before := e.calls.Load()
			id := fmt.Sprintf("12345678-1234-1234-1234-%012d", i)
			for range 2 {
				if got := idleChecklistCall(t, s, id, false); got != tc.block {
					t.Fatalf("block %v, want %v", got, tc.block)
				}
			}
			if e.calls.Load()-before != 1 {
				t.Fatalf("repeated review called haiku %d times", e.calls.Load()-before)
			}
		})
	}
}

func TestChecklistIdleStatePG(t *testing.T) {
	s, e := idleChecklistProbe(t)
	for i, tc := range []struct {
		name, extra         string
		child, codex, block bool
	}{
		{name: "own observer", extra: `,"work":{"observers":1}`},
		{name: "near alarm", extra: `,"nextAlarm":{"next":"2000-01-01T00:00:00Z"}`},
		{name: "paused", extra: `,"pauseAlarm":{"name":"pause"}`},
		{name: "working child", child: true, extra: `},{"session":"child","sessionId":"child","openedKey":"process-42-71","status":"busy"`},
		{name: "idle codex", codex: true, block: true},
		{name: "busy codex", codex: true, extra: `,"processes":1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := ""
			if tc.codex {
				agent = "codex"
			}
			checklistSnapshot(t, s, agent, tc.extra)
			e.children = nil
			if tc.child {
				e.children = []action.ChildSession{{Key: "process-42-71", Conversation: "child"}}
			}
			if got := idleChecklistCall(t, s, fmt.Sprintf("12345678-1234-1234-1234-%012d", i), tc.codex); got != tc.block {
				t.Fatalf("block %v, want %v", got, tc.block)
			}
		})
	}
	if e.calls.Load() != 0 {
		t.Fatalf("busy or codex sessions spent %d haiku calls", e.calls.Load())
	}
}

func TestChecklistIdleDatabasePG(t *testing.T) {
	s, e := idleChecklistProbe(t)
	checklistSnapshot(t, s, "", "")
	hostID, err := s.db.HostID(t.Context(), s.hostName)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.PutSessionPause(t.Context(), hostID, store.SessionPause{LivingSession: store.LivingSession{Conversation: requestConversation, Session: "sentinel"}, Snapshot: json.RawMessage(`{}`), CommandAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if idleChecklistCall(t, s, "12345678-1234-1234-1234-000000000001", false) {
		t.Fatal("a panel pause held the stop")
	}
	if err := s.db.DropSessionPause(t.Context(), hostID, requestConversation); err != nil {
		t.Fatal(err)
	}
	for i, manual := range []bool{false, true} {
		if err := s.db.KeepRestartCycle(t.Context(), store.RestartCycle{Conversation: requestConversation, Session: "sentinel", CommandAt: time.Now(), Manual: manual}); err != nil {
			t.Fatal(err)
		}
		if idleChecklistCall(t, s, fmt.Sprintf("12345678-1234-1234-1234-%012d", i+2), false) {
			t.Fatal("a restart preparation held the stop")
		}
	}
	if err := s.db.KeepRestartCycle(t.Context(), store.RestartCycle{Conversation: requestConversation, Session: "sentinel", CommandAt: time.Now(), Done: true}); err != nil {
		t.Fatal(err)
	}
	newPersonRequest(t, s)
	e.verdict = action.ChecklistVerdict{Current: false, Confidence: 1}
	if !idleChecklistCall(t, s, "12345678-1234-1234-1234-000000000004", false) || e.calls.Load() != 1 {
		t.Fatal("an open request suppressed the idle review")
	}
}

func TestChecklistIdleRoute(t *testing.T) {
	s := &Server{}
	for _, tc := range []struct {
		gate gate
		want int
	}{{s.publicGate(), http.StatusNotFound}, {s.localGate(), http.StatusBadRequest}} {
		w := httptest.NewRecorder()
		s.routes(tc.gate).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/checklist/idle", strings.NewReader(`{}`)))
		if w.Code != tc.want {
			t.Fatalf("idle endpoint: %d, want %d", w.Code, tc.want)
		}
	}
}

func TestChecklistIdleCache(t *testing.T) {
	var cache idleChecklistCache
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	first, second := make(chan bool, 1), make(chan bool, 1)
	review := func() bool { calls.Add(1); close(started); <-release; return false }
	go func() { first <- cache.check(t.Context(), "conversation/message", review) }()
	<-started
	go func() {
		second <- cache.check(t.Context(), "conversation/message", func() bool { calls.Add(1); return true })
	}()
	close(release)
	if <-first || <-second || calls.Load() != 1 {
		t.Fatalf("concurrent review spent %d calls", calls.Load())
	}
	cache.mu.Lock()
	cache.results["conversation/message"].at = time.Now().Add(-idleChecklistKeep)
	cache.mu.Unlock()
	if !cache.check(t.Context(), "conversation/message", func() bool { calls.Add(1); return true }) || calls.Load() != 2 {
		t.Fatal("the expired verdict was reused")
	}
}
