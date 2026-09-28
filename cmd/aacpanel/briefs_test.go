package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"aacpanel/internal/chat"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func sampleBrief() *chat.Brief {
	return &chat.Brief{
		ID:        "seven-after-twelve",
		SessionID: "567f4d24-cd5f-48fa-bdc1-04c89d203494",
		Title:     "Seven questions after twelve",
		Questions: []chat.BriefQuestion{
			{
				ID: "r1", N: "01", Title: "The configuration directory", Kind: "pick",
				Options: []chat.BriefOption{
					{Key: "A", Label: "Curate through --settings"},
					{Key: "B", Label: "A directory per role"},
				},
			},
			{
				ID: "r2", N: "02", Title: "Does the pause expire", Kind: "pick",
				Options: []chat.BriefOption{{Key: "C", Label: "It does not, but it is visible"}},
			},
			{ID: "r3", N: "03", Title: "The value of N", Kind: "text"},
			{ID: "r4", N: "04", Title: "Where this came from", Kind: "none"},
		},
	}
}

func briefReplyOf(answers map[string]store.BriefAnswer) string {
	return briefReply(sampleBrief(), store.BriefDraft{Answers: answers})
}

func TestBriefReplyCarriesPicksNotesAndSkips(t *testing.T) {
	got := briefReplyOf(map[string]store.BriefAnswer{
		"r1": {Picks: []string{"A"}, Note: "try it on the lab first"},
		"r2": {Skip: true},
		"r3": {Note: "k = 2"},
	})

	for _, want := range []string{
		`Brief "Seven questions after twelve" · seven-after-twelve`,
		"Answered 3 of 4",
		"01 The configuration directory",
		"   A · Curate through --settings",
		"   note: try it on the lab first",
		"   skipped",
		"03 The value of N",
		"   note: k = 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the reply does not carry %q:\n%s", want, got)
		}
	}
}

func TestBriefReplyKeepsTheOrderOfTheDocument(t *testing.T) {
	got := briefReplyOf(map[string]store.BriefAnswer{
		"r3": {Note: "third"},
		"r1": {Picks: []string{"B"}},
		"r2": {Picks: []string{"C"}},
	})
	first := strings.Index(got, "01 ")
	second := strings.Index(got, "02 ")
	third := strings.Index(got, "03 ")
	if !(first < second && second < third) {
		t.Errorf("the answers arrived in an order the person did not write them in:\n%s", got)
	}
}

func TestBriefReplyNamesAQuestionLeftUnanswered(t *testing.T) {
	got := briefReplyOf(map[string]store.BriefAnswer{"r1": {Picks: []string{"A"}}})
	if !strings.Contains(got, "02 Does the pause expire\n   no answer") {
		t.Errorf("a question with nothing under it is silently absent, and the session reads that as answered:\n%s", got)
	}
}

func TestBriefReplyLeavesOutAQuestionThatAsksNothing(t *testing.T) {
	got := briefReplyOf(map[string]store.BriefAnswer{"r1": {Picks: []string{"A"}}})
	if strings.Contains(got, "04 Where this came from") {
		t.Errorf("a block that asks nothing came back as an unanswered question:\n%s", got)
	}
}

func TestBriefReplyNamesAPickTheDocumentNoLongerOffers(t *testing.T) {
	got := briefReplyOf(map[string]store.BriefAnswer{"r1": {Picks: []string{"Z"}}})
	if !strings.Contains(got, "   Z · ") {
		t.Errorf("a pick from an older issue of the document vanished from the reply:\n%s", got)
	}
}

func TestBriefDraftRefusesWhatIsNotAnAnswer(t *testing.T) {
	long := strings.Repeat("x", briefMaxNote+1)
	cases := map[string]map[string]store.BriefAnswer{
		"a key that does not name a question": {"../etc": {Note: "x"}},
		"a note over the ceiling":             {"r1": {Note: long}},
		"more picks than options":             {"r1": {Picks: make([]string, briefMaxPicks+1)}},
		"a pick that is not a key":            {"r1": {Picks: []string{"a whole sentence"}}},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := cleanBriefAnswers(answers); err == nil {
				t.Error("taken into the database as an answer")
			}
		})
	}
}

