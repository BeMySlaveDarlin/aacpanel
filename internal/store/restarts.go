package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RestartCycle belongs to a conversation, so a new process under the same
// session name cannot inherit its predecessor's permission to restart.
type RestartCycle struct {
	Conversation, Session string
	CommandAt             time.Time
	Commanded             bool
	RemindedAt, ReadyAt   *time.Time
	Intent                string
	AttemptedAt           *time.Time
	Done                  bool
	Manual                bool
	Finish                string
}

const (
	PrepareRestart  = "restart"
	PrepareClose    = "close"
	PrepareFinalize = "finalize"
)

func (s *Store) RestartCycles(ctx context.Context) ([]RestartCycle, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, session, commanded_at, commanded,
		reminded_at, ready_at, intent, attempted_at, done, manual, finish FROM session_restarts`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RestartCycle, error) {
		var c RestartCycle
		err := row.Scan(&c.Conversation, &c.Session, &c.CommandAt, &c.Commanded,
			&c.RemindedAt, &c.ReadyAt, &c.Intent, &c.AttemptedAt, &c.Done, &c.Manual, &c.Finish)
		return c, err
	})
}

func (s *Store) KeepRestartCycle(ctx context.Context, c RestartCycle) error {
	if c.Finish == "" {
		c.Finish = PrepareRestart
	}
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO session_restarts
		(conversation, session, commanded_at, commanded, reminded_at, ready_at, intent, attempted_at, done, manual, finish)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (conversation) DO UPDATE SET session=$2, commanded_at=$3,
		commanded=CASE WHEN session_restarts.done AND NOT $9 THEN $4 ELSE (session_restarts.commanded OR $4) END,
		reminded_at=$5,
		ready_at=CASE WHEN session_restarts.done AND NOT $9 THEN $6 ELSE COALESCE(session_restarts.ready_at,$6) END,
		intent=CASE WHEN session_restarts.ready_at IS NULL OR (session_restarts.done AND NOT $9) THEN $7 ELSE session_restarts.intent END,
		attempted_at=$8, done=$9,
		manual=CASE WHEN session_restarts.done AND NOT $9 THEN $10 ELSE (session_restarts.manual OR $10) END,
		finish=CASE WHEN session_restarts.done THEN $11 ELSE session_restarts.finish END`,
		c.Conversation, c.Session, c.CommandAt, c.Commanded, c.RemindedAt, c.ReadyAt, c.Intent, c.AttemptedAt, c.Done, c.Manual, c.Finish)
	return err
}

// Keep live conversations even when old: resuming the same conversation
// must not begin another cycle. Absent conversations outlive transient gaps
// in the collector and are eventually pruned, their finalization marks too:
// a closed conversation never starts the turn that would clear its mark.
func (s *Store) PruneRestartCycles(ctx context.Context, live []string, before time.Time) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	if _, err = pool.Exec(ctx, `DELETE FROM session_restarts
		WHERE commanded_at < $1 AND NOT (conversation = ANY($2))`, before, live); err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM session_finalizations
		WHERE ready_at < $1 AND NOT (conversation = ANY($2))`, before, live)
	return err
}

// Readiness may arrive while session.send is returning. A field-specific
// update keeps the cycle's delivery state. A repeat keeps the first intent
// but advances the timestamp: its own turn must also end before the restart.
func (s *Store) ReadyRestartCycle(ctx context.Context, conversation, intent string, now time.Time) (bool, error) {
	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	tag, err := pool.Exec(ctx, `WITH accepted AS (UPDATE session_restarts SET ready_at=$3,
		intent=CASE WHEN ready_at IS NULL THEN $2 ELSE intent END, commanded=true
		WHERE conversation=$1 AND NOT done RETURNING conversation, finish),
		marked AS (INSERT INTO session_finalizations (conversation, ready_at)
		SELECT conversation, $3 FROM accepted WHERE finish IN ('close','finalize')
		ON CONFLICT (conversation) DO UPDATE SET ready_at=EXCLUDED.ready_at)
		SELECT conversation FROM accepted`, conversation, intent, now)
	return tag.RowsAffected() > 0, err
}

// A repeated press cannot clear readiness or move an active cycle's deadline.
func (s *Store) BeginManualRestart(ctx context.Context, conversation, session string, now time.Time) error {
	return s.BeginPreparation(ctx, conversation, session, PrepareRestart, now)
}

func (s *Store) BeginPreparation(ctx context.Context, conversation, session, finish string, now time.Time) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `INSERT INTO session_restarts (conversation, session, commanded_at, manual, finish)
        VALUES ($1,$2,$3,true,$4) ON CONFLICT (conversation) DO UPDATE SET
        session=$2, manual=true, finish=$4,
        commanded_at=CASE WHEN session_restarts.done THEN $3 ELSE session_restarts.commanded_at END,
        commanded=CASE WHEN session_restarts.done THEN false ELSE session_restarts.commanded END,
        reminded_at=CASE WHEN session_restarts.done THEN NULL ELSE session_restarts.reminded_at END,
        ready_at=CASE WHEN session_restarts.done THEN NULL ELSE session_restarts.ready_at END,
        intent=CASE WHEN session_restarts.done THEN '' ELSE session_restarts.intent END,
        attempted_at=CASE WHEN session_restarts.done THEN NULL ELSE session_restarts.attempted_at END,
		done=false WHERE session_restarts.done OR session_restarts.finish=$4`, conversation, session, now, finish)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("the conversation is already preparing for another action")
	}
	return err
}

// The timestamp belongs to the user input that starts a turn. Model requests
// after session_ready still finish that same turn and cannot revoke readiness.
func (s *Store) Finalizations(ctx context.Context, turns map[string]time.Time) (map[string]time.Time, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(turns))
	ats := make([]time.Time, 0, len(turns))
	for id, at := range turns {
		ids = append(ids, id)
		ats = append(ats, at)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM session_finalizations f USING unnest($1::text[], $2::timestamptz[]) AS t(conversation, at)
		WHERE f.conversation=t.conversation AND t.at > f.ready_at`, ids, ats); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, ready_at FROM session_finalizations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}
