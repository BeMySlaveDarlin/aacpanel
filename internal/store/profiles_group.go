package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateGroup creates a group in a profile.
func (s *Store) CreateGroup(ctx context.Context, profileID int, e GroupEdit) (g ProfileGroup, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return g, err
	}
	name, err := checkName(e.Name, "the group name")
	if err != nil {
		return g, err
	}
	row := pool.QueryRow(ctx, `
		INSERT INTO profile_groups (profile_id, name, sort)
		VALUES ($1, $2, COALESCE($3, (SELECT COALESCE(MAX(sort), -1) + 1 FROM profile_groups WHERE profile_id = $1)))
		RETURNING id, profile_id, name, sort`, profileID, name, e.Sort)
	g, err = scanGroup(rowOnly{row})
	if err != nil {
		return g, groupNameTaken(noParent(err, "there is no profile %d", profileID))
	}
	g.Projects = []ProfileProject{}
	return g, nil
}

// UpdateGroup changes only the group's named fields.
func (s *Store) UpdateGroup(ctx context.Context, id int, e GroupEdit) (g ProfileGroup, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return g, err
	}
	var name *string
	if e.Name != nil {
		v, err := checkName(e.Name, "the group name")
		if err != nil {
			return g, err
		}
		name = &v
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return g, err
	}
	defer tx.Rollback(ctx)

	if e.ProfileID != nil {
		if err := checkGroupMove(ctx, tx, id, *e.ProfileID, e.Agreed()); err != nil {
			return g, err
		}
	}

	row := tx.QueryRow(ctx, `
		UPDATE profile_groups SET
			name       = COALESCE($2, name),
			profile_id = COALESCE($4, profile_id),
			sort       = COALESCE($3, CASE
				WHEN $4::int IS NULL OR $4 = profile_id THEN sort
				ELSE (SELECT COALESCE(MAX(sort), -1) + 1 FROM profile_groups WHERE profile_id = $4)
			END)
		WHERE id = $1
		RETURNING id, profile_id, name, sort`, id, name, e.Sort, e.ProfileID)
	g, err = scanGroup(rowOnly{row})
	if err != nil {
		return g, groupNameTaken(missing(err, "there is no group %d", id))
	}
	if err := tx.Commit(ctx); err != nil {
		return ProfileGroup{}, err
	}
	g.Projects = []ProfileProject{}
	return g, nil
}

func checkGroupMove(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, groupID, profileID int, agreed bool) error {
	var was, target *int
	err := q.QueryRow(ctx, `
		SELECT
			(SELECT profile_id FROM profile_groups WHERE id = $1),
			(SELECT id FROM profiles WHERE id = $2)`,
		groupID, profileID).Scan(&was, &target)
	if err != nil {
		return err
	}
	if was == nil {
		return fmt.Errorf("%w: there is no group %d", ErrNotFound, groupID)
	}
	if target == nil {
		return fmt.Errorf("%w: there is no profile %d", ErrNotFound, profileID)
	}
	if *was != *target && !agreed {
		return badRequest("moving group %d to another profile changes the token and the subscription "+
			"of every project in it, and needs explicit consent (moveProfile)", groupID)
	}
	return nil
}

// DeleteGroup removes a group.
func (s *Store) DeleteGroup(ctx context.Context, id int, cascade bool) (err error) {
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

	if cascade {
		if _, err := tx.Exec(ctx,
			`DELETE FROM profile_projects WHERE group_id = $1`, id); err != nil {
			return err
		}
	} else {
		var projects int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM profile_projects WHERE group_id = $1`, id).Scan(&projects); err != nil {
			return err
		}
		if projects > 0 {
			return fmt.Errorf("%w: group %d holds %d projects", ErrNotEmpty, id, projects)
		}
	}

	tag, err := tx.Exec(ctx, `DELETE FROM profile_groups WHERE id = $1`, id)
	if err != nil {
		return notEmpty(err, "a project appeared in group %d", id)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: there is no group %d", ErrNotFound, id)
	}
	return tx.Commit(ctx)
}
