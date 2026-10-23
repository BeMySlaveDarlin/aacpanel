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
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func TestRestartCycleWaitsForTheTurnAndThenForReadiness(t *testing.T) {
	now := time.Unix(2000, 0)
	c := store.RestartCycle{Conversation: switchSID, CommandAt: now}
	row := restartSession{Status: "idle", StatusAt: now.Add(-time.Minute).UnixMilli()}
	step := nextRestart(c, row, now, now)
	if step.Kind != action.SessionSend || step.Text != restartCommand {
		t.Fatalf("command: %+v", step)
	}
	c.Commanded = true
	if step := nextRestart(c, row, now.Add(time.Second), now.Add(time.Second)); step.Kind != "" {
		t.Fatalf("the same idle state counts as a finished turn: %+v", step)
	}
	row.Status = "busy"
	row.StatusAt = now.Add(time.Second).UnixMilli()
	if step := nextRestart(c, row, now.Add(time.Second), now.Add(time.Second)); step.Kind != "" {
		t.Fatal(step)
	}
	row.Status = "idle"
	row.StatusAt = now.Add(2 * time.Second).UnixMilli()
	now = now.Add(3 * time.Second)
	step = nextRestart(c, row, now, now)
	if step.Kind != action.SessionSend || step.Text != restartReminder {
		t.Fatalf("reminder: %+v", step)
	}
	c.RemindedAt = &now
	if step := nextRestart(c, row, now, now.Add(5*time.Minute-time.Millisecond)); step.Kind != "" {
		t.Fatalf("the deadline arrived early: %+v", step)
	}
	row.Status = "busy"
	step = nextRestart(c, row, now, now.Add(5*time.Minute))
	if step.Kind != action.SessionRestart || !step.Anyway {
		t.Fatalf("forced restart: %+v", step)
	}
}

func TestRestartCycleSeesAStreamTurnOverBesideItsAgents(t *testing.T) {
	now := time.Unix(2000, 0)
	c := store.RestartCycle{CommandAt: now.Add(-time.Minute), Commanded: true}
	row := restartSession{Status: "busy", TurnOver: true, LastRequestAt: now.Format(time.RFC3339), Work: json.RawMessage(`{"agents":1}`)}
	step := nextRestart(c, row, now.Add(time.Second), now.Add(time.Second))
	if step.Kind != action.SessionSend || step.Text != restartReminder {
		t.Fatalf("no reminder after the stream's turn: %+v", step)
	}
	c.ReadyAt = &now
	step = nextRestart(c, row, now.Add(time.Second), now.Add(time.Second))
	if step.Kind != action.SessionSend || !strings.Contains(step.Text, "1 agent") {
		t.Fatalf("work reminder: %+v", step)
	}
}