func TestBriefsWithoutACollectorSayWhy(t *testing.T) {
	srv := &Server{}
	for _, call := range []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
	}{
		{"/api/briefs", srv.apiBriefs},
		{"/api/briefs/{id}", srv.apiBrief},
	} {
		w := httptest.NewRecorder()
		call.run(w, httptest.NewRequest(http.MethodGet, "/api/briefs", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with no collector gave %d, expected 503", call.name, w.Code)
		}
	}
}

func TestBriefsListComesBackAsCards(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true,
		"briefs": []map[string]any{
			{"id": "seven-after-twelve", "title": "Seven questions after twelve", "questions": 7},
		},
	})
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiBriefs(w, httptest.NewRequest(http.MethodGet, "/api/briefs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Briefs []chat.BriefCard `json:"briefs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Briefs) != 1 || body.Briefs[0].Questions != 7 {
		t.Fatalf("the shelf came back as %+v", body.Briefs)
	}
}

// The documents live on the host, so the shelf and a brief open with no
// database at all — what goes missing is the answers, not the reading.
func TestBriefsOpenWithoutADatabase(t *testing.T) {
	agent := startAgentSeq(t,
		map[string]any{"ok": true, "briefs": []map[string]any{
			{"id": "seven-after-twelve", "title": "Seven questions after twelve", "questions": 4},
		}},
		map[string]any{"ok": true, "brief": sampleBrief()},
	)
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiBriefs(w, httptest.NewRequest(http.MethodGet, "/api/briefs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("the shelf with no database gave %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/briefs/seven-after-twelve", nil)
	r.SetPathValue("id", "seven-after-twelve")
	srv.apiBrief(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("a brief with no database gave %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Reply, "Answered 0 of") {
		t.Errorf("the reply built with no draft behind it says: %q", body.Reply)
	}

	// Saving, on the other hand, has nowhere to go and says so.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPut, "/api/briefs/seven-after-twelve",
		strings.NewReader(`{"answers":{}}`))
	r.SetPathValue("id", "seven-after-twelve")
	srv.apiBriefDraft(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("saving a draft with no database gave %d, expected 503", w.Code)
	}
}

func TestBriefsListRefusesASessionThatIsNotAnID(t *testing.T) {
	agent := startAgent(t, map[string]any{"ok": true, "briefs": []any{}})
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiBriefs(w, httptest.NewRequest(http.MethodGet, "/api/briefs?session=sentinel", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a session name went to the collector as an id: %d", w.Code)
	}
}

func TestBriefRefusesANameThatIsNotAName(t *testing.T) {
	agent := startAgent(t, map[string]any{"ok": true})
	srv := &Server{chat: chat.New(agent.path)}

	for _, bad := range []string{"../../etc/passwd", "Seven", "", "a b"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/briefs/x", nil)
		r.SetPathValue("id", bad)
		srv.apiBrief(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("the name %q reached the collector: %d", bad, w.Code)
		}
	}
}

func TestBriefAndItsDraftPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	agent := startAgent(t, map[string]any{"ok": true, "brief": sampleBrief()})
	srv := &Server{db: db, chat: chat.New(agent.path)}

	get := func() (string, map[string]store.BriefAnswer) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/briefs/seven-after-twelve", nil)
		r.SetPathValue("id", "seven-after-twelve")
		srv.apiBrief(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			Brief *chat.Brief      `json:"brief"`
			Draft store.BriefDraft `json:"draft"`
			Reply string           `json:"reply"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Brief == nil || body.Brief.ID != "seven-after-twelve" {
			t.Fatalf("the document did not come back: %s", w.Body)
		}
		return body.Reply, body.Draft.Answers
	}

	reply, answers := get()
	if len(answers) != 0 {
		t.Errorf("a brief nobody answered came back with answers: %+v", answers)
	}
	if !strings.Contains(reply, "Answered 0 of 4") {
		t.Errorf("the reply of an untouched brief says: %s", reply)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/briefs/seven-after-twelve",
		strings.NewReader(`{"answers":{"r1":{"picks":["A"],"note":"lab first"},"r2":{"skip":true}}}`))
	r.SetPathValue("id", "seven-after-twelve")
	srv.apiBriefDraft(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("saving the draft: %d %s", w.Code, w.Body.String())
	}

	reply, answers = get()
	if got := answers["r1"]; len(got.Picks) != 1 || got.Picks[0] != "A" || got.Note != "lab first" {
		t.Errorf("the draft came back as %+v", answers)
	}
	if !strings.Contains(reply, "Answered 2 of 4") {
		t.Errorf("the reply after two answers says: %s", reply)
	}

	// The shelf shows how far each brief has got without opening any of them.
	done, err := db.BriefProgress(t.Context(), map[string]string{"seven-after-twelve": "", "never-touched": ""})
	if err != nil {
		t.Fatal(err)
	}
	if done["seven-after-twelve"] != 2 {
		t.Errorf("progress counted %d of the two answers given", done["seven-after-twelve"])
	}
	if _, there := done["never-touched"]; there {
		t.Error("a brief nobody answered turned up in the progress list")
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/briefs/seven-after-twelve/sent", nil)
	r.SetPathValue("id", "seven-after-twelve")
	srv.apiBriefSent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("marking it sent: %d %s", w.Code, w.Body.String())
	}
	draft, err := db.BriefDraftOf(t.Context(), "seven-after-twelve", "")
	if err != nil {
		t.Fatal(err)
	}
	if draft.SentAt == nil {
		t.Error("a brief that went into the session does not remember going")
	}
	if len(draft.Answers) != 2 {
		t.Errorf("the mark took the answers with it: %+v", draft.Answers)
	}
}

// A brief whose answers have gone into a session is settled, and the service is
// where that holds. The screen locks its fields, but a request does not have to
// come from that screen: a draft saved afterwards would leave the panel showing
// one set of answers while the session holds another.
func TestASentBriefTakesNoMoreAnswersPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	doc := sampleBrief()
	doc.ID = "settled-after-sending"
	agent := startAgent(t, map[string]any{"ok": true, "brief": doc})
	srv := &Server{db: db, chat: chat.New(agent.path)}

	save := func(pick string) int {
		t.Helper()
		body := `{"answers":{"r1":{"picks":["` + pick + `"]}}}`
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPut, "/api/briefs/settled-after-sending", strings.NewReader(body))
		r.SetPathValue("id", "settled-after-sending")
		srv.apiBriefDraft(w, r)
		return w.Code
	}
	mark := func() int {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/briefs/settled-after-sending/sent", nil)
		r.SetPathValue("id", "settled-after-sending")
		srv.apiBriefSent(w, r)
		return w.Code
	}

	if code := save("A"); code != http.StatusOK {
		t.Fatalf("the first draft gave %d", code)
	}
	if code := mark(); code != http.StatusOK {
		t.Fatalf("the mark gave %d", code)
	}
	if code := save("B"); code != http.StatusConflict {
		t.Errorf("a draft after the send gave %d, expected 409", code)
	}
	if code := mark(); code != http.StatusConflict {
		t.Errorf("a second send gave %d, expected 409", code)
	}

	draft, err := db.BriefDraftOf(t.Context(), "settled-after-sending", "")
	if err != nil {
		t.Fatal(err)
	}
	if picks := draft.Answers["r1"].Picks; len(picks) != 1 || picks[0] != "A" {
		t.Errorf("the answers the session was sent were changed afterwards: %+v", draft.Answers)
	}
}

// Removing a brief from the panel takes the document off the shelf of the host
// and the answers to it out of the database: answers to a brief nobody can open
// are a record with nothing behind it.
func TestRemovingABriefTakesTheAnswersWithItPG(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}

	agent := startAgent(t, map[string]any{"ok": true, "dropped": "brief-to-remove"})
	srv := &Server{db: db, chat: chat.New(agent.path)}

	if err := db.SaveBriefDraft(t.Context(), "brief-to-remove", "2026-09-08T03:00:00Z",
		map[string]store.BriefAnswer{"r1": {Picks: []string{"A"}}}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/briefs/brief-to-remove", nil)
	r.SetPathValue("id", "brief-to-remove")
	srv.apiBriefDrop(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	// The panel asks for the whole of it: no directory, because the person
	// reading it removes what they are looking at.
	req := <-agent.got
	if req.DropBrief == nil || req.DropBrief.ID != "brief-to-remove" {
		t.Fatalf("what went to the collector: %+v", req.DropBrief)
	}
	if req.DropBrief.CWD != "" {
		t.Errorf("the panel named a directory (%q) and would be refused its own brief", req.DropBrief.CWD)
	}

	draft, err := db.BriefDraftOf(t.Context(), "brief-to-remove", "2026-09-08T03:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Answers) != 0 {
		t.Errorf("the answers outlived the document: %+v", draft.Answers)
	}
}

// A name the route would refuse never reaches the collector.
func TestABriefIsNotRemovedUnderANameThatIsNotOne(t *testing.T) {
	agent := startAgent(t, map[string]any{"ok": true, "dropped": "x"})
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/briefs/x", nil)
	r.SetPathValue("id", "../secret")
	srv.apiBriefDrop(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	select {
	case req := <-agent.got:
		t.Errorf("the collector was asked anyway: %+v", req.DropBrief)
	default:
	}
}

// The draft holds as many answers as the document holds questions.
//
// The two ceilings live in different languages and drifted apart: the collector
// took documents of a hundred questions while the panel refused a draft from
// the twenty-fifth answer on. The refusal throws away the whole draft, and the
// screen goes on collecting answers into a document that stopped saving — so
// the message that finally reaches the session carries a third of an evening's
// work and says so in a line nobody reads twice.
func TestADraftHoldsAnAnswerForEveryQuestionADocumentMayCarry(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent", "briefs.py"))
	if err != nil {
		t.Fatalf("the ceilings of the collector: %v", err)
	}
	found := regexp.MustCompile(`(?m)^MAX_QUESTIONS\s*=\s*(\d+)`).FindSubmatch(raw)
	if found == nil {
		t.Fatal("the collector names no ceiling for the questions of a document")
	}
	questions, err := strconv.Atoi(string(found[1]))
	if err != nil {
		t.Fatalf("the ceiling of the collector is not a number: %v", err)
	}
	if briefMaxAnswers < questions {
		t.Errorf("a document may carry %d questions and a draft holds %d answers: everything after the %dth "+
			"is refused, and the person answering is not told", questions, briefMaxAnswers, briefMaxAnswers)
	}

	full := make(map[string]store.BriefAnswer, questions)
	for i := 0; i < questions; i++ {
		full[fmt.Sprintf("q%d", i)] = store.BriefAnswer{Picks: []string{"A"}}
	}
	if _, err := cleanBriefAnswers(full); err != nil {
		t.Errorf("a document answered in full was refused: %v", err)
	}
}

// shelfAgent stands in for the collector with a shelf of one brief, which a
// test swaps the way a session removes its brief and publishes it again: the
// collector serves both and never tells the panel which happened.
type shelfAgent struct {
	path string
	mu   sync.Mutex
	doc  *chat.Brief
}

func startShelf(t *testing.T, doc *chat.Brief) *shelfAgent {
	t.Helper()
	a := &shelfAgent{path: socketPath(t, "chat.sock"), doc: doc}
	ln, err := net.Listen("unix", a.path)
	if err != nil {
		t.Fatalf("the agent socket: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				body, _ := io.ReadAll(conn)
				var req chat.Req
				_ = json.Unmarshal(body, &req)
				a.mu.Lock()
				doc := *a.doc
				a.mu.Unlock()
				var reply any = map[string]any{"ok": false, "error": "the fake shelf answers the shelf and a brief"}
				switch {
				case req.Briefs != nil:
					card := chat.BriefCard{ID: doc.ID, SessionID: doc.SessionID, Title: doc.Title,
						At: doc.At, FirstAt: doc.FirstAt, Questions: len(doc.Questions)}
					reply = map[string]any{"ok": true, "briefs": []chat.BriefCard{card}}
				case req.Brief == doc.ID:
					reply = map[string]any{"ok": true, "brief": &doc}
				}
				out, _ := json.Marshal(reply)
				_, _ = conn.Write(out)
			}()
		}
	}()
	return a
}

func (a *shelfAgent) publish(doc *chat.Brief) {
	a.mu.Lock()
	a.doc = doc
	a.mu.Unlock()
}

// briefDesk is the panel over a fake shelf and a real database, driven the
// way the brief screen drives it.
type briefDesk struct {
	t   *testing.T
	srv *Server
	id  string
}

func openBriefDesk(t *testing.T, doc *chat.Brief) (*briefDesk, *shelfAgent) {
	t.Helper()
	db, err := store.New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	shelf := startShelf(t, doc)
	return &briefDesk{t: t, srv: &Server{db: db, chat: chat.New(shelf.path)}, id: doc.ID}, shelf
}

func (d *briefDesk) request(method, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	d.t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/api/briefs/"+d.id, strings.NewReader(body))
	r.SetPathValue("id", d.id)
	handler(w, r)
	return w
}

func (d *briefDesk) save(answers string) int {
	d.t.Helper()
	return d.request(http.MethodPut, `{"answers":`+answers+`}`, d.srv.apiBriefDraft).Code
}

func (d *briefDesk) markSent() int {
	d.t.Helper()
	return d.request(http.MethodPost, "", d.srv.apiBriefSent).Code
}

func (d *briefDesk) open() (map[string]store.BriefAnswer, string) {
	d.t.Helper()
	w := d.request(http.MethodGet, "", d.srv.apiBrief)
	if w.Code != http.StatusOK {
		d.t.Fatalf("the brief opened with %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Draft store.BriefDraft `json:"draft"`
		Reply string           `json:"reply"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		d.t.Fatal(err)
	}
	return body.Draft.Answers, body.Reply
}

// card is the row of the brief on the shelf: how far it got and whether it
// went, which is also what a session starting in the project hears about.
func (d *briefDesk) card() (answered int, sent bool) {
	d.t.Helper()
	w := httptest.NewRecorder()
	d.srv.apiBriefs(w, httptest.NewRequest(http.MethodGet, "/api/briefs", nil))
	if w.Code != http.StatusOK {
		d.t.Fatalf("the shelf opened with %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Briefs []struct {
			Answered int  `json:"answered"`
			Sent     bool `json:"sent"`
		} `json:"briefs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Briefs) != 1 {
		d.t.Fatalf("the shelf came back as %s (%v)", w.Body.String(), err)
	}
	return body.Briefs[0].Answered, body.Briefs[0].Sent
}

func publishedAt(id, at string) *chat.Brief {
	doc := sampleBrief()
	doc.ID, doc.At = id, at
	return doc
}

// A session takes its brief off the shelf and publishes one under the same
// name. The collector does both without the panel, and the new document is
// another brief: the answers the person began on the first turn up nowhere —
// not on its screen, not on the shelf, not in the text that would go into
// the session. A reissue of the new one keeps what was given to it.
func TestABriefRemovedAndPublishedAgainComesWithoutTheOldAnswersPG(t *testing.T) {
	desk, shelf := openBriefDesk(t, publishedAt("published-twice", "2026-09-08T03:00:00Z"))

	if code := desk.save(`{"r1":{"picks":["A"],"note":"lab first"},"r3":{"note":"k = 2"}}`); code != http.StatusOK {
		t.Fatalf("the first answers were saved with %d", code)
	}
	if answers, _ := desk.open(); len(answers) != 2 {
		t.Fatalf("the draft of the first publication came back as %+v", answers)
	}

	shelf.publish(publishedAt("published-twice", "2026-09-08T05:00:00Z"))

	answers, reply := desk.open()
	if len(answers) != 0 {
		t.Errorf("the brief published again opened with the answers of the removed one: %+v", answers)
	}
	if !strings.Contains(reply, "Answered 0 of 4") || strings.Contains(reply, "lab first") {
		t.Errorf("the text for the session carries the removed brief's answers:\n%s", reply)
	}
	if answered, sent := desk.card(); answered != 0 || sent {
		t.Errorf("the shelf counts %d answers (sent %v) on a brief nobody has answered", answered, sent)
	}

	if code := desk.save(`{"r2":{"skip":true}}`); code != http.StatusOK {
		t.Fatalf("an answer to the new brief was saved with %d", code)
	}
	reissue := publishedAt("published-twice", "2026-09-08T06:00:00Z")
	reissue.FirstAt = "2026-09-08T05:00:00Z"
	shelf.publish(reissue)
	answers, _ = desk.open()
	if len(answers) != 1 || !answers["r2"].Skip {
		t.Errorf("a reissue of the new brief came back with %+v, not the one answer given to it", answers)
	}
	if answered, _ := desk.card(); answered != 1 {
		t.Errorf("the shelf counts %d answers on the reissue, not the one given", answered)
	}
}

// The mark that a brief went into its session belongs to that publication
// too: the brief published again under its name is still to be answered, and
// a mark carried over would lock its fields and hide it from the session
// starting in the project.
func TestTheSentMarkOfARemovedBriefDoesNotCarryOverPG(t *testing.T) {
	desk, shelf := openBriefDesk(t, publishedAt("sent-then-published-again", "2026-09-08T03:00:00Z"))

	if code := desk.save(`{"r1":{"picks":["B"]}}`); code != http.StatusOK {
		t.Fatalf("the first answers were saved with %d", code)
	}
	if code := desk.markSent(); code != http.StatusOK {
		t.Fatalf("the first brief was marked sent with %d", code)
	}

	shelf.publish(publishedAt("sent-then-published-again", "2026-09-08T05:00:00Z"))

	if answered, sent := desk.card(); answered != 0 || sent {
		t.Errorf("the brief published again stands on the shelf with %d answers, sent %v", answered, sent)
	}
	if code := desk.markSent(); code != http.StatusOK {
		t.Fatalf("sending the new brief gave %d: the mark of the removed one held it", code)
	}
	if answers, _ := desk.open(); len(answers) != 0 {
		t.Errorf("the mark of the new brief took the removed one's answers with it: %+v", answers)
	}

	shelf.publish(publishedAt("sent-then-published-again", "2026-09-08T07:00:00Z"))
	if code := desk.save(`{"r1":{"picks":["A"]}}`); code != http.StatusOK {
		t.Errorf("an answer to a third publication gave %d: the sent mark of the second held it", code)
	}
	if answered, sent := desk.card(); answered != 1 || sent {
		t.Errorf("the third publication, answered once and never sent, stands with %d answers, sent %v", answered, sent)
	}
	if code := desk.save(`{"r1":{"picks":["B"]}}`); code != http.StatusOK {
		t.Errorf("a second answer to the third publication gave %d: the draft took the old mark on", code)
	}
}
