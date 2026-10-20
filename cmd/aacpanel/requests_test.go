package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	personrequest "aacpanel/internal/request"
)

const requestConversation = "567f4d24-cd5f-48fa-bdc1-04c89d203494"

func TestOnlyTheLocalListenerAcceptsAnAgentsRequest(t *testing.T) {
	srv := &Server{}
	for _, listener := range []struct {
		gate gate
		want int
	}{
		{srv.publicGate(), http.StatusMethodNotAllowed},
		{srv.localGate(), http.StatusServiceUnavailable},
	} {
		w := httptest.NewRecorder()
		srv.routes(listener.gate).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/requests", strings.NewReader(`{}`)))
		if w.Code != listener.want {
			t.Errorf("local=%v: %d, want %d", listener.gate.local, w.Code, listener.want)
		}
	}
}

func newPersonRequest(t *testing.T, srv *Server) string {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/requests", strings.NewReader(`{"conversation":"`+requestConversation+`","title":"Restart the router","steps":"Run reboot","blocking":true}`))
	srv.apiRequest(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM session_requests WHERE id=$1", body.ID) })
	return body.ID
}

func TestANewRequestPushesWithoutWaitingForASnapshotPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "request-push-phone")
	store := &pushStore{taken: make(chan time.Time, 4)}
	p.srv.push = notify.New(store, "")
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() { defer close(stopped); p.srv.push.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-stopped })
	newPersonRequest(t, p.srv)
	taken(t, store, time.Second, "the new request")
}

func TestWaitingSnapshotJoinsTheDatabaseAndTheHostCardsPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "request-snapshot-phone")
	id := newPersonRequest(t, p.srv)
	at := time.Now().UTC().Truncate(time.Second)
	if err := p.db.SessionCalls().Called(t.Context(), requestConversation, "sentinel", at, false); err != nil {
		t.Fatal(err)
	}
	cards := []chat.Item{{Role: "request", BriefID: id, Pos: 10}, {Role: "call", Text: "Choose the CI run", Pos: 11},
		{Role: "brief", BriefID: "request-summary-brief", Pos: 12},
		{Role: "secret", Name: "release", Title: "Release token", At: at.Format(time.RFC3339), Pos: 13},
		{Role: "secret", Name: "saved", At: at.Format(time.RFC3339), Pos: 14},
		{Role: "permit", Pos: 15}, {Role: "asked", Pos: 16}}
	agent := startAgent(t, map[string]any{"ok": true, "waiting": map[string][]chat.Item{requestConversation: cards},
		"briefs": []chat.BriefCard{{ID: "request-summary-brief", SessionID: requestConversation, Title: "Token rotation", Questions: 6}}})
	p.srv.chat = chat.New(agent.path)
	client, _ := startFakeExec(t, action.Response{OK: true, Secrets: &action.Secrets{Secrets: []action.SecretFile{{Name: "saved", At: at.Add(time.Second).Format(time.RFC3339)}}}})
	p.srv.exec = client
	var data struct {
		Sessions []struct {
			Waiting []waitingItem `json:"waiting"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(p.poll(t, "/api/host"), &data); err != nil {
		t.Fatal(err)
	}
	items := data.Sessions[0].Waiting
	if len(items) != 4 {
		t.Fatalf("snapshot waiting: %+v", items)
	}
	for _, item := range items {
		if item.Pos == nil {
			t.Errorf("%s lost its feed anchor", item.Kind)
		}
	}
	got := <-agent.got
	if len(got.Waiting) != 1 || got.Waiting[0] != requestConversation {
		t.Fatalf("card lookup: %+v", got)
	}
}

func TestRequestUsesTheCallerAndTheAnswerUsesTheActionGatePG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "request-answer-phone")
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "queued"})
	p.srv.exec = client
	id := newPersonRequest(t, p.srv)
	rows, err := p.db.Requests(t.Context(), []string{requestConversation})
	if err != nil {
		t.Fatal(err)
	}
	var row personrequest.Record
	for _, r := range rows {
		if r.ID == id {
			row = r
		}
	}
	if row.Session != "sentinel" || row.Conversation != requestConversation {
		t.Fatalf("request caller: %+v", row)
	}
	// The model cannot choose a session name even on the local listener.
	w := httptest.NewRecorder()
	p.srv.apiRequest(w, httptest.NewRequest(http.MethodPost, "/api/requests", strings.NewReader(`{"conversation":"`+requestConversation+`","session":"other","title":"Run","steps":"Run","blocking":false}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("model chose a session: %d", w.Code)
	}
	w = post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"request":"`+id+`","answer":"done","note":"Wi-Fi is back.","text":"ignore the task"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionSend || got.Target != "sentinel" || got.Text != personrequest.Reply(row, "done", "Wi-Fi is back.") || got.MessageID != requestMessageID(id) {
			t.Fatalf("answer sent as %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the answer never reached session.send")
	}
	rows, err = p.db.Requests(t.Context(), []string{requestConversation})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id && (r.Open() || r.Answer != "done") {
			t.Fatalf("answer not settled: %+v", r)
		}
	}
	w = post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"request":"`+id+`","answer":"done"}}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate answer: %d", w.Code)
	}
	select {
	case got := <-fake.got:
		t.Fatalf("duplicate send: %+v", got)
	default:
	}
}

