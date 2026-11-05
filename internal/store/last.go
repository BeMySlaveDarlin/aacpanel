package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// KeepSessionLast remembers what the card of a live conversation shows now,
// for its archive card once the session is gone.
func (s *Store) KeepSessionLast(ctx context.Context, conversation string, last json.RawMessage) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO session_last (conversation, last) VALUES ($1, $2)
		ON CONFLICT (conversation) DO UPDATE SET last = excluded.last, kept_at = now()`,
		conversation, last)
	if err != nil {
		return fmt.Errorf("keeping the last card of %s: %w", conversation, Unavailable(err))
	}
	return nil
}

// SessionLasts returns what the cards of the conversations last showed, for
// those the panel saw live.
func (s *Store) SessionLasts(ctx context.Context, conversations []string) (map[string]json.RawMessage, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, last FROM session_last WHERE conversation = ANY($1)`, conversations)
	if err != nil {
		return nil, fmt.Errorf("the last cards: %w", Unavailable(err))
	}
	type kept struct {
		conversation string
		last         json.RawMessage
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (kept, error) {
		var got kept
		return got, r.Scan(&got.conversation, &got.last)
	})
	if err != nil {
		return nil, fmt.Errorf("the last cards: %w", Unavailable(err))
	}
	out := make(map[string]json.RawMessage, len(list))
	for _, got := range list {
		out[got.conversation] = got.last
	}
	return out, nil
}
