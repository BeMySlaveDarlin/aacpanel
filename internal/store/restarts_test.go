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
	if _, err := pool.Exec(t.Context(), "DELETE FROM session_restarts"); err != nil {
		t.Fatal(err)
	}
	return s
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
	if err := s.PruneRestartCycles(t.Context(), []string{c.Conversation}, now); err != nil {
		t.Fatal(err)
	}
	rows, err = s.RestartCycles(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatal("pruned a live conversation")
	}
	if err := s.PruneRestartCycles(t.Context(), []string{}, now); err != nil {
		t.Fatal(err)
	}
	rows, err = s.RestartCycles(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("kept a dead cycle: %v %v", rows, err)
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
