package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	"aacpanel/internal/question"
)

const requestConversation = "567f4d24-cd5f-48fa-bdc1-04c89d203494"

func TestOnlyTheLocalListenerAcceptsAnAgentsRequest(t *testing.T) {
	srv := &Server{}
	for _, listener := range []struct {
		gate gate
		want int
	}{
		{srv.publicGate(), http.StatusNotFound},
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
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM acts WHERE id=$1", body.ID) })
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
	if err := p.db.CallAct(t.Context(), requestConversation, "sentinel", at, false); err != nil {
		t.Fatal(err)
	}
	secret := newSecretAct(t, p.srv, "release")
	saved := newSecretAct(t, p.srv, "saved")
	if err := p.db.AnswerSecrets(t.Context(), "saved", "/srv/secrets/saved"); err != nil {
		t.Fatal(err)
	}
	cards := []chat.Item{{Role: "request", BriefID: id, Pos: 10}, {Role: "call", Text: "Choose the CI run", At: at.Format(time.RFC3339Nano), Pos: 11},
		{Role: "brief", BriefID: "request-summary-brief", Pos: 12},
		{Role: "secret", BriefID: secret, Name: "release", Title: "Release token", At: at.Format(time.RFC3339), Pos: 13},
		{Role: "secret", BriefID: saved, Name: "saved", At: at.Format(time.RFC3339), Pos: 14},
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
	rows, err := p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	var row act.Record
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
	w = post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"act":"`+id+`","answer":"done","note":"Wi-Fi is back.","text":"ignore the task"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionSend || got.Target != "sentinel" || got.Text != row.Reply("done", "Wi-Fi is back.") || got.MessageID != requestMessageID(id) {
			t.Fatalf("answer sent as %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the answer never reached session.send")
	}
	rows, err = p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id && (r.Open() || r.Answer == nil || r.Answer.Outcome != "done") {
			t.Fatalf("answer not settled: %+v", r)
		}
	}
	w = post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"act":"`+id+`","answer":"done"}}`)
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
		w := post(t, p.srv, `{"kind":"session.send","target":"`+target+`","params":{"act":"`+id+`","answer":"cant","note":"No access"}}`)
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
	rows, err := p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id && !r.Open() {
			t.Fatal("failed delivery closed the task")
		}
	}
	if err := p.db.WithdrawAct(t.Context(), requestConversation, "sentinel", id); err != nil {
		t.Fatal(err)
	}
}

func TestWaitingSummaryCountsOnlyOutstandingWork(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []act.Record{{ID: "open", Kind: "do", Title: "Restart", State: "open", Blocking: true, Created: at},
		{ID: "done", Kind: "do", State: "answered", Closed: &at}, {ID: "withdrawn", Kind: "do", State: "withdrawn", Closed: &at},
		{ID: "call", Kind: "call", State: "open", Created: at}, {ID: "secret", Kind: "secret", State: "open", Title: "Release token", Created: at},
		{ID: "ask", Kind: "ask", State: "open", Title: "Publish?", Body: act.Body{Source: "prose", Message: "message", Questions: publishQuestions()}, Created: at}}
	cards := []chat.Item{{Role: "request", BriefID: "open", Pos: 11}, {Role: "call", Text: "Need your choice", At: at.Format(time.RFC3339Nano), Pos: 12},
		{Role: "secret", BriefID: "secret", Name: "release", Pos: 13}, {Role: "secret", Name: "legacy", Pos: 14},
		{Role: "question", BriefID: "message", Pos: 15, Nth: 2}, {Role: "question", BriefID: "unclassified", Pos: 16}}
	briefs := []chat.BriefCard{{ID: "waiting", Title: "Rotation", Questions: 6}, {ID: "sent", Questions: 2}, {ID: "read"}}
	got := waitingFor(rows, cards, briefs, map[string]bool{"sent": true})
	if len(got) != 5 {
		t.Fatalf("waiting summary: %+v", got)
	}
	counts := map[string]int{}
	for _, item := range got {
		counts[item.Kind]++
		if item.Kind == "question" && (item.ID != "ask" || item.Pos == nil || *item.Pos != 15 || item.Nth != 2 || len(item.Questions) != 1 || len(item.Questions[0].Options) != 2) {
			t.Errorf("question anchor: %+v", item)
		}
	}
	for _, kind := range []string{"request", "call", "brief", "secret", "question"} {
		if counts[kind] != 1 {
			t.Errorf("%s count: %v", kind, counts)
		}
	}
	if got[0].ID != "open" || got[0].Pos == nil || *got[0].Pos != 11 {
		t.Fatalf("blocking task anchor: %+v", got[0])
	}
	rows[3].State = "answered"
	if after := waitingFor(rows, cards, briefs, map[string]bool{"sent": true}); len(after) != 4 {
		t.Fatalf("viewing cleared more than call: %+v", after)
	}
	rows[3].State = "open"
	cards[1].At = at.Add(time.Second).Format(time.RFC3339Nano)
	for _, item := range waitingFor(rows, cards, nil, nil) {
		if item.Kind == "call" && item.Pos != nil {
			t.Fatalf("an old call points at a newer transcript card: %+v", item)
		}
	}
	if got := waitingFor(nil, cards, nil, nil); len(got) != 0 {
		t.Fatalf("transcript resurrected acts: %+v", got)
	}
}

func TestRequestSummaryKeepsTheTaskAfterViewingPG(t *testing.T) {
	p := newPollProbe(t, context.Background(), "request-view-phone")
	id := newPersonRequest(t, p.srv)
	if err := p.db.CallAct(t.Context(), requestConversation, "sentinel", time.Now(), false); err != nil {
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

func newSecretAct(t *testing.T, srv *Server, name string) string {
	t.Helper()
	w := httptest.NewRecorder()
	srv.apiAct(w, httptest.NewRequest(http.MethodPost, "/api/acts", strings.NewReader(`{"conversation":"`+requestConversation+`","kind":"secret","title":"Release token","body":{"name":"`+name+`"},"blocking":false}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("secret create: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	pool, _ := srv.db.Pool()
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM acts WHERE id=$1", body.ID) })
	return body.ID
}

