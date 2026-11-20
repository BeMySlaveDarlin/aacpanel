package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func recoveryID(t *testing.T) string {
	t.Helper()
	id := sha256.Sum256([]byte(t.Name()))
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func recoveryName(t *testing.T) string {
	t.Helper()
	return "recovery-" + recoveryID(t)[:8]
}

func recoverySnapshot(t *testing.T, srv *Server, at time.Time, rows ...restartSession) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"at": at.Unix(), "sessionsAt": at.Unix(), "sessions": rows})
	if err != nil {
		t.Fatal(err)
	}
	srv.host = host.NewReader(snapshotWith(t, string(raw)))
}

func recoveryCycle(t *testing.T, srv *Server) store.RestartCycle {
	t.Helper()
	cycles, err := srv.db.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 {
		t.Fatalf("restart cycles: %+v %v", cycles, err)
	}
	return cycles[0]
}

func TestRestartRecoveryOpensTheProjectAndDeliversTheSavedIntentPG(t *testing.T) {
	srv, _, dir := restartServer(t)
	client, failed := startFakeExec(t, action.Response{OK: false, Error: "starting the session: context canceled"})
	srv.exec = client
	now := time.Now().UTC().Truncate(time.Second)
	ready := now.Add(-time.Minute)
	c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: ready,
		Commanded: true, ReadyAt: &ready, Intent: "Read the saved handoff"}
	if err := srv.db.BeginManualRestart(t.Context(), c.Conversation, c.Session, ready); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	row := restartSession{LiveSession: host.LiveSession{Name: c.Session, SessionID: c.Conversation, CWD: dir,
		Parent: &host.SessionRef{Session: "lead"}}, Status: "idle"}
	recoverySnapshot(t, srv, now, row)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if req := restartRequest(t, failed); req.Kind != action.SessionRestart {
		t.Fatalf("the original restart was not attempted: %+v", req)
	}
	c = recoveryCycle(t, srv)
	if c.Attempts != 1 || c.CWD != dir || c.Parent != "lead" || c.Done || c.AttemptedAt == nil {
		t.Fatalf("the interrupted restart lost its recovery state: %+v", c)
	}
	client, fake := startFakeExec(t, action.Response{OK: true, Session: c.Session})
	srv = &Server{db: srv.db, exec: client}
	recoverySnapshot(t, srv, now.Add(restartGrace-time.Second))
	if err := srv.restartOnce(t.Context(), now.Add(restartGrace-time.Second)); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
	recoverySnapshot(t, srv, now.Add(restartGrace))
	if err := srv.restartOnce(t.Context(), now.Add(restartGrace)); err != nil {
		t.Fatal(err)
	}
	open, send := restartRequest(t, fake), restartRequest(t, fake)
	if open.Kind != action.SessionOpen || open.Target != c.Session || open.Project == nil ||
		open.Project.Path != dir || open.Project.Session != c.Session || open.Project.Parent != "lead" || open.Resume != "" {
		t.Fatalf("the closed conversation did not open with its project and name: %+v", open)
	}
	if send.Kind != action.SessionSend || send.Target != c.Session || send.Text != c.Intent {
		t.Fatalf("the saved intent did not follow the open: %+v", send)
	}
	c = recoveryCycle(t, srv)
	if !c.Done || c.Attempts != 2 || c.RecoveredSession != c.Session || c.AttemptedAt != nil {
		t.Fatalf("the recovered cycle is not terminal: %+v", c)
	}
	// Even a later resume of the old conversation cannot replay this cycle.
	recoverySnapshot(t, srv, now.Add(2*restartGrace), row)
	if err := srv.restartOnce(t.Context(), now.Add(2*restartGrace)); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
	journal, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 3})
	if err != nil || len(journal) != 3 || journal[0].Kind != "session.send" || journal[0].Result != store.ActionOK ||
		journal[1].Kind != "session.open" || journal[1].Result != store.ActionOK || journal[2].Result != store.ActionFailed {
		t.Fatalf("the recovery is missing from the action journal: %+v %v", journal, err)
	}
}

