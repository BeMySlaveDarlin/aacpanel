package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func preparationRequest(t *testing.T, fake *fakeExec) action.Request {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case req := <-fake.got:
			if req.Ask == action.AskHeavy {
				continue
			}
			return req
		case <-deadline:
			t.Fatal("the preparation sent no action")
			return action.Request{}
		}
	}
}

func noPreparationAction(t *testing.T, fake *fakeExec) {
	t.Helper()
	for {
		select {
		case req := <-fake.got:
			if req.Ask != "" {
				continue
			}
			t.Fatalf("an unexpected action reached the executor: %+v", req)
		default:
			return
		}
	}
}

func finalizedRow(t *testing.T, srv *Server) map[string]json.RawMessage {
	t.Helper()
	raw, err := srv.hostSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil || len(snap.Sessions) != 1 {
		t.Fatalf("snapshot: %s %v", raw, err)
	}
	return snap.Sessions[0]
}

func TestPreparedCloseWaitsForReadinessAndTheTurnPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"},
		Status: "idle", LastTurnAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}
	restartSnapshot(t, srv, row, now)
	if w := post(t, srv, `{"kind":"session.close","target":"aacpanel-2","params":{"prepare":true}}`); w.Code != http.StatusOK {
		t.Fatalf("close: %d %s", w.Code, w.Body.String())
	}
	noPreparationAction(t, fake)
	cycles, err := srv.db.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || cycles[0].Finish != store.PrepareClose {
		t.Fatalf("cycle: %+v %v", cycles, err)
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	command := preparationRequest(t, fake)
	if command.Kind != action.SessionSend || command.Text != preparationMessage(cycles[0], closeCommand) {
		t.Fatalf("command: %+v", command)
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	noPreparationAction(t, fake)
	if w := readyPost(t, srv, true, ""); w.Code != http.StatusOK {
		t.Fatalf("ready: %d %s", w.Code, w.Body.String())
	}
	// The tool result and the final model request still belong to the saving turn.
	row.Status, row.LastRequestAt = "busy", now.Add(time.Second).Format(time.RFC3339Nano)
	restartSnapshot(t, srv, row, now.Add(2*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	noPreparationAction(t, fake)
	row.Status = "idle"
	restartSnapshot(t, srv, row, now.Add(3*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	req := preparationRequest(t, fake)
	if req.Kind != action.SessionClose || req.Conversation != switchSID || req.Text != "" {
		t.Fatalf("close after readiness: %+v", req)
	}
	if finalizedRow(t, srv)["finalizedAt"] == nil {
		t.Fatal("accepted readiness lost its finalization mark")
	}
}

func TestWrapUpKeepsItsMarkThroughTheReadyTurnAndCloseSkipsSavingPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"},
		Status: "idle", LastTurnAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}
	restartSnapshot(t, srv, row, now)
	if w := post(t, srv, `{"kind":"session.finalize","target":"aacpanel-2"}`); w.Code != http.StatusOK {
		t.Fatalf("wrap up: %d %s", w.Code, w.Body.String())
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	req := preparationRequest(t, fake)
	if req.Kind != action.SessionSend || req.Text != action.PanelMessage("session wrap up", finalizeCommand) {
		t.Fatalf("wrap up command: %+v", req)
	}
	if w := readyPost(t, srv, true, ""); w.Code != http.StatusOK {
		t.Fatalf("ready: %d %s", w.Code, w.Body.String())
	}
	row.LastRequestAt = now.Add(2 * time.Second).Format(time.RFC3339Nano)
	restartSnapshot(t, srv, row, now.Add(3*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	noPreparationAction(t, fake)
	marked := finalizedRow(t, srv)
	if marked["finalizedAt"] == nil || string(marked["prepareAvailable"]) != "true" || marked["restartPhase"] != nil {
		t.Fatalf("wrap up did not settle as finalized: %+v", marked)
	}
	cycles, err := srv.db.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || !cycles[0].Done {
		t.Fatalf("wrap up cycle: %+v %v", cycles, err)
	}
	if w := readyPost(t, srv, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("readiness outside a cycle: %d", w.Code)
	}
	if w := post(t, srv, `{"kind":"session.close","target":"aacpanel-2","params":{"prepare":true}}`); w.Code != http.StatusOK {
		t.Fatalf("saved close: %d %s", w.Code, w.Body.String())
	}
	req = preparationRequest(t, fake)
	if req.Kind != action.SessionClose || req.Conversation != switchSID {
		t.Fatalf("saved close asked to save again: %+v", req)
	}
	// A later input revokes the persisted mark even when the whole next turn
	// fits between collector reads. Replacing the service cannot restore it.
	row.LastTurnAt, row.LastRequestAt = now.Add(4*time.Second).Format(time.RFC3339Nano), now.Add(5*time.Second).Format(time.RFC3339Nano)
	restartSnapshot(t, srv, row, now.Add(6*time.Second))
	srv = &Server{db: srv.db, host: srv.host, exec: srv.exec, hostName: srv.hostName}
	if marked := finalizedRow(t, srv); marked["finalizedAt"] != nil {
		t.Fatalf("new input kept finalization: %+v", marked)
	}
	if w := post(t, srv, `{"kind":"session.close","target":"aacpanel-2","params":{"prepare":true}}`); w.Code != http.StatusOK {
		t.Fatalf("new turn close: %d %s", w.Code, w.Body.String())
	}
	noPreparationAction(t, fake)
	if err := srv.restartOnce(t.Context(), now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if req := preparationRequest(t, fake); req.Kind != action.SessionSend || !strings.Contains(req.Text, closeCommand) {
		t.Fatalf("new input bypassed saving: %+v", req)
	}
}

func TestPreparedCloseRemindsThenForcesAndWrapUpNeverCloses(t *testing.T) {
	now := time.Unix(2000, 0)
	c := store.RestartCycle{Finish: store.PrepareClose, Manual: true, Commanded: true, CommandAt: now.Add(-time.Minute)}
	row := restartSession{Status: "idle", StatusAt: now.UnixMilli()}
	step := nextRestart(c, row, now, now)
	if step.Kind != action.SessionSend || !strings.Contains(step.Text, "will close this session") {
		t.Fatalf("reminder: %+v", step)
	}
	c.RemindedAt = &now
	row.Status = "busy"
	if step := nextRestart(c, row, now, now.Add(restartGrace-time.Millisecond)); step.Kind != "" {
		t.Fatalf("early close: %+v", step)
	}
	if step := nextRestart(c, row, now, now.Add(restartGrace)); step.Kind != action.SessionClose || !step.Anyway {
		t.Fatalf("forced close: %+v", step)
	}
	c.Finish = store.PrepareFinalize
	if step := nextRestart(c, row, now, now.Add(2*restartGrace)); step.Kind != "" {
		t.Fatalf("wrap up ended a session without readiness: %+v", step)
	}
}

func TestPreparedCloseFallsBackWithoutReadinessAccessPG(t *testing.T) {
	for _, mode := range []string{"codex", "outside", "unmapped", "without tools", "immediate"} {
		t.Run(mode, func(t *testing.T) {
			srv, fake, dir := restartServer(t)
			row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"}, Status: "idle"}
			params := `{"prepare":true}`
			switch mode {
			case "codex":
				row.Agent = "codex"
			case "outside":
				row.Outside = true
			case "unmapped":
				row.CWD = "/srv/unmapped"
			case "without tools":
				list, err := srv.db.Profiles(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{LaunchSet: map[string]any{"panelTools": false}}); err != nil {
					t.Fatal(err)
				}
			case "immediate":
				params = `{"prepare":false}`
			}
			restartSnapshot(t, srv, row, time.Now())
			if w := post(t, srv, `{"kind":"session.close","target":"aacpanel-2","params":`+params+`}`); w.Code != http.StatusOK {
				t.Fatalf("close: %d %s", w.Code, w.Body.String())
			}
			if req := preparationRequest(t, fake); req.Kind != action.SessionClose {
				t.Fatalf("fallback: %+v", req)
			}
			cycles, err := srv.db.RestartCycles(t.Context())
			if err != nil || len(cycles) != 0 {
				t.Fatalf("fallback made a cycle: %+v %v", cycles, err)
			}
		})
	}
}

func TestPreparedCloseForcesThroughTheOrdinaryExecutorPathPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := restartSession{LiveSession: host.LiveSession{Name: "aacpanel-2", SessionID: switchSID, CWD: dir, Transport: "stream"}, Status: "idle"}
	restartSnapshot(t, srv, row, now)
	if w := post(t, srv, `{"kind":"session.close","target":"aacpanel-2","params":{"prepare":true}}`); w.Code != http.StatusOK {
		t.Fatalf("close: %d %s", w.Code, w.Body.String())
	}
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if req := preparationRequest(t, fake); req.Kind != action.SessionSend {
		t.Fatal(req)
	}
	row.StatusAt = now.Add(time.Second).UnixMilli()
	restartSnapshot(t, srv, row, now.Add(2*time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if req := preparationRequest(t, fake); req.Kind != action.SessionSend || !strings.Contains(req.Text, "five minutes after this reminder") {
		t.Fatalf("reminder: %+v", req)
	}
	cycles, err := srv.db.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || cycles[0].RemindedAt == nil {
		t.Fatalf("reminder state: %+v %v", cycles, err)
	}
	deadline := cycles[0].RemindedAt.Add(restartGrace + time.Second)
	row.Status, row.Work = "busy", json.RawMessage(`{"agents":1}`)
	restartSnapshot(t, srv, row, deadline)
	if err := srv.restartOnce(t.Context(), deadline); err != nil {
		t.Fatal(err)
	}
	if req := preparationRequest(t, fake); req.Kind != action.SessionClose || req.Conversation != switchSID {
		t.Fatalf("forced close: %+v", req)
	}
	rows, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range rows {
		if a.Kind == string(action.SessionClose) && a.Params["anyway"] == true && a.Result == store.ActionOK {
			found = true
		}
	}
	if !found {
		t.Fatalf("forced close was not journaled: %+v", rows)
	}
	if w := readyPost(t, srv, true, ""); w.Code != http.StatusConflict {
		t.Fatalf("readiness after close: %d", w.Code)
	}
}
