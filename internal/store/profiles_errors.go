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