func TestOnlyTheLocalListenerCreatesActs(t *testing.T) {
	srv := &Server{}
	for _, local := range []bool{false, true} {
		gate := srv.publicGate()
		want := http.StatusMethodNotAllowed
		if local {
			gate = srv.localGate()
			want = http.StatusServiceUnavailable
		}
		w := httptest.NewRecorder()
		srv.routes(gate).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/acts", strings.NewReader(`{}`)))
		if w.Code != want {
			t.Errorf("local=%v: %d want %d", local, w.Code, want)
		}
	}
}

func TestSecretActDeclineAndSaveReachTheBoundConversationPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "secret-act-phone")
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "saved", Secrets: &action.Secrets{Dir: "/srv/secrets"}})
	p.srv.exec = client
	for _, answer := range []string{"decline", "save"} {
		id := newSecretAct(t, p.srv, "release.env")
		payload := `{"kind":"session.send","target":"sentinel","params":{"act":"` + id + `","answer":"decline","note":"Cannot share it."}}`
		if answer == "save" {
			payload = `{"kind":"secret.put","target":"sentinel","params":{"act":"` + id + `","name":"release.env","text":"TOKEN=private"}}`
		}
		w := post(t, p.srv, payload)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", answer, w.Code, w.Body.String())
		}
		sent := <-fake.got
		rows, err := p.db.ActsOf(t.Context(), requestConversation)
		if err != nil {
			t.Fatal(err)
		}
		var row act.Record
		for _, got := range rows {
			if got.ID == id {
				row = got
			}
		}
		want := "declined"
		if answer == "save" {
			want = "answered"
		}
		if row.State != want || row.AnsweredBy != "person" || row.Answer == nil {
			t.Fatalf("%s: %+v", answer, row)
		}
		if answer == "decline" {
			if sent.Text != row.Reply("decline", "Cannot share it.") || sent.MessageID != act.MessageID(id) || row.Answer.Note != "Cannot share it." {
				t.Fatalf("decline: %+v %+v", sent, row)
			}
		} else {
			if sent.Secret == nil || sent.Secret.Act != id || row.Answer.Path != "/srv/secrets/release.env" {
				t.Fatalf("save: %+v %+v", sent, row)
			}
			if query := <-fake.got; query.Ask != action.AskSecrets {
				t.Fatalf("secret path query: %+v", query)
			}
		}
		w = post(t, p.srv, payload)
		if w.Code != http.StatusConflict {
			t.Fatalf("duplicate %s: %d %s", answer, w.Code, w.Body.String())
		}
	}
}

