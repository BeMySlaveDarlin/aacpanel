package store

import (
	"testing"
	"time"
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
		Commanded: true, ReadyAt: &now, RemindedAt: &now, AttemptedAt: &now, Intent: "Read the handoff"}
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
		r.RemindedAt == nil || !r.RemindedAt.Equal(now) || r.AttemptedAt == nil || !r.AttemptedAt.Equal(now) {
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
