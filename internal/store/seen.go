package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionSeen holds when the person last had each conversation on screen, on
// any device, on top of the database.
type SessionSeen struct{ s *Store }

// SessionSeen returns the marks of the conversations seen.
func (s *Store) SessionSeen() *SessionSeen {
	if s == nil {
		return nil
	}
	return &SessionSeen{s: s}
}

// Mark remembers that the person has the conversation on screen now.
func (v *SessionSeen) Mark(ctx context.Context, conversation string) error {
	if v == nil || v.s == nil {
		return ErrUnavailable
	}
	pool, err := v.s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO session_seen (conversation, seen_at) VALUES ($1, now())
		ON CONFLICT (conversation) DO UPDATE SET seen_at = excluded.seen_at`,
		conversation)
	if err != nil {
		return fmt.Errorf("marking the conversation seen: %w", Unavailable(err))
	}
	return nil
}

// Of returns when each of the conversations was last on screen, for those
// that ever were.
func (v *SessionSeen) Of(ctx context.Context, conversations []string) (map[string]time.Time, error) {
	if v == nil || v.s == nil {
		return nil, ErrUnavailable
	}
	pool, err := v.s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, seen_at FROM session_seen WHERE conversation = ANY($1)`, conversations)
	if err != nil {
		return nil, fmt.Errorf("the conversations seen: %w", Unavailable(err))
	}
	type mark struct {
		conversation string
		at           time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (mark, error) {
		var got mark
		return got, r.Scan(&got.conversation, &got.at)
	})
	if err != nil {
		return nil, fmt.Errorf("the conversations seen: %w", Unavailable(err))
	}
	out := make(map[string]time.Time, len(list))
	for _, got := range list {
		out[got.conversation] = got.at
	}
	return out, nil
}