func TestRestartRecoveryFinishesClosedOrReplacedCyclesPG(t *testing.T) {
	for _, reason := range []string{"preparation", "failed preparation", "close", "wrap up", "replacement", "human close", "human kill", "recovered session closed", "recovered session disappeared", "exhausted"} {
		t.Run(reason, func(t *testing.T) {
			srv, fake, dir := restartServer(t)
			now := time.Now().UTC().Truncate(time.Second)
			attempt := now.Add(-restartGrace)
			c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: attempt.Add(-time.Minute),
				Commanded: true, Attempts: 1, AttemptedAt: &attempt, CWD: dir}
			var rows []restartSession
			switch reason {
			case "preparation":
				c.Attempts, c.AttemptedAt = 0, nil
			case "failed preparation":
				c.Attempts = 0
			case "close":
				c.Finish = store.PrepareClose
			case "wrap up":
				c.Finish = store.PrepareFinalize
			case "replacement":
				rows = []restartSession{{LiveSession: host.LiveSession{Name: c.Session, SessionID: "new-conversation", CWD: dir}}}
			case "recovered session disappeared":
				c.RecoveredSession = "opened-" + c.Session
			case "human close", "human kill", "recovered session closed":
				kind := "session.close"
				if reason == "human kill" {
					kind = "session.kill"
				}
				target, conversation := c.Session, c.Conversation
				if reason == "recovered session closed" {
					c.RecoveredSession = "opened-" + c.Session
					target, conversation = c.RecoveredSession, "replacement-conversation"
				}
				id, err := srv.db.LogAttempt(t.Context(), store.Action{Kind: kind, Target: target,
					DeviceName: "browser", Params: map[string]any{"conversation": conversation}})
				if err != nil {
					t.Fatal(err)
				}
				if err := srv.db.LogResult(t.Context(), id, store.ActionOutcome{Result: store.ActionOK}); err != nil {
					t.Fatal(err)
				}
			case "exhausted":
				c.Attempts = restartAttempts
			}
			if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			recoverySnapshot(t, srv, now, rows...)
			if err := srv.restartOnce(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			if got := recoveryCycle(t, srv); !got.Done {
				t.Fatalf("the terminal cycle was left armed: %+v", got)
			}
			noRestartRequest(t, fake)
		})
	}
}

func TestRestartRecoveryFinishesAfterTheLastLiveRestartFailsPG(t *testing.T) {
	srv, _, dir := restartServer(t)
	client, fake := startFakeExec(t, action.Response{OK: false, Error: "the session did not close"})
	srv.exec = client
	now := time.Now().UTC().Truncate(time.Second)
	ready := now.Add(-time.Minute)
	c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: ready,
		Commanded: true, Manual: true, ReadyAt: &ready, Attempts: restartAttempts - 1}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	row := restartSession{LiveSession: host.LiveSession{Name: c.Session, SessionID: c.Conversation, CWD: dir}, Status: "idle"}
	recoverySnapshot(t, srv, now, row)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if req := restartRequest(t, fake); req.Kind != action.SessionRestart {
		t.Fatalf("the last live attempt: %+v", req)
	}
	if got := recoveryCycle(t, srv); !got.Done || got.Attempts != restartAttempts {
		t.Fatalf("the last live failure left the cycle armed: %+v", got)
	}
	recoverySnapshot(t, srv, now.Add(restartGrace), row)
	if err := srv.restartOnce(t.Context(), now.Add(restartGrace)); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
}

func TestRestartRecoveryBoundsFailedOpensAcrossServiceReplacementsPG(t *testing.T) {
	srv, _, dir := restartServer(t)
	client, fake := startFakeExec(t, action.Response{OK: false, Error: "the launcher is unavailable"})
	srv.exec = client
	now := time.Now().UTC().Truncate(time.Second)
	attempt := now.Add(-restartGrace)
	c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: attempt.Add(-time.Minute),
		Commanded: true, Attempts: 1, AttemptedAt: &attempt, CWD: dir}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= restartAttempts; n++ {
		srv = &Server{db: srv.db, exec: client}
		recoverySnapshot(t, srv, now)
		if err := srv.restartOnce(t.Context(), now); err != nil {
			t.Fatal(err)
		}
		if req := restartRequest(t, fake); req.Kind != action.SessionOpen {
			t.Fatalf("the closed session was restarted instead of opened: %+v", req)
		}
		got := recoveryCycle(t, srv)
		if got.Attempts != n || got.Done != (n == restartAttempts) {
			t.Fatalf("the attempt budget was not persisted: %+v", got)
		}
		now = now.Add(restartGrace)
	}
	recoverySnapshot(t, srv, now)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
}

