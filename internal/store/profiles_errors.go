package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func nameTaken(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return fmt.Errorf("%w: the profile name is taken", ErrConflict)
	}
	return err
}

func groupNameTaken(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return fmt.Errorf("%w: this profile already has a group with that name", ErrConflict)
	}
	return err
}

func (s *Store) pathTaken(ctx context.Context, err error, path string) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		return err
	}
	profileName, groupName, lookupErr := s.pathOwner(ctx, path)
	if lookupErr != nil {
		return fmt.Errorf("%w: this directory is already in the map", ErrConflict)
	}
	return fmt.Errorf("%w: this directory is already in the map — profile %q, group %q",
		ErrConflict, profileName, groupName)
}

// sessionTaken refuses a project whose session answers to the name another
// project of the map already answers to. A session name is the machine's, not
// the contour's: tmux keeps one namespace for all of them, so the second of two
// such projects comes up as name-2 while the panel waits for it under the name,
// and each project's screen counts the other's sessions as its own. A project
// with no session name of its own answers to the name of its directory.
func sessionTaken(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, p ProfileProject) error {
	var name, contour, project string
	err := q.QueryRow(ctx, `
		WITH named AS (
			SELECT p.id, p.name, g.profile_id,
				CASE WHEN btrim(p.session_name) <> '' THEN btrim(p.session_name)
				     ELSE regexp_replace(rtrim(p.path, '/'), '^.*/', '') END AS session
			FROM profile_projects p JOIN profile_groups g ON g.id = p.group_id)
		SELECT mine.session, pr.name, other.name
		FROM named mine
		JOIN named other ON other.session = mine.session AND other.id <> mine.id
		JOIN profiles pr ON pr.id = other.profile_id
		WHERE mine.id = $1
		ORDER BY other.id LIMIT 1`, p.ID).Scan(&name, &contour, &project)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: the session name %q is taken by project %q of contour %q — two projects answering "+
		"to one name take each other's sessions; give this one a session name of its own", ErrConflict, name, project, contour)
}

func (s *Store) pathOwner(ctx context.Context, path string) (profileName, groupName string, err error) {
	var pool *pgxpool.Pool
	pool, err = s.Pool()
	if err != nil {
		return "", "", err
	}
	err = pool.QueryRow(ctx, `
		SELECT pr.name, g.name
		FROM profile_projects p
		JOIN profile_groups g ON g.id = p.group_id
		JOIN profiles pr ON pr.id = g.profile_id
		WHERE p.path = $1`, path).Scan(&profileName, &groupName)
	return profileName, groupName, err
}

func noParent(err error, format string, args ...any) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return fmt.Errorf("%w: "+format, append([]any{ErrNotFound}, args...)...)
	}
	return missing(err, format, args...)
}

func notEmpty(err error, format string, args ...any) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "23001") {
		return fmt.Errorf("%w: "+format, append([]any{ErrNotEmpty}, args...)...)
	}
	return err
}

func missing(err error, format string, args ...any) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: "+format, append([]any{ErrNotFound}, args...)...)
	}
	return err
}
