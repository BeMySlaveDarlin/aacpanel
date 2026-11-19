package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	"aacpanel/internal/question"
	"reflect"
)

type questionExecutor struct {
	calls        atomic.Int32
	yes          bool
	failed       bool
	action       string
	started      chan struct{}
	release      <-chan struct{}
	alreadyAsked []string
}

func (q *questionExecutor) Execute(context.Context, action.Request) (string, error) {
	return "", errors.New("no actions")
}
func (q *questionExecutor) Question(ctx context.Context, target, conversation, text string, alreadyAsked []string) (question.Verdict, error) {
	q.calls.Add(1)
	if !slices.Equal(alreadyAsked, q.alreadyAsked) {
		return question.Verdict{}, fmt.Errorf("open questions: %v; want %v", alreadyAsked, q.alreadyAsked)
	}
	if target != "sentinel" || conversation != requestConversation || text != "Publish? The checks passed." {
		return question.Verdict{}, errors.New("wrong candidate")
	}
	if q.failed {
		return question.Verdict{}, errors.New("classifier refused")
	}
	if q.started != nil {
		q.started <- struct{}{}
	}
	if q.release != nil {
		select {
		case <-q.release:
		case <-ctx.Done():
			return question.Verdict{}, ctx.Err()
		}
	}
	v := question.Verdict{Questions: []question.Question{}, Action: q.action}
	if q.yes {
		v.Questions = publishQuestions()
	}
	if q.yes || q.action != "" {
		v.What = "Publish?"
	}
	return v, nil
}

func TestBothClassifiersCarryOnlyOpenQuestionsOfTheirConversationPG(t *testing.T) {
	for _, hook := range []bool{false, true} {
		t.Run(fmt.Sprintf("hook=%v", hook), func(t *testing.T) {
			p := newPollProbe(t, t.Context(), "classifier-open-questions")
			pool, err := p.db.Pool()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
				pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=ANY($1)", []string{requestConversation, "other-question-conversation"})
			})
			q := &questionExecutor{action: "Merge the change"}
			path := socketPath(t, "open-questions.sock")
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
			classify := func(n int) {
				t.Helper()
				id := fmt.Sprintf("12345678-1234-1234-1234-%012d", n)
				if hook {
					if got, _ := turnRequest(t, p.srv, id, "Publish? The checks passed."); got != q.action {
						t.Fatalf("hook verdict: %q", got)
					}
				} else {
					p.srv.classifyTextQuestion(t.Context(), requestConversation, "sentinel", chat.Item{BriefID: id, Text: "Publish? The checks passed."})
					verdicts, err := p.db.QuestionVerdicts(t.Context(), []string{requestConversation})
					if err != nil || verdicts[requestConversation][id].Action != q.action {
						t.Fatalf("fallback verdict: %+v %v", verdicts, err)
					}
				}
			}
			blocking := false
			create := func(conversation, kind, source, text string) act.Record {
				t.Helper()
				body := act.Body{Steps: "Check the screen"}
				if kind == "ask" {
					body = act.Body{Source: source, Questions: []question.Question{{Question: text}}}
				}
				r, err := p.db.CreateAct(t.Context(), conversation, "sentinel", act.Input{Kind: kind, Title: "Choose the plan", Blocking: &blocking, Body: body})
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			create("other-question-conversation", "ask", "tool", "Other conversation?")
			create(requestConversation, "do", "", "")
			closed := create(requestConversation, "ask", "prose", "Closed question?")
			if err := p.db.WithdrawAct(t.Context(), requestConversation, "sentinel", closed.ID); err != nil {
				t.Fatal(err)
			}
			classify(1)
			create(requestConversation, "ask", "tool", "Which region?")
			create(requestConversation, "ask", "prose", "Which window?")
			q.alreadyAsked = []string{"Which region?", "Which window?"}
			classify(2)
			if q.calls.Load() != 2 {
				t.Fatalf("classifier calls: %d", q.calls.Load())
			}
		})
	}
}

