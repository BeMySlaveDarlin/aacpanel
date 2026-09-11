package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/notify"
)

// PushState is the log of reasons on top of the database.
type PushState struct{ s *Store }

// PushJournal returns the memory of reasons.
func (s *Store) PushJournal() *PushState {
	if s == nil {
		return nil
	}
	return &PushState{s: s}
}

// Raised returns the reasons that have already been announced.
func (p *PushState) Raised(ctx context.Context) (out []notify.Event, err error) {
	defer func() { err = Unavailable(err) }()

	if p == nil || p.s == nil {
		return nil, ErrUnavailable
	}
	pool, err := p.s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT event FROM push_state`)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (notify.Event, error) {
		var raw []byte
		if err := r.Scan(&raw); err != nil {
			return notify.Event{}, err
		}
		var e notify.Event
		err := json.Unmarshal(raw, &e)
		return e, err
	})
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		if e.Key != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// Raise remembers a reason.
func (p *PushState) Raise(ctx context.Context, e notify.Event) error {
	if p == nil || p.s == nil {
		return ErrUnavailable
	}
	pool, err := p.s.Pool()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO push_state (key, event) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET event = excluded.event, raised_at = now()`,
		e.Key, payload)
	if err != nil {
		return fmt.Errorf("writing the reason: %w", Unavailable(err))
	}
	return nil
}

// Drop forgets a reason.
func (p *PushState) Drop(ctx context.Context, key string) error {
	if p == nil || p.s == nil {
		return ErrUnavailable
	}
	pool, err := p.s.Pool()
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM push_state WHERE key = $1`, key); err != nil {
		return fmt.Errorf("clearing the reason: %w", Unavailable(err))
	}
	return nil
}
