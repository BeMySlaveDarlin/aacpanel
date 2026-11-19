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

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/question"
)

func turnRequest(t *testing.T, s *Server, message, text string) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"conversation": requestConversation, "session": "sentinel", "message": message, "text": text})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.apiTurn(w, httptest.NewRequest(http.MethodPost, "/api/turn", strings.NewReader(string(raw))))
	var got struct {
		Action string `json:"action"`
		Block  bool   `json:"block"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("turn: %d %s", w.Code, w.Body.String())
	}
	return got.Action, got.Block
}

func TestTurnClaimsOneVerdictAndBlocksOnlyUnrecordedManualWorkPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "turn-verdict-phone")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
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
		name, action                       string
		questions, existing, failed, block bool
	}{
		{name: "report"}, {name: "question", questions: true},
		{name: "manual action", action: "Merge the change", block: true},
		{name: "question and action", questions: true, action: "Check the screen", block: true},
		{name: "already recorded", action: "Merge the change", existing: true},
		{name: "failed classifier", action: "Merge the change", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q.yes, q.failed, q.action = tc.questions, tc.failed, tc.action
			id := fmt.Sprintf("12345678-1234-1234-1234-%012d", i)
			if tc.existing {
				newPersonRequest(t, p.srv)
			}
			before := q.calls.Load()
			for range 2 {
				action, block := turnRequest(t, p.srv, id, "Publish? The checks passed.")
				wantAction := tc.action
				if tc.failed {
					wantAction = ""
				}
				if action != wantAction || block != tc.block {
					t.Fatalf("action %q block %v; want %q %v", action, block, wantAction, tc.block)
				}
			}
			if q.calls.Load()-before != 1 {
				t.Fatalf("repeated message made %d classifier calls", q.calls.Load()-before)
			}
			rows, err := p.db.ActsOf(t.Context(), requestConversation)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, row := range rows {
				if row.Kind == "ask" && row.Body.Message == id {
					count++
					if row.Body.Source != "prose" || row.Body.Action != tc.action || len(row.Body.Questions) != 1 {
						t.Fatalf("ask: %+v", row)
					}
				}
			}
			if (count == 1) != (tc.questions && !tc.failed) {
				t.Fatalf("question act count: %d", count)
			}
		})
	}
	before := q.calls.Load()
	if _, block := turnRequest(t, p.srv, "12345678-1234-1234-1234-999999999999", "The build passed."); block || q.calls.Load() != before {
		t.Fatal("a report reached the classifier")
	}
}

func TestHookQuestionSurvivesTheFallbackAndHumanInputSettlesOnlyProsePG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "hook-question-lifecycle")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
	message := "12345678-1234-1234-1234-333333333333"
	if fresh, err := p.db.ClaimTurnQuestion(t.Context(), requestConversation, message); err != nil || !fresh {
		t.Fatalf("claim: %v %v", fresh, err)
	}
	if err := p.db.SetQuestionVerdict(t.Context(), requestConversation, "sentinel", message, "yes", question.Verdict{What: "Publish?", Questions: publishQuestions()}); err != nil {
		t.Fatal(err)
	}
	blocking := false
	tool, err := p.db.CreateAct(t.Context(), requestConversation, "sentinel", act.Input{Kind: "ask", Title: "Name?", Blocking: &blocking, Body: act.Body{Source: "tool", Questions: []question.Question{{Question: "Name?"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.db.CloseQuestions(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	rows, err := p.db.OpenActs(t.Context(), []string{requestConversation})
	if err != nil || len(rows) != 2 {
		t.Fatalf("fallback retired an ask: %+v %v", rows, err)
	}
	p.srv.exec, _ = startFakeExec(t, action.Response{OK: true})
	w := post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"text":"I will answer later"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("human reply: %d %s", w.Code, w.Body.String())
	}
	rows, err = p.db.OpenActs(t.Context(), []string{requestConversation})
	if err != nil || len(rows) != 1 || rows[0].ID != tool.ID {
		t.Fatalf("human reply lifecycle: %+v %v", rows, err)
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
		a, b := turnRequest(t, p.srv, id, "Publish? The checks passed.")
		out <- verdict{a, b}
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
