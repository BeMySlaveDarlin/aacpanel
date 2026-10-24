package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
)

type questionExecutor struct {
	calls  atomic.Int32
	yes    bool
	failed bool
}

func (q *questionExecutor) Execute(context.Context, action.Request) (string, error) {
	return "", errors.New("no actions")
}
func (q *questionExecutor) Question(_ context.Context, target, conversation, text string) (bool, error) {
	q.calls.Add(1)
	if target != "sentinel" || conversation != requestConversation || text != "Publish? The checks passed." {
		return false, errors.New("wrong candidate")
	}
	if q.failed {
		return false, errors.New("classifier refused")
	}
	return q.yes, nil
}

func TestTextQuestionsUseStoredVerdictsAndTheLiveChoicePG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "question-phone")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM push_prefs")
	})
	q := &questionExecutor{}
	// The test goes through the same closed socket protocol as production.
	path := socketPath(t, "q.sock")
	server := action.NewServer(path, q, time.Second)
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); server.Run(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	p.srv.exec = action.NewClient(path, time.Second)
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`[{"sessionId":"`+requestConversation+`","session":"sentinel","status":"idle"}]`), &rows); err != nil {
		t.Fatal(err)
	}
	off := false
	for n, tc := range []struct {
		name                             string
		yes, failed, disabled, cut, want bool
	}{
		{name: "no"}, {name: "yes", yes: true, want: true}, {name: "error", failed: true, want: true}, {name: "disabled", disabled: true, want: true},
		{name: "cut", cut: true, want: true},
	} {
		card := chat.Item{Role: "question", BriefID: tc.name, Text: "Publish? The checks passed.", Title: "Asks in text", Pos: 42, Cut: tc.cut}
		q.yes, q.failed = tc.yes, tc.failed
		prefs := notify.Prefs{}
		if tc.disabled {
			prefs.CheckQuestions = &off
		}
		if _, err := p.srv.savePushPrefs(t.Context(), prefs); err != nil {
			t.Fatal(err)
		}
		before := q.calls.Load()
		for range 2 {
			p.srv.classifyTextQuestion(t.Context(), requestConversation, "sentinel", card)
		}
		wantCalls := int32(1)
		if tc.disabled || tc.cut {
			wantCalls = 0
		}
		if q.calls.Load()-before != wantCalls {
			t.Errorf("%s: %d calls, want %d", tc.name, q.calls.Load()-before, wantCalls)
		}
		cards := map[string][]chat.Item{requestConversation: {card}}
		got := p.srv.questionCards(t.Context(), rows, cards)
		if (len(got[requestConversation]) == 1) != tc.want {
			t.Errorf("%s: cards %v", tc.name, got)
		}
		rows[0]["status"] = json.RawMessage(`"busy"`)
		if got := p.srv.questionCards(t.Context(), rows, cards); len(got[requestConversation]) != 0 {
			t.Errorf("busy session has a question: %v", got)
		}
		rows[0]["status"] = json.RawMessage(`"idle"`)
		rows[0]["agent"] = json.RawMessage(`"codex"`)
		if got := p.srv.questionCards(t.Context(), rows, cards); len(got[requestConversation]) != 0 {
			t.Errorf("codex has a question: %v", got)
		}
		delete(rows[0], "agent")
		if n == 1 {
			items := waitingFor(nil, "", got[requestConversation], nil, nil, nil)
			if len(items) != 1 || items[0].Kind != "question" || items[0].Pos == nil || *items[0].Pos != 42 {
				t.Errorf("the feed anchor was lost: %v", items)
			}
		}
		first, err := p.db.PushQuestion(t.Context(), requestConversation, card.BriefID)
		if err != nil || !first {
			t.Fatalf("first push: %v %v", first, err)
		}
		second, err := p.db.PushQuestion(t.Context(), requestConversation, card.BriefID)
		if err != nil || second {
			t.Fatalf("repeated push: %v %v", second, err)
		}
	}
	// A live switch off reveals even a stored no, and switching it back uses
	// that same verdict without another model call.
	if _, err := p.srv.savePushPrefs(t.Context(), notify.Prefs{CheckQuestions: &off}); err != nil {
		t.Fatal(err)
	}
	card := chat.Item{Role: "question", BriefID: "no"}
	if got := p.srv.questionCards(t.Context(), rows, map[string][]chat.Item{requestConversation: {card}}); len(got[requestConversation]) != 1 {
		t.Fatal("switch off kept a stored no hidden")
	}
	if _, err := p.srv.savePushPrefs(t.Context(), notify.Prefs{}); err != nil {
		t.Fatal(err)
	}
	if got := p.srv.questionCards(t.Context(), rows, map[string][]chat.Item{requestConversation: {card}}); len(got[requestConversation]) != 0 {
		t.Fatal("switch on forgot the stored no")
	}
}

func TestQuestionClaimsSurviveRestartAndExpiredWorkIsVisiblePG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "question-claims")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	conversation := "question-claims"
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", conversation)
	})
	claimed := make(chan bool, 8)
	failed := make(chan error, 8)
	for range 8 {
		go func() {
			yes, err := p.db.ClaimQuestion(t.Context(), conversation, "message")
			claimed <- yes
			failed <- err
		}()
	}
	count := 0
	for range 8 {
		if <-claimed {
			count++
		}
		if err := <-failed; err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("concurrent scans claimed %d calls", count)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE text_questions SET started_at=now()-interval '2 minutes' WHERE conversation=$1", conversation); err != nil {
		t.Fatal(err)
	}
	verdicts, err := p.db.QuestionVerdicts(t.Context(), []string{conversation})
	if err != nil || !questionVisible(true, verdicts[conversation]["message"]) {
		t.Fatalf("interrupted classification disappeared: %v %v", verdicts, err)
	}
	again, err := p.db.ClaimQuestion(t.Context(), conversation, "message")
	if err != nil || again {
		t.Fatalf("expired claim spent another call: %v %v", again, err)
	}
}
