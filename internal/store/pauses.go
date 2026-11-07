package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionPause survives both the session's process and the panel. Snapshot
// preserves the card while the host has no process to describe.
type SessionPause struct {
	LivingSession
	Snapshot  json.RawMessage
	Until     *time.Time
	Sleeping  bool
	CommandAt time.Time
}

func (s *Store) SessionPauses(ctx context.Context, host int) ([]SessionPause, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT saved,snapshot,until_at,sleeping,commanded_at FROM session_pauses WHERE host_id=$1 ORDER BY session`, host)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SessionPause, error) {
		var p SessionPause
		var raw []byte
		if err := row.Scan(&raw, &p.Snapshot, &p.Until, &p.Sleeping, &p.CommandAt); err != nil {
			return p, err
		}
		err := json.Unmarshal(raw, &p.LivingSession)
		return p, err
	})
}

func (s *Store) PutSessionPause(ctx context.Context, host int, p SessionPause) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(p.LivingSession)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO session_pauses (host_id,conversation,session,saved,snapshot,until_at,sleeping,commanded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (host_id,conversation) DO UPDATE SET
		session=$3,saved=$4,snapshot=$5,until_at=$6,sleeping=$7,commanded_at=$8`,
		host, p.Conversation, p.Session, raw, p.Snapshot, p.Until, p.Sleeping, p.CommandAt)
	return err
}

func (s *Store) DropSessionPause(ctx context.Context, host int, conversation string) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM session_pauses WHERE host_id=$1 AND conversation=$2`, host, conversation)
	return err
}
