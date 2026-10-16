package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// callsKept is how long the call of a conversation is kept. The mark of a
// call does not go stale; a conversation nobody opened in a month is gone
// from the lists long before.
const callsKept = 30 * 24 * time.Hour

// SessionCalls holds the calls of the sessions for the person on top of the
// database: the last call of each conversation and whether the person has
// opened its session since.
type SessionCalls struct{ s *Store }

// SessionCalls returns the calls of the sessions.
func (s *Store) SessionCalls() *SessionCalls {
	if s == nil {
		return nil
	}
	return &SessionCalls{s: s}
}

// Called remembers a call of the conversation made at at under the session
// name; seen says someone had that session on screen as the call came. The
// collector lists a call for minutes and every listing comes here: a listing
// of the same call keeps what the person did about it, and a newer call
// replaces it. Calls older than callsKept go.
func (c *SessionCalls) Called(ctx context.Context, conversation, session string, at time.Time, seen bool) error {
	if c == nil || c.s == nil {
		return ErrUnavailable
	}
	pool, err := c.s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO session_calls (conversation, session, called_at, seen_at)
		VALUES ($1, $2, $3, CASE WHEN $4 THEN now() END)
		ON CONFLICT (conversation) DO UPDATE SET
			session   = CASE WHEN excluded.called_at >= session_calls.called_at
			                 THEN excluded.session ELSE session_calls.session END,
			seen_at   = CASE WHEN excluded.called_at > session_calls.called_at THEN excluded.seen_at
			                 ELSE coalesce(session_calls.seen_at, excluded.seen_at) END,
			called_at = greatest(session_calls.called_at, excluded.called_at)`,
		conversation, session, at, seen)
	if err != nil {
		return fmt.Errorf("remembering the call: %w", Unavailable(err))
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM session_calls WHERE called_at < now() - make_interval(secs => $1)`,
		callsKept.Seconds()); err != nil {
		return fmt.Errorf("forgetting old calls: %w", Unavailable(err))
	}
	return nil
}

// Seen marks the calls of the session as seen now: the person has it on
// screen. The session is found by its name, which is what a screen says it
// shows, or by its conversation, which outlives a new name.
func (c *SessionCalls) Seen(ctx context.Context, session, conversation string) error {
	if c == nil || c.s == nil {
		return ErrUnavailable
	}
	pool, err := c.s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		UPDATE session_calls SET seen_at = now()
		 WHERE seen_at IS NULL AND (session = $1 OR conversation = $2)`,
		session, conversation)
	if err != nil {
		return fmt.Errorf("marking the calls seen: %w", Unavailable(err))
	}
	return nil
}

// Unseen returns the time of the call of every conversation the person has
// not opened since it called, by conversation.
func (c *SessionCalls) Unseen(ctx context.Context) (map[string]time.Time, error) {
	if c == nil || c.s == nil {
		return nil, ErrUnavailable
	}
	pool, err := c.s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT conversation, called_at FROM session_calls WHERE seen_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("the calls not seen: %w", Unavailable(err))
	}
	type call struct {
		conversation string
		at           time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (call, error) {
		var got call
		return got, r.Scan(&got.conversation, &got.at)
	})
	if err != nil {
		return nil, fmt.Errorf("the calls not seen: %w", Unavailable(err))
	}
	out := make(map[string]time.Time, len(list))
	for _, got := range list {
		out[got.conversation] = got.at
	}
	return out, nil
}
