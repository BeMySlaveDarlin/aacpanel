package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/question"
)

func TestAskFormValidatesBeforeSendingAndClosesAfterDeliveryPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "ask-form-phone")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1 AND kind='ask'", requestConversation)
	})
	questions := []question.Question{
		{Question: "Which checks?", Header: "Checks", Multi: true, Options: []question.Option{{Label: "Unit"}, {Label: "Integration"}}},
		{Question: "Which branch?", Header: "Branch", Options: []question.Option{}},
	}
	raw, err := json.Marshal(map[string]any{"conversation": requestConversation, "kind": "ask", "title": "Checks and branch", "blocking": false, "body": act.Body{Source: "tool", What: "Checks and branch", Questions: questions}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	p.srv.apiAct(w, httptest.NewRequest(http.MethodPost, "/api/acts", strings.NewReader(string(raw))))
	var created struct {
		ID string `json:"id"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("create ask: %d %s", w.Code, w.Body.String())
	}
	client, fake := startFakeExec(t, action.Response{OK: true})
	p.srv.exec = client
	for _, answers := range []string{`[]`, `[{}]`, `[{"picks":["Unit"]},{"text":" "}]`, `[{"picks":["Foreign"]},{"text":"main"}]`, `[{"picks":["Unit"],"extra":true},{"text":"main"}]`, `"invalid"`} {
		w = post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"act":"`+created.ID+`","answers":`+answers+`}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid answer %s: %d %s", answers, w.Code, w.Body.String())
		}
		select {
		case got := <-fake.got:
			t.Fatalf("invalid answers sent a letter: %+v", got)
		default:
		}
	}
	answers := []question.Answer{{Picks: []string{"Unit", "Integration"}, Text: "and smoke"}, {Text: "main"}}
	payload, err := json.Marshal(map[string]any{"kind": "session.send", "target": "sentinel", "params": map[string]any{"act": created.ID, "answers": answers, "answer": "cant", "note": "ignored legacy fields"}})
	if err != nil {
		t.Fatal(err)
	}
	w = post(t, p.srv, string(payload))
	if w.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	row := act.Record{ID: created.ID, Kind: "ask", Body: act.Body{Source: "tool", Questions: questions}}
	if got.Kind != action.SessionSend || got.MessageID != act.MessageID(created.ID) || got.Text != row.Reply("", "", answers...) {
		t.Fatalf("answer letter: %+v", got)
	}
	rows, err := p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == created.ID && (r.Open() || r.State != "answered" || r.Answer == nil || r.Answer.Outcome != "" || r.Answer.Note != "" || !reflect.DeepEqual(r.Answer.Answers, answers)) {
			t.Fatalf("saved ask: %+v", r)
		}
	}
	w = post(t, p.srv, string(payload))
	if w.Code != http.StatusConflict {
		t.Fatalf("repeat answer: %d", w.Code)
	}
	select {
	case got := <-fake.got:
		t.Fatalf("repeat answer sent another letter: %+v", got)
	default:
	}
}

func TestFailedAskDeliveryLeavesTheFormOpenPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "ask-failure-phone")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1 AND kind='ask'", requestConversation)
	})
	blocking := false
	row, err := p.db.CreateAct(t.Context(), requestConversation, "sentinel", act.Input{Kind: "ask", Title: "Publish?", Blocking: &blocking, Body: act.Body{Source: "tool", Questions: publishQuestions()}})
	if err != nil {
		t.Fatal(err)
	}
	p.srv.exec, _ = startFakeExec(t, action.Response{OK: false, Error: "not delivered"})
	w := post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"act":"`+row.ID+`","answers":[{"text":"Later"}]}}`)
	if w.Code == http.StatusOK {
		t.Fatal("failed delivery was accepted")
	}
	rows, err := p.db.OpenActs(t.Context(), []string{requestConversation})
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("failed delivery retired the form: %+v %v", rows, err)
	}
}
