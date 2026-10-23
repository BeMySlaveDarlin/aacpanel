package store

import (
	"context"
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
}

func (s *Store) RestartCycles(ctx context.Context) ([]RestartCycle, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, session, commanded_at, commanded,
		reminded_at, ready_at, intent, attempted_at, done FROM session_restarts`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RestartCycle, error) {
		var c RestartCycle
		err := row.Scan(&c.Conversation, &c.Session, &c.CommandAt, &c.Commanded,
			&c.RemindedAt, &c.ReadyAt, &c.Intent, &c.AttemptedAt, &c.Done)
		return c, err
	})
}

func (s *Store) KeepRestartCycle(ctx context.Context, c RestartCycle) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO session_restarts
		(conversation, session, commanded_at, commanded, reminded_at, ready_at, intent, attempted_at, done)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (conversation) DO UPDATE SET session=$2, commanded_at=$3, commanded=(session_restarts.commanded OR $4),
		reminded_at=$5, ready_at=COALESCE(session_restarts.ready_at,$6),
		intent=CASE WHEN session_restarts.ready_at IS NULL THEN $7 ELSE session_restarts.intent END,
		attempted_at=$8, done=$9`,
		c.Conversation, c.Session, c.CommandAt, c.Commanded, c.RemindedAt, c.ReadyAt, c.Intent, c.AttemptedAt, c.Done)
	return err
}

// Keep live conversations even when old: resuming the same conversation
// must not begin another cycle. Absent conversations outlive transient gaps
// in the collector and are eventually pruned.
func (s *Store) PruneRestartCycles(ctx context.Context, live []string, before time.Time) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM session_restarts
		WHERE commanded_at < $1 AND NOT (conversation = ANY($2))`, before, live)
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
	tag, err := pool.Exec(ctx, `UPDATE session_restarts SET ready_at=$3,
		intent=CASE WHEN ready_at IS NULL THEN $2 ELSE intent END, commanded=true
		WHERE conversation=$1 AND NOT done`, conversation, intent, now)
	return tag.RowsAffected() > 0, err
}
