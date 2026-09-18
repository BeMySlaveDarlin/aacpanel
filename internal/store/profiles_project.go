package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateProject creates a project in a group.
func (s *Store) CreateProject(ctx context.Context, groupID int, e ProjectEdit) (p ProfileProject, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return p, err
	}
	name, err := checkName(e.Name, "the project label")
	if err != nil {
		return p, err
	}
	if e.Path == nil {
		return p, badRequest("the project path is not set")
	}
	path, err := s.checkPath(*e.Path)
	if err != nil {
		return p, err
	}
	session, err := checkSession(e.Session)
	if err != nil {
		return p, err
	}
	base, err := checkBase(e.Base)
	if err != nil {
		return p, err
	}
	launch, err := checkLaunch(e.Launch)
	if err != nil {
		return p, err
	}

	row := pool.QueryRow(ctx, `
		INSERT INTO profile_projects (group_id, name, path, session_name, base_branch, sort, launch)
		VALUES ($1, $2, $3, COALESCE($4, ''), COALESCE($7, ''), COALESCE($5, (SELECT COALESCE(MAX(sort), -1) + 1 FROM profile_projects WHERE group_id = $1)), COALESCE($6, '{}'::jsonb))
		RETURNING id, group_id, name, path, session_name, base_branch, sort, launch`,
		groupID, name, path, session, e.Sort, launch, base)
	p, err = scanProject(rowOnly{row})
	if err != nil {
		return p, s.pathTaken(ctx, noParent(err, "there is no group %d", groupID), path)
	}
	return p, nil
}

// UpdateProject changes only the project's named fields, the group included.
func (s *Store) UpdateProject(ctx context.Context, id int, e ProjectEdit) (p ProfileProject, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return p, err
	}
	var name *string
	if e.Name != nil {
		v, err := checkName(e.Name, "the project label")
		if err != nil {
			return p, err
		}
		name = &v
	}
	var path *string
	if e.Path != nil {
		v, err := s.checkPath(*e.Path)
		if err != nil {
			return p, err
		}
		path = &v
	}
	session, err := checkSession(e.Session)
	if err != nil {
		return p, err
	}
	base, err := checkBase(e.Base)
	if err != nil {
		return p, err
	}
	launch, err := checkLaunch(e.Launch)
	if err != nil {
		return p, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)

	if e.GroupID != nil {
		if err := checkMove(ctx, tx, id, *e.GroupID); err != nil {
			return p, err
		}
	}

	row := tx.QueryRow(ctx, `
		UPDATE profile_projects SET
			name         = COALESCE($2, name),
			path         = COALESCE($3, path),
			session_name = COALESCE($4, session_name),
			base_branch  = COALESCE($8, base_branch),
			group_id     = COALESCE($7, group_id),
			sort         = COALESCE($5, CASE
				WHEN $7::int IS NULL OR $7 = group_id THEN sort
				ELSE (SELECT COALESCE(MAX(sort), -1) + 1 FROM profile_projects WHERE group_id = $7)
			END),
			launch       = COALESCE($6, launch)
		WHERE id = $1
		RETURNING id, group_id, name, path, session_name, base_branch, sort, launch`,
		id, name, path, session, e.Sort, launch, e.GroupID, base)
	p, err = scanProject(rowOnly{row})
	if err != nil {
		pathVal := ""
		if path != nil {
			pathVal = *path
		}
		return p, s.pathTaken(ctx, missing(err, "there is no project %d", id), pathVal)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProfileProject{}, err
	}
	return p, nil
}

func checkMove(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, projectID, groupID int) error {
	var was, target *int
	err := q.QueryRow(ctx, `
		SELECT
			(SELECT g.profile_id FROM profile_groups g
			  JOIN profile_projects r ON r.group_id = g.id WHERE r.id = $1),
			(SELECT profile_id FROM profile_groups WHERE id = $2)`,
		projectID, groupID).Scan(&was, &target)
	if err != nil {
		return err
	}
	if was == nil {
		return fmt.Errorf("%w: there is no project %d", ErrNotFound, projectID)
	}
	if target == nil {
		return fmt.Errorf("%w: there is no group %d", ErrNotFound, groupID)
	}
	if *was != *target {
		return badRequest("group %d belongs to another profile: that one has its own token, its own "+
			"subscription and its own conversation archive, while the sessions, the launch "+
			"history and the transcripts of the project would stay with the old one — what "+
			"moves is not the project, only its map entry. A whole group moves as one", groupID)
	}
	return nil
}

// DeleteProject removes a project.
func (s *Store) DeleteProject(ctx context.Context, id int) (err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `DELETE FROM profile_projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: there is no project %d", ErrNotFound, id)
	}
	return nil
}

// ProjectBase returns the branch a review of this project is measured against,
// or an empty string when the map holds no project at that path or no choice
// for it. The absence is not an error: a directory can be read as a repository
// without ever appearing on the map.
func (s *Store) ProjectBase(ctx context.Context, path string) (string, error) {
	pool, err := s.Pool()
	if err != nil {
		return "", Unavailable(err)
	}
	var base string
	err = pool.QueryRow(ctx, `SELECT base_branch FROM profile_projects WHERE path = $1`, path).Scan(&base)
	if err != nil {
		return "", Unavailable(err)
	}
	return base, nil
}
