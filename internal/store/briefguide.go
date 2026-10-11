package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// BriefGuide distinguishes an empty guide from one that has never been imported:
// clearing the editor must not bring the host's file back on the next start.
func (s *Store) BriefGuide(ctx context.Context) (text string, found bool, err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return "", false, err
	}
	err = pool.QueryRow(ctx, `SELECT text FROM brief_guide WHERE id = 1`).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return text, err == nil, err
}

func (s *Store) SaveBriefGuide(ctx context.Context, text string) (err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO brief_guide (id, text) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET text = EXCLUDED.text, updated_at = now()`, text)
	return err
}

// ImportBriefGuide never overwrites a save that arrived while the collector
// was reading the file. An absent file still completes the import with "".
func (s *Store) ImportBriefGuide(ctx context.Context, text string) (err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO brief_guide (id, text) VALUES (1, $1)
		ON CONFLICT (id) DO NOTHING`, text)
	return err
}
