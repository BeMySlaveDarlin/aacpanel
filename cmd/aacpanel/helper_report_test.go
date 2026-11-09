package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/session"
)

func TestACodexHelperReportWakesItsSleepingParentAndLiftsThePausePG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.srv.runAction(w, r.WithContext(context.WithValue(r.Context(), localKey{}, true)), true)
	}))
	t.Cleanup(panel.Close)
	report := action.HelperReport{Thread: s.row(9).Conversation, Parent: p.Conversation}
	h := session.Host{Panel: panel.URL}
	before := len(s.requests)
	const body = "The Codex helper's complete final report"
	if err := h.Report(t.Context(), report, body); err != nil {
		t.Fatal(err)
	}
	s.drain()
	s.publish()
	sent := s.requests[before:]
	if len(sent) != 2 || sent[0].Kind != action.SessionThaw || sent[0].Conversation != p.Conversation || s.frozen[p.Conversation] {
		t.Fatalf("report wake-up: %+v", sent)
	}
	delivered := sent[1]
	if delivered.Kind != action.SessionLetter || delivered.Target != p.Session || delivered.Text != body || delivered.Report == nil || *delivered.Report != report || delivered.Conversation != p.Conversation || delivered.From != "" {
		t.Fatalf("report delivery: %+v", delivered)
	}
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("the report left its parent paused: %+v %v", rows, err)
	}
	pool, _ := s.srv.db.Pool()
	var params json.RawMessage
	if err := pool.QueryRow(t.Context(), `SELECT params FROM actions WHERE kind='session.letter' AND device_name='helper reports' ORDER BY id DESC LIMIT 1`).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(params), body) || !strings.Contains(string(params), report.Thread) {
		t.Fatalf("the report journal lost its identity or kept private text: %s", params)
	}
	// A reused display name cannot turn a deliberate close into a wake.
	s.snap.Sessions = []livingRow{{LiveSession: host.LiveSession{Name: p.Session, SessionID: s.row(10).Conversation, CWD: s.dir}}}
	s.publish()
	before = len(s.requests)
	if err := h.Report(t.Context(), report, "A report for the closed conversation"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("a closed parent accepted a report: %v", err)
	}
	s.drain()
	if len(s.requests) != before {
		t.Fatalf("a closed or reused parent was acted on: %+v", s.requests[before:])
	}
}

func TestHelperReportIngressRefusesPublicAndUnboundRequestsPG(t *testing.T) {
	s, p := pausedScene(t, true)
	p = sleepTurn(t, s, p)
	body := fmt.Sprintf(`{"kind":"session.letter","params":{"report":{"thread":%q,"parent":%q},"text":"Report"}}`, s.row(9).Conversation, p.Conversation)
	if w := post(t, s.srv, body); w.Code != http.StatusForbidden {
		t.Fatalf("a public helper report: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(strings.Replace(body, s.row(9).Conversation, "invalid-thread", 1)))
	r = r.WithContext(context.WithValue(r.Context(), localKey{}, true))
	w := httptest.NewRecorder()
	s.srv.runAction(w, r, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an unbound helper report: %d %s", w.Code, w.Body.String())
	}
	rows, err := s.srv.sessionPauses(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Sleeping {
		t.Fatalf("a refused report changed the pause: %+v %v", rows, err)
	}
}
