package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func decisionBrief(t *testing.T) *chat.Brief {
	t.Helper()
	doc := &chat.Brief{
		ID: "rollout-decisions", SessionID: "11111111-1111-4111-8111-111111111111",
		CWD: "/srv/proj", Title: "Prepare the rollout", Task: "Choose the rollout order.", Why: "The release is ready.",
		At: "2026-09-15T15:26:37Z",
		Decisions: []chat.BriefDecision{
			{ID: "order", Title: "Rollout order: two steps", Options: []chat.BriefOption{
				{Key: "A", Label: "Migration first", Reason: "The code needs it", Cost: "One rebuild", Hours: 0.5, Recommended: true},
				{Key: "B", Label: "Code first", Reason: "Earlier deployment", Cost: "Temporary downtime", Hours: 1.2}}},
			{ID: "checks", Title: "Checks", Multi: true, Options: []chat.BriefOption{
				{Key: "A", Label: "API", Reason: "Check routes", Cost: "One run", Hours: 0.1, Recommended: true},
				{Key: "B", Label: "Browser", Reason: "Check screen", Cost: "One run", Hours: 0.2, Recommended: true}}},
			{ID: "later", Title: "Later work", Options: []chat.BriefOption{
				{Key: "A", Label: "Wait", Recommended: true}, {Key: "B", Label: "Continue"}}},
		},
	}
	return doc
}

func TestDecisionDraftChecksItsOwnOptions(t *testing.T) {
	doc := decisionBrief(t)
	for name, raw := range map[string]store.BriefDecisions{
		"foreign decision": {Decisions: map[string]store.BriefAnswer{"unknown": {Picks: []string{"A"}}}},
		"foreign key":      {Decisions: map[string]store.BriefAnswer{"order": {Picks: []string{"Z"}}}},
		"single choice":    {Decisions: map[string]store.BriefAnswer{"order": {Picks: []string{"A", "B"}}}},
		"duplicate key":    {Decisions: map[string]store.BriefAnswer{"checks": {Picks: []string{"A", "A"}}}},
		"skip":             {Decisions: map[string]store.BriefAnswer{"order": {Skip: true}}},
		"long choice note": {Decisions: map[string]store.BriefAnswer{"order": {Note: strings.Repeat("x", 2001)}}},
		"long task note":   {Note: strings.Repeat("x", 4001)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cleanBriefDecisions(doc, raw); err == nil {
				t.Fatal("an invalid draft was accepted")
			}
		})
	}
	valid := store.BriefDecisions{Decisions: map[string]store.BriefAnswer{"checks": {Picks: []string{"A", "B"}, Note: strings.Repeat("é", 2000)}}, Note: strings.Repeat("é", 4000)}
	if _, err := cleanBriefDecisions(doc, valid); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionReplyNamesChoicesRecommendationsAndUnanswered(t *testing.T) {
	doc := decisionBrief(t)
	draft := store.BriefDraft{DecisionAnswers: &store.BriefDecisions{
		Decisions: map[string]store.BriefAnswer{"order": {Picks: []string{"B"}, Note: "Use the maintenance window."}, "checks": {Picks: []string{"B", "A"}}},
		Note:      "Proceed after review.",
	}}
	want := "The person decided brief \"Prepare the rollout\" (rollout-decisions): 2 of 3 decided.\n" +
		"1. Rollout order: two steps\n   picked: Code first (changed; recommended: Migration first)\n" +
		"   note: Use the maintenance window.\n" +
		"2. Checks\n   picked: API, Browser (recommended)\n" +
		"3. Later work\n   no answer\nNote: Proceed after review.\n"
	if got := briefReply(doc, draft); got != want {
		t.Fatalf("reply:\n%s\nwant:\n%s", got, want)
	}
	first := briefMessageID(doc)
	doc.FirstAt, doc.At = doc.At, "2026-09-16T00:00:00Z"
	if got := briefMessageID(doc); got != first {
		t.Fatalf("reissue changed the message identity: %s, %s", first, got)
	}
	doc.FirstAt = ""
	if briefMessageID(doc) == first {
		t.Fatal("another publication reused the message identity")
	}
}