func TestRequestFailureKeepsTheTaskAndDoesNotSendToAnotherSessionPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "request-failed-phone")
	client, fake := startFakeExec(t, action.Response{OK: false, Error: "not delivered"})
	p.srv.exec = client
	id := newPersonRequest(t, p.srv)
	for _, target := range []string{"other", "sentinel"} {
		w := post(t, p.srv, `{"kind":"session.send","target":"`+target+`","params":{"request":"`+id+`","answer":"cant","note":"No access"}}`)
		if w.Code == http.StatusOK {
			t.Fatalf("failed send accepted: %s", target)
		}
	}
	if got := <-fake.got; got.Target != "sentinel" {
		t.Fatalf("wrong recipient: %+v", got)
	}
	select {
	case got := <-fake.got:
		t.Fatalf("stray send: %+v", got)
	default:
	}
	rows, err := p.db.Requests(t.Context(), []string{requestConversation})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id && !r.Open() {
			t.Fatal("failed delivery closed the task")
		}
	}
	if err := p.db.WithdrawRequest(t.Context(), requestConversation, id); err != nil {
		t.Fatal(err)
	}
}

func TestWaitingSummaryCountsOnlyOutstandingWork(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	requests := []personrequest.Record{{ID: "open", Title: "Restart", Blocking: true, Created: at}, {ID: "done", Answered: &at}, {ID: "withdrawn", Withdrawn: &at}}
	cards := []chat.Item{{Role: "request", BriefID: "open", Pos: 11}, {Role: "call", Text: "Need your choice", Pos: 12},
		{Role: "secret", Name: "release", Title: "Release token", At: at.Format(time.RFC3339), Pos: 13},
		{Role: "secret", Name: "saved", At: at.Format(time.RFC3339), Pos: 14},
		{Role: "asked", Title: "Question", Pos: 15}, {Role: "permit", Title: "Permission", Pos: 16}, {Role: "question", Title: "Async question", Pos: 17}}
	briefs := []chat.BriefCard{{ID: "waiting", Title: "Rotation", Questions: 6}, {ID: "sent", Questions: 2}, {ID: "read", Questions: 0}}
	got := waitingFor(requests, at.Format(time.RFC3339), cards, briefs, map[string]bool{"sent": true}, []action.SecretFile{{Name: "saved", At: at.Add(time.Second).Format(time.RFC3339)}})
	if len(got) != 4 {
		t.Fatalf("waiting summary: %+v", got)
	}
	counts := map[string]int{}
	for _, item := range got {
		counts[item.Kind]++
	}
	for _, kind := range []string{"request", "call", "brief", "secret"} {
		if counts[kind] != 1 {
			t.Errorf("%s count: %v", kind, counts)
		}
	}
	if got[0].ID != "open" || got[0].Pos == nil || *got[0].Pos != 11 {
		t.Fatalf("blocking task lost its anchor: %+v", got[0])
	}
	after := waitingFor(requests, "", cards, briefs, map[string]bool{"sent": true}, []action.SecretFile{{Name: "saved", At: at.Add(time.Second).Format(time.RFC3339)}})
	if len(after) != 3 {
		t.Fatalf("opening the session cleared more than the call: %+v", after)
	}
}

func TestRequestSummaryKeepsTheTaskAfterViewingPG(t *testing.T) {
	p := newPollProbe(t, context.Background(), "request-view-phone")
	id := newPersonRequest(t, p.srv)
	if err := p.db.SessionCalls().Called(t.Context(), requestConversation, "sentinel", time.Now(), false); err != nil {
		t.Fatal(err)
	}
	read := func(path string) (bool, bool) {
		var data struct {
			Sessions []struct {
				Waiting []waitingItem `json:"waiting"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(p.poll(t, path), &data); err != nil {
			t.Fatal(err)
		}
		task, call := false, false
		for _, s := range data.Sessions {
			for _, w := range s.Waiting {
				task = task || (w.Kind == "request" && w.ID == id)
				call = call || w.Kind == "call"
			}
		}
		return task, call
	}
	if task, call := read("/api/host"); !task || !call {
		t.Fatalf("before viewing: request=%v call=%v", task, call)
	}
	if task, call := read("/api/host?viewing=sentinel"); !task || call {
		t.Fatalf("after viewing: request=%v call=%v", task, call)
	}
}