func TestRestartCycleReadinessWaitsForFreshIdleAndAllWork(t *testing.T) {
	now := time.Unix(2000, 0)
	c := store.RestartCycle{CommandAt: now.Add(-time.Minute), Commanded: true, ReadyAt: &now, Intent: "Read the handoff"}
	row := restartSession{Status: "idle"}
	if step := nextRestart(c, row, now.Add(-time.Second), now); step.Kind != "" {
		t.Fatalf("stale idle: %+v", step)
	}
	row.Status = "busy"
	if step := nextRestart(c, row, now.Add(time.Second), now); step.Kind != "" {
		t.Fatalf("running turn: %+v", step)
	}
	row.Status = "idle"
	for _, work := range []string{`{"agents":1}`, `{"tasks":2,"wakes":1}`, `{"workflows":1}`, `{"observers":1}`} {
		row.Work = json.RawMessage(work)
		step := nextRestart(c, row, now.Add(time.Second), now)
		if step.Kind != action.SessionSend || !strings.Contains(step.Text, "at work") {
			t.Fatalf("%s: %+v", work, step)
		}
	}
	c.RemindedAt = &now
	step := nextRestart(c, row, now.Add(time.Second), now.Add(5*time.Minute))
	if step.Kind != action.SessionRestart || !step.Anyway || step.Text != c.Intent {
		t.Fatalf("busy deadline: %+v", step)
	}
	row.Work = json.RawMessage(`{"tasks":1,"wakes":1,"alarms":1}`)
	step = nextRestart(c, row, now.Add(time.Second), now.Add(time.Second))
	if step.Kind != action.SessionRestart || step.Anyway || step.Text != c.Intent {
		t.Fatalf("ready and clear: %+v", step)
	}
	row.Waiting = "dialog open"
	if step := nextRestart(c, row, now.Add(time.Second), now.Add(time.Second)); step.Kind != "" {
		t.Fatalf("waiting for permission: %+v", step)
	}
	row.Waiting = ""
	c.AttemptedAt = &now
	if step := nextRestart(c, row, now.Add(time.Second), now.Add(5*time.Minute-time.Millisecond)); step.Kind != "" {
		t.Fatalf("early retry: %+v", step)
	}
	if step := nextRestart(c, row, now.Add(time.Second), now.Add(5*time.Minute)); step.Kind != action.SessionRestart {
		t.Fatalf("retry: %+v", step)
	}
	c.Done = true
	if step := nextRestart(c, row, now.Add(time.Hour), now.Add(time.Hour)); step.Kind != "" {
		t.Fatalf("second cycle: %+v", step)
	}
}

func restartServer(t *testing.T) (*Server, *fakeExec, string) {
	t.Helper()
	srv, fake, dir := switchServer(t, "stream", "stream")
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM session_restarts"); err != nil {
		t.Fatal(err)
	}
	return srv, fake, dir
}

func restartSnapshot(t *testing.T, srv *Server, row restartSession, at time.Time) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"at": at.Unix(), "sessionsAt": at.Unix(), "sessions": []restartSession{row}})
	if err != nil {
		t.Fatal(err)
	}
	srv.host = host.NewReader(snapshotWith(t, string(raw)))
}

func readyPost(t *testing.T, srv *Server, local bool, intent string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"kind": action.SessionReady, "params": action.Ready{Conversation: switchSID, Intent: intent}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(string(body)))
	if local {
		r = r.WithContext(context.WithValue(r.Context(), localKey{}, true))
	}
	w := httptest.NewRecorder()
	srv.runAction(w, r, false)
	return w
}

