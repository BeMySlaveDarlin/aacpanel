package store

import (
	"testing"
	"time"

	"aacpanel/deploy/migrations"
)

func restartStore(t *testing.T) *Store {
	t.Helper()
	s := requestStore(t)
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM session_restarts; DELETE FROM session_finalizations"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPreparationFinalizationAndConflictingActionsPG(t *testing.T) {
	s := restartStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.BeginPreparation(t.Context(), "conversation", "lab", PrepareFinalize, now); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginPreparation(t.Context(), "conversation", "lab", PrepareClose, now); err == nil {
		t.Fatal("a close silently replaced an active wrap up")
	}
	if ok, err := s.ReadyRestartCycle(t.Context(), "conversation", "", now); err != nil || !ok {
		t.Fatalf("ready: %v %v", ok, err)
	}
	marks, err := s.Finalizations(t.Context(), map[string]time.Time{"conversation": now.Add(-time.Second)})
	if err != nil || !marks["conversation"].Equal(now) {
		t.Fatalf("mark: %+v %v", marks, err)
	}
	cycles, err := s.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || cycles[0].Finish != PrepareFinalize {
		t.Fatalf("cycle: %+v %v", cycles, err)
	}
	cycles[0].Done = true
	if err := s.KeepRestartCycle(t.Context(), cycles[0]); err != nil {
		t.Fatal(err)
	}
	// A later automatic cycle cannot inherit a completed save's readiness.
	if err := s.KeepRestartCycle(t.Context(), RestartCycle{Conversation: "conversation", Session: "lab", CommandAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	cycles, err = s.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || cycles[0].Finish != PrepareRestart || cycles[0].Manual || cycles[0].Commanded || cycles[0].ReadyAt != nil {
		t.Fatalf("automatic cycle inherited wrap up: %+v %v", cycles, err)
	}
	cycles[0].Done = true
	if err := s.KeepRestartCycle(t.Context(), cycles[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginPreparation(t.Context(), "conversation", "lab", PrepareClose, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cycles, err = s.RestartCycles(t.Context())
	if err != nil || len(cycles) != 1 || cycles[0].Finish != PrepareClose || cycles[0].ReadyAt != nil || cycles[0].Done {
		t.Fatalf("new cycle: %+v %v", cycles, err)
	}
	marks, err = s.Finalizations(t.Context(), map[string]time.Time{"conversation": now.Add(time.Second)})
	if err != nil || len(marks) != 0 {
		t.Fatalf("new turn kept mark: %+v %v", marks, err)
	}
	if err := s.BeginManualRestart(t.Context(), "restart", "peer", now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ReadyRestartCycle(t.Context(), "restart", "Next", now); err != nil || !ok {
		t.Fatalf("restart ready: %v %v", ok, err)
	}
	marks, err = s.Finalizations(t.Context(), nil)
	if err != nil || len(marks) != 0 {
		t.Fatalf("a restart marked the replaced conversation: %+v %v", marks, err)
	}
}

func TestRestartCyclePersistenceAndPruningPG(t *testing.T) {
	s := restartStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := RestartCycle{Conversation: "conversation", Session: "lab", CommandAt: now.Add(-8 * 24 * time.Hour),
		Commanded: true, ReadyAt: &now, RemindedAt: &now, AttemptedAt: &now, Intent: "Read the handoff",
		Attempts: 2, CWD: "/srv/project", Parent: "lead", RecoveredSession: "lab-2"}
	if err := s.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	c.Done = true
	if err := s.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	rows, err := s.RestartCycles(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("cycles: %v, %v", rows, err)
	}
	r := rows[0]
	if !r.Done || !r.Commanded || r.Intent != c.Intent || r.ReadyAt == nil || !r.ReadyAt.Equal(now) ||
		r.RemindedAt == nil || !r.RemindedAt.Equal(now) || r.AttemptedAt == nil || !r.AttemptedAt.Equal(now) ||
		r.Attempts != c.Attempts || r.CWD != c.CWD || r.Parent != c.Parent || r.RecoveredSession != c.RecoveredSession {
		t.Fatalf("lost cycle state: %+v", r)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO session_finalizations (conversation, ready_at) VALUES ($1, $2)`,
		c.Conversation, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	marked := func() int {
		var n int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM session_finalizations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := s.PruneRestartCycles(t.Context(), []string{c.Conversation}, now); err != nil {
		t.Fatal(err)
	}
	rows, err = s.RestartCycles(t.Context())
	if err != nil || len(rows) != 1 || marked() != 1 {
		t.Fatal("pruned a live conversation")
	}
	if err := s.PruneRestartCycles(t.Context(), []string{}, now); err != nil {
		t.Fatal(err)
	}
	rows, err = s.RestartCycles(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("kept a dead cycle: %v %v", rows, err)
	}
	if marked() != 0 {
		t.Fatal("kept the finalization mark of a closed conversation")
	}
}

func TestRestartRecoveryStateResetsForANewPreparationPG(t *testing.T) {
	s := restartStore(t)
	now := time.Now().UTC()
	c := RestartCycle{Conversation: "conversation", Session: "lab", CommandAt: now, Commanded: true,
		Attempts: 3, CWD: "/srv/project", Parent: "lead", RecoveredSession: "lab-2", Done: true}
	if err := s.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginManualRestart(t.Context(), c.Conversation, c.Session, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.RestartCycles(t.Context())
	if err != nil || len(rows) != 1 || rows[0].Attempts != 0 || rows[0].CWD != "" || rows[0].Parent != "" ||
		rows[0].RecoveredSession != "" || rows[0].Done {
		t.Fatalf("a new preparation inherited recovery state: %+v %v", rows, err)
	}
}

func TestRestartClosedRequiresACompletedClosePG(t *testing.T) {
	s := restartStore(t)
	for _, tc := range []struct {
		name, kind, device, result string
		prepare, old, want         bool
	}{
		{name: "queued preparation", prepare: true},
		{name: "failed close", result: ActionFailed},
		{name: "cycle close", device: "context restart"},
		{name: "old close", old: true},
		{name: "human close", want: true},
		{name: "human kill", kind: "session.kill", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := RestartCycle{Conversation: t.Name(), Session: "work/" + tc.name, CommandAt: time.Now().Add(-time.Minute)}
			if tc.old {
				c.CommandAt = time.Now().Add(time.Minute)
			}
			kind, device, result := tc.kind, tc.device, tc.result
			if kind == "" {
				kind = "session.close"
			}
			if device == "" {
				device = "browser"
			}
			if result == "" {
				result = ActionOK
			}
			id, err := s.LogAttempt(t.Context(), Action{Kind: kind, Target: c.Session, DeviceName: device,
				Params: map[string]any{"conversation": c.Conversation, "prepare": tc.prepare}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.LogResult(t.Context(), id, ActionOutcome{Result: result}); err != nil {
				t.Fatal(err)
			}
			if got, err := s.RestartClosed(t.Context(), c); err != nil || got != tc.want {
				t.Fatalf("close completed: %v, want %v: %v", got, tc.want, err)
			}
		})
	}
}

func TestRestartRecoveryMigrationCountsOnlyAttemptedRestartsPG(t *testing.T) {
	s := restartStore(t)
	pool, _ := s.Pool()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), `CREATE TEMP TABLE session_restarts
		(LIKE session_restarts INCLUDING ALL) ON COMMIT DROP;
		ALTER TABLE session_restarts DROP COLUMN attempts, DROP COLUMN cwd, DROP COLUMN parent, DROP COLUMN recovered_session;
		CREATE TEMP TABLE actions (LIKE actions INCLUDING ALL) ON COMMIT DROP;
		INSERT INTO session_restarts (conversation,session,commanded_at,commanded,attempted_at,done) VALUES
		('failed-restart','work','2026-01-01',true,'2026-01-02',false),
		('failed-command','work','2026-01-01',false,'2026-01-02',false),
		('done','work','2026-01-01',true,'2026-01-02',true);
		INSERT INTO actions (ts,kind,target,device_name,params) VALUES
		('2026-01-02','session.restart','work','context restart','{"conversation":"failed-restart"}'),
		('2026-01-02','session.send','work','context restart','{"conversation":"failed-command"}'),
		('2026-01-02','session.restart','work','browser','{"conversation":"failed-command"}'),
		('2025-01-01','session.restart','work','context restart','{"conversation":"failed-command"}'),
		('2026-01-02','session.restart','work','context restart','{"conversation":"done"}')`); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.FS.ReadFile("078_restart_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), string(body)); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"failed-restart": 1, "failed-command": 0, "done": 0} {
		var got int
		if err := tx.QueryRow(t.Context(), `SELECT attempts FROM session_restarts WHERE conversation=$1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s has %d restart attempts, want %d", id, got, want)
		}
	}
}

func TestReadinessSurvivesTheCommandReturningAndRepeatsPG(t *testing.T) {
	s := restartStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := RestartCycle{Conversation: "conversation", Session: "lab", CommandAt: now}
	if err := s.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ReadyRestartCycle(t.Context(), c.Conversation, "First intent", now); err != nil || !ok {
		t.Fatalf("ready: %v %v", ok, err)
	}
	// Delivery completes using the state it read before the tool replied.
	c.Commanded = true
	if err := s.KeepRestartCycle(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ReadyRestartCycle(t.Context(), c.Conversation, "Second intent", now.Add(time.Second)); err != nil || !ok {
		t.Fatalf("repeat: %v %v", ok, err)
	}
	rows, err := s.RestartCycles(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("cycles: %v %v", rows, err)
	}
	if rows[0].ReadyAt == nil || !rows[0].ReadyAt.Equal(now.Add(time.Second)) || rows[0].Intent != "First intent" {
		t.Fatalf("readiness overwritten: %+v", rows[0])
	}
	if ok, err := s.ReadyRestartCycle(t.Context(), "unasked", "", now); err != nil || ok {
		t.Fatalf("unasked: %v %v", ok, err)
	}
}
