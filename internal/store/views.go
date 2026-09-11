package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionViews holds the view marks on top of the database.
type SessionViews struct{ s *Store }

// SessionViews returns the view marks.
func (s *Store) SessionViews() *SessionViews {
	if s == nil {
		return nil
	}
	return &SessionViews{s: s}
}

// Mark remembers that a device is looking at this session right now.
func (v *SessionViews) Mark(ctx context.Context, device int64, session string) error {
	if v == nil || v.s == nil {
		return ErrUnavailable
	}
	pool, err := v.s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO session_views (device_id, session) VALUES ($1, $2)
		ON CONFLICT (device_id) DO UPDATE SET session = excluded.session, seen_at = now()`,
		device, session)
	if err != nil {
		return fmt.Errorf("marking the view: %w", Unavailable(err))
	}
	return nil
}

// Forget removes the mark.
func (v *SessionViews) Forget(ctx context.Context, device int64) error {
	if v == nil || v.s == nil {
		return ErrUnavailable
	}
	pool, err := v.s.Pool()
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM session_views WHERE device_id = $1`, device); err != nil {
		return fmt.Errorf("clearing the view mark: %w", Unavailable(err))
	}
	return nil
}

// Watching returns the devices that have this session on screen.
func (v *SessionViews) Watching(ctx context.Context, session string, fresh time.Duration) ([]int64, error) {
	if v == nil || v.s == nil {
		return nil, ErrUnavailable
	}
	pool, err := v.s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT device_id FROM session_views
		 WHERE session = $1 AND seen_at > now() - make_interval(secs => $2)`,
		session, fresh.Seconds())
	if err != nil {
		return nil, fmt.Errorf("who is looking at the session: %w", Unavailable(err))
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (int64, error) {
		var id int64
		return id, r.Scan(&id)
	})
	if err != nil {
		return nil, fmt.Errorf("who is looking at the session: %w", Unavailable(err))
	}
	return list, nil
}