func TestActsAPIListsAndWithdrawsOnlyTheCallersActsPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "act-list-phone")
	id := newSecretAct(t, p.srv, "release.env")
	w := httptest.NewRecorder()
	p.srv.apiActs(w, httptest.NewRequest(http.MethodGet, "/api/acts?session="+requestConversation, nil))
	var body struct {
		Acts []act.Record `json:"acts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range body.Acts {
		if row.ID == id {
			found = row.Kind == "secret" && row.Open() && row.Body.Name == "release.env"
		}
	}
	if w.Code != http.StatusOK || !found {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	withdraw := func() int {
		w := httptest.NewRecorder()
		p.srv.apiAct(w, httptest.NewRequest(http.MethodPost, "/api/acts", strings.NewReader(`{"conversation":"`+requestConversation+`","id":"`+id+`","withdraw":true}`)))
		return w.Code
	}
	if got := withdraw(); got != http.StatusOK {
		t.Fatalf("withdraw: %d", got)
	}
	if got := withdraw(); got != http.StatusConflict {
		t.Fatalf("second withdraw: %d", got)
	}
}

func TestPersonAndTranscriptRepliesSettleQuestionActsPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "person-question-act-phone")
	client, fake := startFakeExec(t, action.Response{OK: true})
	p.srv.exec = client
	pool, _ := p.db.Pool()
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM acts WHERE conversation=$1 AND kind='ask'", requestConversation)
		pool.Exec(context.Background(), "DELETE FROM text_questions WHERE conversation=$1", requestConversation)
	})
	for _, message := range []string{"panel-reply", "console-reply"} {
		if _, err := p.db.ClaimQuestion(t.Context(), requestConversation, message); err != nil {
			t.Fatal(err)
		}
		if err := p.db.SetQuestionVerdict(t.Context(), requestConversation, "sentinel", message, "yes", question.Verdict{What: "Publish?", Questions: publishQuestions()}); err != nil {
			t.Fatal(err)
		}
		if message == "panel-reply" {
			w := post(t, p.srv, `{"kind":"session.send","target":"sentinel","params":{"text":"Yes, publish."}}`)
			if w.Code != http.StatusOK {
				t.Fatalf("reply: %d %s", w.Code, w.Body.String())
			}
			<-fake.got
		} else {
			agent := startAgent(t, map[string]any{"ok": true, "waiting": map[string][]chat.Item{}, "answeredQuestions": map[string][]string{requestConversation: {message}}})
			p.srv.chat = chat.New(agent.path)
			if _, err := p.srv.waitingCards(t.Context(), []string{requestConversation}); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := p.db.ActsOf(t.Context(), requestConversation)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.Kind == "ask" && row.Body.Message == message {
				found = true
				if row.State != "answered" || row.AnsweredBy != "person" {
					t.Fatalf("person answer: %+v", row)
				}
			}
		}
		if !found {
			t.Fatal("question act missing")
		}
	}
}

func TestSavingByNameSettlesAllSecretActsAndANotificationFailureStillSettlesPG(t *testing.T) {
	p := newPollProbe(t, t.Context(), "secret-name-phone")
	id := newSecretAct(t, p.srv, "release.env")
	client, fake := startFakeExec(t, action.Response{OK: true, Secrets: &action.Secrets{Dir: "/srv/secrets"}})
	p.srv.exec = client
	w := post(t, p.srv, `{"kind":"secret.put","target":"sentinel","params":{"name":"release.env","text":"TOKEN=private"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save by name: %d %s", w.Code, w.Body.String())
	}
	<-fake.got
	<-fake.got
	rows, err := p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == id && (row.State != "answered" || row.Answer == nil || row.Answer.Path != "/srv/secrets/release.env") {
			t.Fatalf("saved by name: %+v", row)
		}
	}
	id = newSecretAct(t, p.srv, "release.env")
	client, fake = startFakeExec(t, action.Response{OK: false, Error: "saved but session not told", SecretPath: "/srv/secrets/release.env"})
	p.srv.exec = client
	w = post(t, p.srv, `{"kind":"secret.put","target":"sentinel","params":{"act":"`+id+`","name":"release.env","text":"TOKEN=private"}}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("notification failure: %d %s", w.Code, w.Body.String())
	}
	<-fake.got
	rows, err = p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == id && (row.State != "answered" || row.Answer == nil || row.Answer.Path != "/srv/secrets/release.env") {
			t.Fatalf("saved without notification: %+v", row)
		}
	}

	// The pad of one act writes the file every other ask of that name waits for.
	first, second := newSecretAct(t, p.srv, "release.env"), newSecretAct(t, p.srv, "release.env")
	client, fake = startFakeExec(t, action.Response{OK: true, SecretPath: "/srv/secrets/release.env"})
	p.srv.exec = client
	w = post(t, p.srv, `{"kind":"secret.put","target":"sentinel","params":{"act":"`+first+`","name":"release.env","text":"TOKEN=private"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save by act: %d %s", w.Code, w.Body.String())
	}
	<-fake.got
	rows, err = p.db.ActsOf(t.Context(), requestConversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if (row.ID == first || row.ID == second) && row.State != "answered" {
			t.Fatalf("an ask of the saved name stays open: %+v", row)
		}
	}
}
