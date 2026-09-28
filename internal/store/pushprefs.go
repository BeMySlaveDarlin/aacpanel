package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// PushPrefs returns the stored choice of pushes, or an empty object when
// nothing was ever chosen.
func (s *Store) PushPrefs(ctx context.Context) (raw json.RawMessage, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	err = pool.QueryRow(ctx, `SELECT prefs FROM push_prefs WHERE id = 1`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return raw, err
}

// SavePushPrefs stores the choice of pushes whole.
func (s *Store) SavePushPrefs(ctx context.Context, raw json.RawMessage) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO push_prefs (id, prefs, updated_at) VALUES (1, $1, now())
		ON CONFLICT (id) DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()`, raw)
	return err
}

// PushRule is a rule as the choice of pushes lists it.
type PushRule struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// PushRules lists the rules for the choice of pushes, in the order they were made.
func (s *Store) PushRules(ctx context.Context) (out []PushRule, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT id, name, key, enabled FROM rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (PushRule, error) {
		var p PushRule
		err := r.Scan(&p.ID, &p.Name, &p.Key, &p.Enabled)
		return p, err
	})
	if out == nil {
		out = []PushRule{}
	}
	return out, err
}
