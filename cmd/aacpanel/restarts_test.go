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
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

func TestRestartCycleWaitsForTheTurnAndThenForReadiness(t *testing.T) {
	now := time.Unix(2000, 0)
	c := store.RestartCycle{Conversation: switchSID, CommandAt: now}
	row := restartSession{Status: "idle", StatusAt: now.Add(-time.Minute).UnixMilli()}
	step := nextRestart(c, row, now, now)
	if step.Kind != action.SessionSend || step.Text != restartMessage(restartCommand) {
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
	if step.Kind != action.SessionSend || step.Text != restartMessage(restartReminder) {
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
	if step.Kind != action.SessionSend || step.Text != restartMessage(restartReminder) {
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

func TestRestartMessagesArePanelPrompts(t *testing.T) {
	now := time.Unix(2000, 0)
	for _, tc := range []struct {
		name  string
		cycle store.RestartCycle
		work  string
		text  string
	}{
		{name: "command", text: restartCommand},
		{name: "readiness reminder", cycle: store.RestartCycle{Commanded: true, CommandAt: now.Add(-time.Minute)}, text: restartReminder},
		{name: "work reminder", cycle: store.RestartCycle{Commanded: true, ReadyAt: &now}, work: `{"agents":1}`, text: restartWorkReminder("1 agent")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := restartSession{Status: "idle", StatusAt: now.UnixMilli(), Work: json.RawMessage(tc.work)}
			step := nextRestart(tc.cycle, row, now.Add(time.Second), now.Add(time.Second))
			want := "<panel-message from=\"context restart\">\n" + tc.text + "\n</panel-message>"
			if step.Kind != action.SessionSend || step.Text != want {
				t.Fatalf("the instruction is not a panel prompt: %+v", step)
			}
			req := action.Request{ID: "restart-message", Kind: step.Kind, Target: "aacpanel", Text: step.Text}
			if err := req.Validate(); err != nil {
				t.Fatalf("session.send rejects the wrapped instruction: %v", err)
			}
		})
	}
}

func restartServer(t *testing.T) (*Server, *fakeExec, string) {
	t.Helper()
	srv, fake, dir := switchServer(t, "stream", "stream")
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM session_restarts; DELETE FROM session_finalizations"); err != nil {
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
	return readyConversationPost(t, srv, local, switchSID, intent)
}

func readyConversationPost(t *testing.T, srv *Server, local bool, conversation, intent string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"kind": action.SessionReady, "params": action.Ready{Conversation: conversation, Intent: intent}})
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
	if req := <-fake.got; req.Kind != action.SessionSend || req.Text != restartMessage(restartCommand) {
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

func TestRestartCycleStartsNearTheCapAfterIdlePG(t *testing.T) {
	for _, agent := range []string{schema.AgentClaude, schema.AgentCodex} {
		t.Run(agent, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				pct     float64
				idle    time.Duration
				edit    func(*restartSession)
				restart bool
			}{
				{"ten minutes at 95 percent of cap", 66.5, 10 * time.Minute, nil, true},
				{"longer than ten minutes", 66.5, 11 * time.Minute, nil, true},
				{"nine minutes", 66.5, 9 * time.Minute, nil, false},
				{"just before ten minutes", 66.5, 10*time.Minute - time.Millisecond, nil, false},
				{"94 percent of cap", 65.8, 10 * time.Minute, nil, false},
				{"cap minus margin at turn end", 69, 0, nil, true},
				{"under cap minus margin at turn end", 68.9, 0, nil, false},
				{"running turn", 66.5, 10 * time.Minute, func(r *restartSession) { r.Status = "busy" }, false},
				{"turn over beside an agent", 66.5, 10 * time.Minute, func(r *restartSession) {
					r.Status, r.TurnOver, r.Work = "busy", true, json.RawMessage(`{"agents":1}`)
				}, true},
				{"waiting", 66.5, 10 * time.Minute, func(r *restartSession) { r.Waiting = "input needed" }, false},
				{"question", 66.5, 10 * time.Minute, func(r *restartSession) { r.Ask = json.RawMessage(`{"text":"choose"}`) }, false},
				{"permission", 66.5, 10 * time.Minute, func(r *restartSession) { r.Permission = json.RawMessage(`{}`) }, false},
				{"unknown window", 66.5, 10 * time.Minute, func(r *restartSession) { v := false; r.LimitKnown = &v }, false},
				{"missing window", 66.5, 10 * time.Minute, func(r *restartSession) { r.Limit = 0 }, false},
				{"missing context", 66.5, 10 * time.Minute, func(r *restartSession) { r.Pct = nil }, false},
				{"missing status time", 66.5, 10 * time.Minute, func(r *restartSession) { r.StatusAt = 0 }, false},
				{"future status time", 66.5, -time.Minute, nil, false},
				{"paused conversation", 66.5, 10 * time.Minute, nil, false},
				{"pause alarm", 66.5, 10 * time.Minute, func(r *restartSession) { r.Pause = &pauseAlarm{ID: "pause", Name: action.PauseNoEndAlarm} }, false},
				{"automatic restart disabled", 66.5, 10 * time.Minute, nil, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv, fake, dir := restartServer(t)
					profiles, err := srv.db.Profiles(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					if _, err := srv.db.UpdateProfile(t.Context(), profiles[0].ID, store.ProfileEdit{LaunchSet: map[string]any{
						"agent": agent, "autoRestart": tc.name != "automatic restart disabled", "contextCap": 70,
					}}); err != nil {
						t.Fatal(err)
					}
					now := time.Now().Truncate(time.Millisecond)
					known := true
					row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir,
						Agent: agent, Transport: "stream"}, Status: "idle", StatusAt: now.Add(-tc.idle).UnixMilli(),
						Pct: &tc.pct, Limit: 1000000, LimitKnown: &known}
					if tc.edit != nil {
						tc.edit(&row)
					}
					if tc.name == "paused conversation" {
						keepRestartPause(t, srv, row, now)
					}
					restartSnapshot(t, srv, row, now)
					if err := srv.restartOnce(t.Context(), now); err != nil {
						t.Fatal(err)
					}
					cycles, err := srv.db.RestartCycles(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					if !tc.restart {
						noRestartRequest(t, fake)
						if len(cycles) != 0 {
							t.Fatalf("an ineligible session entered the cycle: %+v", cycles)
						}
						return
					}
					if req := restartRequest(t, fake); req.Kind != action.SessionSend || req.Text != restartMessage(restartCommand) {
						t.Fatalf("save command: %+v", req)
					}
					if len(cycles) != 1 || !cycles[0].Commanded || cycles[0].Done || cycles[0].Conversation != row.SessionID || !cycles[0].CommandAt.Equal(now) {
						t.Fatalf("the save command did not enter the readiness cycle: %+v", cycles)
					}
					if err := srv.restartOnce(t.Context(), now); err != nil {
						t.Fatal(err)
					}
					noRestartRequest(t, fake)
				})
			}
		})
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

func TestManualRestartWaitsForReadinessWithoutTheCapPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	nextRequest := func() action.Request {
		t.Helper()
		deadline := time.After(time.Second)
		for {
			select {
			case req := <-fake.got:
				// The host snapshot also reads the queue; only the cycle's
				// action advances the preparation checked here.
				if req.Ask == action.AskHeavy {
					continue
				}
				return req
			case <-deadline:
				t.Fatal("the manual cycle sent no action")
				return action.Request{}
			}
		}
	}
	now := time.Now().Truncate(time.Second)
	row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"}, Status: "idle"}
	restartSnapshot(t, srv, row, now)
	w := post(t, srv, `{"kind":"session.restart","target":"aacpanel-2","params":{"prepare":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("manual restart: %d %s", w.Code, w.Body.String())
	}
	cycles, err := srv.db.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || !cycles[0].Manual {
		t.Fatalf("manual cycle: %+v %v", cycles, err)
	}
	raw, err := srv.hostSnapshot(t.Context())
	if err != nil || !strings.Contains(string(raw), `"restarting":true`) {
		t.Fatalf("snapshot lost the cycle: %s %v", raw, err)
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	command := nextRequest()
	if command.Kind != action.SessionSend || !strings.Contains(command.Text, "The person has asked") || strings.Contains(command.Text, "reached its project's context cap") {
		t.Fatalf("manual command: %+v", command)
	}
	if w := readyPost(t, srv, true, ""); w.Code != http.StatusOK {
		t.Fatalf("readiness: %d %s", w.Code, w.Body.String())
	}
	// A repeated press keeps the readiness already given.
	if w := post(t, srv, `{"kind":"session.restart","target":"aacpanel-2","params":{"prepare":true}}`); w.Code != http.StatusOK {
		t.Fatalf("repeat: %d", w.Code)
	}
	restartSnapshot(t, srv, row, now.Add(3*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	req := nextRequest()
	if req.Kind != action.SessionRestart || !req.AutoRestart || req.Resume != "" || req.Project == nil {
		t.Fatalf("prepared restart: %+v", req)
	}
	row.Agent = "codex"
	restartSnapshot(t, srv, row, now)
	if w := post(t, srv, `{"kind":"session.restart","target":"aacpanel-2","params":{"prepare":true}}`); w.Code == http.StatusOK {
		t.Fatal("codex entered a preparation cycle")
	}
}

// The card says where the cycle stands: saving from the command on, forcing
// from the reminder on with the moment of the forced restart, and nothing
// before the command or after the restart.
func TestRestartPhaseReachesTheRowPG(t *testing.T) {
	srv, _, dir := restartServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"}, Status: "idle"}
	restartSnapshot(t, srv, row, now)
	phase := func() string {
		t.Helper()
		raw, err := srv.hostSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var snap struct {
			Sessions []struct {
				Phase json.RawMessage `json:"restartPhase"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(raw, &snap); err != nil || len(snap.Sessions) != 1 {
			t.Fatalf("snapshot: %s %v", raw, err)
		}
		return string(snap.Sessions[0].Phase)
	}
	cycle := store.RestartCycle{Conversation: switchSID, Session: "aacpanel-2", CommandAt: now}
	if err := srv.db.KeepRestartCycle(t.Context(), cycle); err != nil {
		t.Fatal(err)
	}
	if got := phase(); got != "" {
		t.Fatalf("a cycle before its command has the phase %s", got)
	}
	cycle.Commanded = true
	if err := srv.db.KeepRestartCycle(t.Context(), cycle); err != nil {
		t.Fatal(err)
	}
	if got := phase(); got != `{"phase":"saving"}` {
		t.Fatalf("a commanded cycle: %s", got)
	}
	reminded := now.Add(time.Minute)
	cycle.RemindedAt = &reminded
	if err := srv.db.KeepRestartCycle(t.Context(), cycle); err != nil {
		t.Fatal(err)
	}
	want := `{"phase":"forcing","forceAt":"` + reminded.Add(restartGrace).Format(time.RFC3339) + `"}`
	if got := phase(); got != want {
		t.Fatalf("a reminded cycle: %s, want %s", got, want)
	}
	attempted := reminded.Add(restartGrace + time.Second)
	cycle.AttemptedAt = &attempted
	if err := srv.db.KeepRestartCycle(t.Context(), cycle); err != nil {
		t.Fatal(err)
	}
	want = `{"phase":"forcing","forceAt":"` + attempted.Add(restartGrace).Format(time.RFC3339) + `"}`
	if got := phase(); got != want {
		t.Fatalf("a cycle whose forced restart failed: %s, want %s", got, want)
	}
	cycle.AttemptedAt = nil
	cycle.Done = true
	if err := srv.db.KeepRestartCycle(t.Context(), cycle); err != nil {
		t.Fatal(err)
	}
	if got := phase(); got != "" {
		t.Fatalf("a finished cycle has the phase %s", got)
	}
}
