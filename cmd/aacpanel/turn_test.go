package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/notify"
)

type turnVerdict struct {
	Question string `json:"question"`
	Action   string `json:"action"`
	Block    bool   `json:"block"`
}

func turnRequest(t *testing.T, s *Server, message, text string) turnVerdict {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"conversation": requestConversation, "session": "sentinel", "message": message, "text": text})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.apiTurn(w, httptest.NewRequest(http.MethodPost, "/api/turn", strings.NewReader(string(raw))))
	var got turnVerdict
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("turn: %d %s", w.Code, w.Body.String())
	}
	return got
}

func TestTurnClaimsOneVerdictAndRequiresStructuredQuestionsOrManualWorkPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "turn-verdict-phone")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
	pushes := &pushStore{taken: make(chan time.Time, 16)}
	p.srv.push = notify.New(pushes, "")
	pushCtx, stopPush := context.WithCancel(t.Context())
	pushDone := make(chan struct{})
	go func() { defer close(pushDone); p.srv.push.Run(pushCtx) }()
	t.Cleanup(func() { stopPush(); <-pushDone })
	q := &questionExecutor{}
	path := socketPath(t, "turn.sock")
	executor := action.NewServer(path, q, time.Second)
	ln, err := executor.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); executor.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	p.srv.exec = action.NewClient(path, time.Second)
	for i, tc := range []struct {
		name, action                                  string
		questions, existing, failed, block, noSummary bool
	}{
		{name: "report"}, {name: "question", questions: true, block: true},
		{name: "question without a summary", questions: true, noSummary: true, block: true},
		{name: "manual action", action: "Merge the change", block: true},
		{name: "question and action", questions: true, action: "Check the screen", block: true},
		{name: "question with recorded work", questions: true, existing: true, block: true},
		{name: "already recorded", action: "Merge the change", existing: true},
		{name: "failed classifier", action: "Merge the change", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q.yes, q.failed, q.action, q.noSummary = tc.questions, tc.failed, tc.action, tc.noSummary
			id := fmt.Sprintf("12345678-1234-1234-1234-%012d", i)
			if tc.existing {
				recorded := newPersonRequest(t, p.srv)
				taken(t, pushes, time.Second, "the existing request")
				t.Cleanup(func() { pool.Exec(t.Context(), "DELETE FROM acts WHERE id=$1", recorded) })
			}
			before := q.calls.Load()
			for range 2 {
				got := turnRequest(t, p.srv, id, "Publish? The checks passed.")
				wantAction := tc.action
				if tc.failed {
					wantAction = ""
				}
				wantQuestion := ""
				if tc.questions && !tc.failed {
					wantQuestion = "Publish?"
				}
				if got.Action != wantAction || got.Block != tc.block || got.Question != wantQuestion {
					t.Fatalf("verdict: %+v; want question %q action %q block %v", got, wantQuestion, wantAction, tc.block)
				}
			}
			if q.calls.Load()-before != 1 {
				t.Fatalf("repeated message made %d classifier calls", q.calls.Load()-before)
			}
			rows, err := p.db.ActsOf(t.Context(), requestConversation)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Kind == "ask" {
					t.Fatalf("a question verdict created an act: %+v", row)
				}
			}
			nothingTaken(t, pushes, 20*time.Millisecond, "a prose verdict pushed to the person")

		})
	}
	before := q.calls.Load()
	if got := turnRequest(t, p.srv, "12345678-1234-1234-1234-999999999999", "The build passed."); got.Block || q.calls.Load() != before {
		t.Fatal("a report reached the classifier")
	}
	for i, tc := range []struct {
		name     string
		disabled bool
		row      string
	}{
		{name: "disabled checker", disabled: true},
		{name: "codex", row: `,"agent":"codex"`},
		{name: "outside panel", row: `,"outside":true`},
		{name: "other session", row: `,"session":"other"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			on := !tc.disabled
			if _, err := p.srv.savePushPrefs(t.Context(), notify.Prefs{CheckQuestions: &on}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM push_prefs") })
			p.srv.host = hostWith(t, `{"sessions":[{"sessionId":"`+requestConversation+`","session":"sentinel"`+tc.row+`}]}`)
			before := q.calls.Load()
			got := turnRequest(t, p.srv, fmt.Sprintf("12345678-1234-1234-1234-%012d", i+100), "Publish? The checks passed.")
			if got.Block || got.Question != "" || q.calls.Load() != before {
				t.Fatalf("out-of-scope turn: %+v, calls %d", got, q.calls.Load()-before)
			}
		})
	}

}

func TestTurnEndpointBelongsOnlyToTheLocalListener(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.routes(s.publicGate()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/turn", strings.NewReader(`{}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("public turn endpoint: %d", w.Code)
	}
	w = httptest.NewRecorder()
	s.routes(s.localGate()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/turn", strings.NewReader(`{}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("local turn endpoint: %d", w.Code)
	}
}

func TestTurnRepeatWaitsForTheClaimedVerdictPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "turn-repeat-phone")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
	release := make(chan struct{})
	q := &questionExecutor{action: "Merge the change", started: make(chan struct{}, 1), release: release}
	path := socketPath(t, "turn-repeat.sock")
	executor := action.NewServer(path, q, time.Second)
	ln, err := executor.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); executor.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	p.srv.exec = action.NewClient(path, time.Second)
	id := "12345678-1234-1234-1234-444444444444"
	type verdict struct {
		action string
		block  bool
	}
	request := func(out chan<- verdict) {
		got := turnRequest(t, p.srv, id, "Publish? The checks passed.")
		out <- verdict{got.Action, got.Block}
	}
	first, second := make(chan verdict, 1), make(chan verdict, 1)
	go request(first)
	select {
	case <-q.started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("the classifier never took the first turn")
	}
	go request(second)
	select {
	case v := <-second:
		close(release)
		t.Fatalf("a pending verdict was returned as %+v", v)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for _, out := range []<-chan verdict{first, second} {
		select {
		case v := <-out:
			if v.action != "Merge the change" || !v.block {
				t.Fatalf("saved verdict: %+v", v)
			}
		case <-time.After(time.Second):
			t.Fatal("the saved verdict never reached the caller")
		}
	}
	if q.calls.Load() != 1 {
		t.Fatalf("concurrent stops called the classifier %d times", q.calls.Load())
	}
}