func TestRestartRecoveryRetriesIntentOnTheSessionItOpenedPG(t *testing.T) {
	srv, _, dir := restartServer(t)
	client, fake := startFakeExec(t, action.Response{OK: true, Session: "aacpanel-9"})
	fake.failures = map[action.Kind]string{action.SessionSend: "message delivery failed"}
	srv.exec = client
	now := time.Now().UTC().Truncate(time.Second)
	attempt := now.Add(-restartGrace)
	c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: attempt.Add(-time.Minute),
		Commanded: true, Attempts: 1, AttemptedAt: &attempt, CWD: dir, Intent: "Read the handoff"}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	recoverySnapshot(t, srv, now)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if open, send := restartRequest(t, fake), restartRequest(t, fake); open.Kind != action.SessionOpen ||
		send.Kind != action.SessionSend || send.Target != "aacpanel-9" {
		t.Fatalf("open and send: %+v %+v", open, send)
	}
	if got := recoveryCycle(t, srv); got.Done || got.RecoveredSession != "aacpanel-9" {
		t.Fatalf("the failed delivery lost the opened session: %+v", got)
	}
	client, fake = startFakeExec(t, action.Response{OK: true})
	srv = &Server{db: srv.db, exec: client}
	now = now.Add(restartGrace)
	recoverySnapshot(t, srv, now, restartSession{LiveSession: host.LiveSession{Name: "aacpanel-9", SessionID: "new", CWD: dir}})
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if send := restartRequest(t, fake); send.Kind != action.SessionSend || send.Target != "aacpanel-9" || send.Text != c.Intent {
		t.Fatalf("the retry opened another session or lost the intent: %+v", send)
	}
	noRestartRequest(t, fake)
	if got := recoveryCycle(t, srv); !got.Done || got.Attempts != restartAttempts {
		t.Fatalf("successful delivery left the cycle armed: %+v", got)
	}
}

func TestRestartRecoveryFindsTheProjectOfAnInterruptedLegacyCyclePG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	attempt := now.Add(-restartGrace)
	c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: attempt.Add(-time.Minute),
		Commanded: true, Attempts: 1, AttemptedAt: &attempt}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	hostID, err := srv.db.HostID(t.Context(), srv.hostName)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.db.KeepLiving(t.Context(), hostID, "boot", attempt, []store.LivingSession{{
		Conversation: c.Conversation, Session: c.Session, CWD: dir, Parent: "lead", Pause: json.RawMessage(`null`),
	}}); err != nil {
		t.Fatal(err)
	}
	recoverySnapshot(t, srv, now)
	if err := srv.restartOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if req := restartRequest(t, fake); req.Kind != action.SessionOpen || req.Project == nil || req.Project.Path != dir || req.Project.Parent != "lead" {
		t.Fatalf("the legacy cycle lost its project: %+v", req)
	}
	noRestartRequest(t, fake)
	if got := recoveryCycle(t, srv); !got.Done {
		t.Fatalf("the legacy cycle remained armed: %+v", got)
	}
}

func TestRestartRecoveryWaitsForAFreshCensusPG(t *testing.T) {
	srv, fake, dir := restartServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	attempt := now.Add(-restartGrace)
	c := store.RestartCycle{Conversation: recoveryID(t), Session: recoveryName(t), CommandAt: attempt.Add(-time.Minute),
		Commanded: true, Attempts: 1, AttemptedAt: &attempt, CWD: dir}
	if err := srv.db.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	recoverySnapshot(t, srv, now.Add(-restartSnapshotAge-time.Second))
	if err := srv.restartOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	noRestartRequest(t, fake)
	if got := recoveryCycle(t, srv); got.Done || got.Attempts != 1 {
		t.Fatalf("a stale census changed the cycle: %+v", got)
	}
}
