package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
)

func TestAskActsCannotBeCreatedOrAnsweredPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "retired-ask-phone")
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1 AND kind='ask'", requestConversation)
	})
	for _, source := range []string{"tool", "prose"} {
		raw, err := json.Marshal(map[string]any{"conversation": requestConversation, "kind": "ask", "title": "Publish?", "blocking": false,
			"body": act.Body{Source: source, Questions: publishQuestions()}})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		p.srv.apiAct(w, httptest.NewRequest(http.MethodPost, "/api/acts", strings.NewReader(string(raw))))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("create %s ask: %d %s", source, w.Code, w.Body.String())
		}
	}
	blocking := false
	row, err := p.db.CreateAct(t.Context(), requestConversation, "sentinel", act.Input{Kind: "ask", Title: "Publish?", Blocking: &blocking,
		Body: act.Body{Source: "prose", Questions: publishQuestions()}})
	if err != nil {
		t.Fatal(err)
	}
	client, fake := startFakeExec(t, action.Response{OK: true})
	p.srv.exec = client
	w := post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"act":"`+row.ID+`","answers":[{"text":"Publish"}]}}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("answer retired ask: %d %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a retired ask sent a letter: %+v", got)
	default:
	}
}