func TestDecisionBriefSendsSavedDraftAndClosesItPG(t *testing.T) {
	doc := decisionBrief(t)
	desk, _ := openBriefDesk(t, doc)
	t.Cleanup(func() { desk.srv.db.DropBriefDraft(t.Context(), doc.ID) })
	desk.srv.host = host.NewReader(snapshotWith(t, `{"sessions":[{"session":"author","sessionId":"`+doc.SessionID+`","cwd":"/srv/proj"}]}`))
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "queued"})
	desk.srv.exec = client
	w := desk.request(http.MethodPost, "", desk.srv.apiBriefSend)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty send: %d %s", w.Code, w.Body.String())
	}
	if code := desk.save(`{"decisions":{"order":{"picks":["B"],"note":"Window"},"checks":{"picks":["A","B"]}},"note":"Ready"}`); code != http.StatusOK {
		t.Fatalf("save: %d", code)
	}
	if answered, sent := desk.card(); answered != 2 || sent {
		t.Fatalf("progress before sending: %d, %v", answered, sent)
	}
	w = desk.request(http.MethodPost, `{"text":"forged"}`, desk.srv.apiBriefSend)
	if w.Code != http.StatusOK {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Draft store.BriefDraft `json:"draft"`
		Reply string           `json:"reply"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Draft.SentAt == nil || result.Draft.DecisionAnswers == nil || result.Draft.Decided() != 2 {
		t.Fatalf("sent result: %+v", result.Draft)
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionSend || got.Target != "author" || got.Text != briefReply(doc, result.Draft) || got.MessageID != briefMessageID(doc) {
			t.Fatalf("delivery: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("decisions never reached the executor")
	}
	draft, err := desk.srv.db.BriefDraftOf(t.Context(), doc.ID, doc.Born())
	if err != nil || draft.SentAt == nil || draft.DecisionAnswers == nil {
		t.Fatalf("mark and answers: %+v, %v", draft, err)
	}
	if answered, sent := desk.card(); answered != 2 || !sent {
		t.Fatalf("sent progress: %d, %v", answered, sent)
	}
	if w := desk.request(http.MethodPost, "", desk.srv.apiBriefSend); w.Code != http.StatusConflict {
		t.Fatalf("second send: %d %s", w.Code, w.Body.String())
	}
	if code := desk.save(`{"decisions":{"order":{"picks":["A"]}}}`); code != http.StatusConflict {
		t.Fatalf("save after send: %d", code)
	}
	if err := desk.srv.db.SaveBriefDecisions(t.Context(), doc.ID, doc.Born(), store.BriefDecisions{Decisions: map[string]store.BriefAnswer{}}); err != store.ErrBriefSent {
		t.Fatalf("store accepted a replacement for a closed draft: %v", err)
	}
	select {
	case got := <-fake.got:
		t.Fatalf("duplicate delivery: %+v", got)
	default:
	}
}

func TestDecisionBriefRefusalsKeepDraftOpenPG(t *testing.T) {
	doc := decisionBrief(t)
	doc.ID = "refused-decisions"
	desk, _ := openBriefDesk(t, doc)
	t.Cleanup(func() { desk.srv.db.DropBriefDraft(t.Context(), doc.ID) })
	for _, body := range []string{
		`{"decisions":{"order":{"picks":["Z"]}}}`,
		`{"decisions":{"order":{"picks":["A","B"]}}}`,
	} {
		if code := desk.save(body); code != http.StatusBadRequest {
			t.Fatalf("invalid draft: %d", code)
		}
	}
	if code := desk.save(`{"decisions":{"order":{"note":"Thinking"}}}`); code != http.StatusOK {
		t.Fatal(code)
	}
	if w := desk.request(http.MethodPost, "", desk.srv.apiBriefSend); w.Code != http.StatusBadRequest {
		t.Fatalf("note counted as choice: %d", w.Code)
	}
	if code := desk.save(`{"decisions":{"order":{"picks":["A"]}}}`); code != http.StatusOK {
		t.Fatal(code)
	}
	if code := desk.markSent(); code != http.StatusBadRequest {
		t.Fatalf("decisions marked without delivery: %d", code)
	}
	desk.srv.host = host.NewReader(snapshotWith(t, `{"sessions":[{"session":"elsewhere","cwd":"/srv/elsewhere"}]}`))
	if w := desk.request(http.MethodPost, "", desk.srv.apiBriefSend); w.Code != http.StatusConflict {
		t.Fatalf("no live target: %d %s", w.Code, w.Body.String())
	}
	desk.srv.host = host.NewReader(snapshotWith(t, `{"sessions":[{"session":"replacement","cwd":"/srv/proj"}]}`))
	client, fake := startFakeExec(t, action.Response{OK: false, Error: "session unavailable"})
	desk.srv.exec = client
	if w := desk.request(http.MethodPost, "", desk.srv.apiBriefSend); w.Code == http.StatusOK {
		t.Fatal("failed delivery was accepted")
	}
	got := <-fake.got
	if got.Target != "replacement" || got.MessageID != briefMessageID(doc) {
		t.Fatalf("fallback delivery: %+v", got)
	}
	draft, err := desk.srv.db.BriefDraftOf(t.Context(), doc.ID, doc.Born())
	if err != nil || draft.SentAt != nil || draft.Decided() != 1 {
		t.Fatalf("failed send changed draft: %+v %v", draft, err)
	}
	client, fake = startFakeExec(t, action.Response{OK: true})
	desk.srv.exec = client
	if w := desk.request(http.MethodPost, "", desk.srv.apiBriefSend); w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	retry := <-fake.got
	if retry.MessageID != got.MessageID {
		t.Fatal("retry changed message identity")
	}
}

func TestDecisionBriefWaitingCountsDecisions(t *testing.T) {
	briefs := []chat.BriefCard{{ID: "decisions", Decisions: 3}, {ID: "settled", Decisions: 2}}
	got := waitingFor(nil, nil, briefs, map[string]bool{"settled": true})
	if len(got) != 1 || got[0].ID != "decisions" {
		t.Fatalf("waiting: %+v", got)
	}
}