func TestTextQuestionsUseStoredVerdictsAndTheLiveChoicePG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "question-phone")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1 AND kind='ask'", requestConversation)
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
		q.alreadyAsked, err = p.db.OpenAskQuestions(t.Context(), requestConversation)
		if err != nil {
			t.Fatal(err)
		}
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
			if got[requestConversation][0].Title != "Publish?" || got[requestConversation][0].Text != card.Text {
				t.Fatalf("the question lost its summary or full message: %+v", got)
			}
			acts, err := p.db.OpenActs(t.Context(), []string{requestConversation})
			if err != nil {
				t.Fatal(err)
			}
			items := waitingFor(acts, got[requestConversation], nil, nil)
			if len(items) != 1 || items[0].Kind != "question" || items[0].Title != "Publish?" || !reflect.DeepEqual(items[0].Questions, publishQuestions()) || items[0].Pos == nil || *items[0].Pos != 42 {
				t.Errorf("the feed anchor was lost: %v", items)
			}
			wire, err := json.Marshal(items)
			if err != nil || !strings.Contains(string(wire), `"questions":[{"question":"Publish?"`) {
				t.Fatalf("waiting replies did not travel: %s, %v", wire, err)
			}
		}
		verdicts, err := p.db.QuestionVerdicts(t.Context(), []string{requestConversation})
		wantQuestions := []question.Question{}
		if tc.yes {
			wantQuestions = publishQuestions()
		}
		if err != nil || (len(verdicts[requestConversation][tc.name].Questions) != len(wantQuestions) || (len(wantQuestions) > 0 && !reflect.DeepEqual(verdicts[requestConversation][tc.name].Questions, wantQuestions))) {
			t.Fatalf("%s lost stored replies: %+v, %v", tc.name, verdicts, err)
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
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1 AND kind='ask'", conversation)
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
	if err != nil || verdicts[conversation]["message"].Questions == nil || len(verdicts[conversation]["message"].Questions) != 0 {
		t.Fatalf("an unclassified message has no empty replies: %+v, %v", verdicts, err)
	}
	if err != nil || !questionVisible(true, verdicts[conversation]["message"].Verdict) {
		t.Fatalf("interrupted classification disappeared: %v %v", verdicts, err)
	}
	again, err := p.db.ClaimQuestion(t.Context(), conversation, "message")
	if err != nil || again {
		t.Fatalf("expired claim spent another call: %v %v", again, err)
	}
	if fresh, err := p.db.PushQuestion(t.Context(), conversation, "message"); err != nil || !fresh {
		t.Fatalf("push claim: %v %v", fresh, err)
	}
	// A failed collector read must not retire the question. A complete empty
	// snapshot must, including when the whole session has left the host.
	p.srv.host = hostWith(t, `{"sessions":[{"sessionId":"`+conversation+`","session":"sentinel","status":"idle"}]}`)
	active := startAgentSeq(t,
		map[string]any{"ok": true, "waiting": map[string][]chat.Item{conversation: {{Role: "question", BriefID: "message"}}}},
		map[string]any{"ok": false, "error": "the transcript is unavailable"},
		map[string]any{"ok": true, "waiting": map[string][]chat.Item{conversation: {}}})
	p.srv.chat = chat.New(active.path)
	scan := func() {
		p.srv.scanTextQuestions(t.Context(), func(string, string, chat.Item) { t.Error("a stored question was classified again") })
	}
	for range 2 {
		scan()
		tags, err := p.db.ClaimQuestionClosures(t.Context())
		if err != nil || len(tags) != 0 {
			t.Fatalf("an active or unreadable question closed: %v %v", tags, err)
		}
	}
	scan()
	tags, err := p.db.ClaimQuestionClosures(t.Context())
	if err != nil || !slices.Equal(tags, []string{notify.QuestionTag(conversation, "message")}) {
		t.Fatalf("closure: %v %v", tags, err)
	}
	scan()
	tags, err = p.db.ClaimQuestionClosures(t.Context())
	if err != nil || len(tags) != 0 {
		t.Fatalf("closure sent twice: %v %v", tags, err)
	}
	if _, err := p.db.PushQuestion(t.Context(), conversation, "gone"); err != nil {
		t.Fatal(err)
	}
	p.srv.host = hostWith(t, `{"sessions":[]}`)
	scan()
	tags, err = p.db.ClaimQuestionClosures(t.Context())
	if err != nil || !slices.Equal(tags, []string{notify.QuestionTag(conversation, "gone")}) {
		t.Fatalf("a vanished session kept its push: %v %v", tags, err)
	}

}

func TestWaitingQuestionCarriesItsForm(t *testing.T) {
	items := waitingFor([]act.Record{{ID: "act", Kind: "ask", State: "open", Title: "Publish?", Body: act.Body{Message: "message", What: "Publish?", Questions: publishQuestions()}}}, []chat.Item{{Role: "question", BriefID: "message"}}, nil, nil)
	wire, err := json.Marshal(items)
	if err != nil || !strings.Contains(string(wire), `"questions":[{"question":"Publish?"`) || !strings.Contains(string(wire), `"what":"Publish?"`) || strings.Contains(string(wire), `"answers"`) {
		t.Fatalf("form: %s %v", wire, err)
	}
}

func publishQuestions() []question.Question {
	return []question.Question{{Question: "Publish?", Header: "Release", Options: []question.Option{{Label: "Publish"}, {Label: "Wait"}}}}
}
