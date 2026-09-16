package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	done, err := db.BriefProgress(t.Context(), []string{"seven-after-twelve", "never-touched"})
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
	draft, err := db.BriefDraftOf(t.Context(), "seven-after-twelve")
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

	draft, err := db.BriefDraftOf(t.Context(), "settled-after-sending")
	if err != nil {
		t.Fatal(err)
	}
	if picks := draft.Answers["r1"].Picks; len(picks) != 1 || picks[0] != "A" {
		t.Errorf("the answers the session was sent were changed afterwards: %+v", draft.Answers)
	}
}
