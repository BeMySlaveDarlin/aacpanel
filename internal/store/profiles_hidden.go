package store

import (
	"context"
)

// HiddenDirs returns the paths removed from the parsing queue.
func (s *Store) HiddenDirs(ctx context.Context) (out map[string]bool, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT path FROM disk_hidden`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out = map[string]bool{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out[path] = true
	}
	return out, rows.Err()
}

// HideDir removes a directory from the parsing queue.
func (s *Store) HideDir(ctx context.Context, path string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	clean, err := s.checkPath(path)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO disk_hidden (path) VALUES ($1) ON CONFLICT (path) DO NOTHING`, clean)
	return err
}

// ShowDir returns a directory to the parsing queue.
func (s *Store) ShowDir(ctx context.Context, path string) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `DELETE FROM disk_hidden WHERE path = $1`, path)
	return err
}
