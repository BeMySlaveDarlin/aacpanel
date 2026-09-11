package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func checkSameSet(existing, ids []int) error {
	if len(existing) != len(ids) {
		return badRequest("the list does not match the current set: there were %d, %d arrived", len(existing), len(ids))
	}
	want := make(map[int]bool, len(existing))
	for _, id := range existing {
		want[id] = true
	}
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return badRequest("id %d repeats in the list", id)
		}
		seen[id] = true
		if !want[id] {
			return badRequest("id %d is not part of the current set", id)
		}
	}
	return nil
}

func scanIDs(rows pgx.Rows) ([]int, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (int, error) {
		var id int
		err := r.Scan(&id)
		return id, err
	})
}

// ReorderProfiles sets the new order of the profiles.
func (s *Store) ReorderProfiles(ctx context.Context, ids []int) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT id FROM profiles FOR UPDATE`)
	if err != nil {
		return err
	}
	existing, err := scanIDs(rows)
	if err != nil {
		return err
	}
	if err := checkSameSet(existing, ids); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE profiles SET sort = $2 WHERE id = $1`, id, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReorderGroups sets the new order of the groups inside one profile.
func (s *Store) ReorderGroups(ctx context.Context, profileID int, ids []int) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT id FROM profile_groups WHERE profile_id = $1 FOR UPDATE`, profileID)
	if err != nil {
		return err
	}
	existing, err := scanIDs(rows)
	if err != nil {
		return err
	}
	if err := checkSameSet(existing, ids); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE profile_groups SET sort = $2 WHERE id = $1`, id, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReorderProjects sets the new order of the projects inside one group.
func (s *Store) ReorderProjects(ctx context.Context, groupID int, ids []int) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT id FROM profile_projects WHERE group_id = $1 FOR UPDATE`, groupID)
	if err != nil {
		return err
	}
	existing, err := scanIDs(rows)
	if err != nil {
		return err
	}
	if err := checkSameSet(existing, ids); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE profile_projects SET sort = $2 WHERE id = $1`, id, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