func TestRestartCycleSurvivesServiceReplacementPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	list, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{LaunchSet: map[string]any{
		"autoRestart": true, "contextCap": 80, "restartIntent": "Project fallback",
	}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Millisecond)
	pct, known := 79.0, true
	row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"},
		Status: "idle", StatusAt: now.Add(-time.Minute).UnixMilli(), Pct: &pct, Limit: 1000000, LimitKnown: &known,
		Work: json.RawMessage(`{"observers":1}`)}
	restartSnapshot(t, srv, row, now)
	if w := readyPost(t, srv, true, ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "session_restart") {
		t.Fatalf("unasked readiness: %d %s", w.Code, w.Body.String())
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if req := <-fake.got; req.Kind != action.SessionSend || req.Text != restartCommand {
		t.Fatalf("command: %+v", req)
	}
	if w := readyPost(t, srv, false, "Next step"); w.Code != http.StatusForbidden {
		t.Fatalf("remote readiness: %d", w.Code)
	}
	if w := readyPost(t, srv, true, "/clear"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad intent: %d", w.Code)
	}
	if w := readyPost(t, srv, true, "Read the saved handoff"); w.Code != http.StatusOK {
		t.Fatalf("readiness: %d %s", w.Code, w.Body.String())
	}
	// A new service has no in-memory cycle state. The saved readiness must
	// win over the cap still present in the collector's row.
	srv = &Server{db: srv.db, exec: srv.exec, host: srv.host}
	row.Work = json.RawMessage(`{}`)
	row.StatusAt = now.Add(10 * time.Second).UnixMilli()
	restartSnapshot(t, srv, row, now.Add(11*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	req := <-fake.got
	if req.Kind != action.SessionRestart || req.Resume != "" || !req.AutoRestart || req.Conversation != switchSID ||
		req.Text != "Read the saved handoff" || req.Project == nil {
		t.Fatalf("fresh restart: %+v", req)
	}
	if err := srv.restartOnce(t.Context(), now.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-fake.got:
		t.Fatalf("a second cycle: %+v", req)
	default:
	}
	row.SessionID = "99999999-9999-4999-8999-999999999999"
	restartSnapshot(t, srv, row, now.Add(13*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(13*time.Second)); err != nil {
		t.Fatal(err)
	}
	if req := <-fake.got; req.Kind != action.SessionSend {
		t.Fatalf("new conversation: %+v", req)
	}
}

func TestRestartCycleEligibilityPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	list, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	pct, known := 79.0, true
	base := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir},
		Status: "idle", Pct: &pct, Limit: 1000000, LimitKnown: &known}
	restartSnapshot(t, srv, base, now)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{LaunchSet: map[string]any{"autoRestart": true}}); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*restartSession){
		func(r *restartSession) { r.Agent = "codex" },
		func(r *restartSession) { r.CWD += "-outside" },
		func(r *restartSession) { r.Status = "busy" },
		func(r *restartSession) { r.Waiting = "input needed" },
		func(r *restartSession) { r.Ask = json.RawMessage(`{"text":"choose"}`) },
		func(r *restartSession) { r.Permission = json.RawMessage(`{}`) },
		func(r *restartSession) { v := false; r.LimitKnown = &v },
		func(r *restartSession) { v := 78.9; r.Pct = &v },
		func(r *restartSession) { r.Limit = 0 },
	} {
		row := base
		edit(&row)
		restartSnapshot(t, srv, row, now)
		if err := srv.restartOnce(t.Context(), now); err != nil {
			t.Fatal(err)
		}
	}
	restartSnapshot(t, srv, base, now.Add(-time.Minute))
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-fake.got:
		t.Fatalf("ineligible command: %+v", req)
	default:
	}
}

func TestRestartCycleJournalsFailuresAndRetriesAfterFiveMinutesPG(t *testing.T) {
	srv, _, dir := restartServer(t)
	list, err := srv.db.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{LaunchSet: map[string]any{"autoRestart": true}}); err != nil {
		t.Fatal(err)
	}
	client, fake := startFakeExec(t, action.Response{OK: false, Error: "the old conversation is gone"})
	srv.exec = client
	now := time.Now().Truncate(time.Second)
	reminded := now.Add(-5 * time.Minute)
	c := store.RestartCycle{Conversation: switchSID, Session: "aacpanel-2", CommandAt: reminded.Add(-time.Minute), Commanded: true, RemindedAt: &reminded}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	row := restartSession{LiveSession: host.LiveSession{Name: c.Session, SessionID: switchSID, CWD: dir}, Status: "busy"}
	restartSnapshot(t, srv, row, now)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	req := <-fake.got
	if req.Kind != action.SessionRestart || !req.Anyway || !req.AutoRestart || req.Conversation != switchSID || req.Resume != "" || req.Text != "" {
		t.Fatalf("forced restart: %+v", req)
	}
	// Replacement of the service also keeps the failed attempt's retry delay.
	srv = &Server{db: srv.db, host: srv.host, exec: srv.exec}
	restartSnapshot(t, srv, row, now.Add(5*time.Minute-time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(5*time.Minute-time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-fake.got:
		t.Fatalf("early retry: %+v", req)
	default:
	}
	restartSnapshot(t, srv, row, now.Add(5*time.Minute))
	if err := srv.restartOnce(t.Context(), now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if req := <-fake.got; req.Kind != action.SessionRestart {
		t.Fatal(req)
	}
	rows, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 2})
	if err != nil || len(rows) != 2 {
		t.Fatalf("journal: %v %v", rows, err)
	}
	for _, row := range rows {
		if row.Result != store.ActionFailed || row.Error != "the old conversation is gone" || row.Params["anyway"] != true {
			t.Fatalf("failure missing from journal: %+v", row)
		}
	}
}
