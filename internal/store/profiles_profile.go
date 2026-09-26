package store

import (
	"context"
	"fmt"

	"aacpanel/internal/schema"
)

// CreateProfile creates a profile.
func (s *Store) CreateProfile(ctx context.Context, e ProfileEdit) (p Profile, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return p, err
	}
	name, err := checkName(e.Name, "the profile name")
	if err != nil {
		return p, err
	}
	if e.ConfigDir == nil {
		return p, badRequest("the config directory is not set")
	}
	configDir, err := CheckConfigDir(*e.ConfigDir)
	if err != nil {
		return p, err
	}
	prefix, err := s.checkPrefix(e.Prefix)
	if err != nil {
		return p, err
	}
	var claudeBin string
	if e.ClaudeBin != nil {
		if claudeBin, err = CheckClaudeBin(*e.ClaudeBin); err != nil {
			return p, err
		}
	}
	launch, err := checkLaunch(e.Launch, schema.LevelContour)
	if err != nil {
		return p, err
	}

	row := pool.QueryRow(ctx, `
		INSERT INTO profiles (name, config_dir, prefix, claude_bin, sort, launch)
		VALUES ($1, $2, $3, $4, COALESCE($5, (SELECT COALESCE(MAX(sort), -1) + 1 FROM profiles)), COALESCE($6, '{}'::jsonb))
		RETURNING `+profileCols, name, configDir, prefix, claudeBin, e.Sort, launch)
	p, err = scanProfile(rowOnly{row})
	if err != nil {
		return p, nameTaken(err)
	}
	return p, nil
}

// UpdateProfile changes only the named fields.
func (s *Store) UpdateProfile(ctx context.Context, id int, e ProfileEdit) (p Profile, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return p, err
	}
	var name *string
	if e.Name != nil {
		v, err := checkName(e.Name, "the profile name")
		if err != nil {
			return p, err
		}
		name = &v
	}
	var configDir *string
	if e.ConfigDir != nil {
		v, err := CheckConfigDir(*e.ConfigDir)
		if err != nil {
			return p, err
		}
		configDir = &v
	}
	var prefix *string
	if e.Prefix != nil {
		v, err := s.checkPrefix(e.Prefix)
		if err != nil {
			return p, err
		}
		prefix = &v
	}
	var claudeBin *string
	if e.ClaudeBin != nil {
		v, err := CheckClaudeBin(*e.ClaudeBin)
		if err != nil {
			return p, err
		}
		claudeBin = &v
	}
	launch, err := checkLaunch(e.Launch, schema.LevelContour)
	if err != nil {
		return p, err
	}
	ops, err := launchOps(e.Launch, e.LaunchSet, e.LaunchUnset)
	if err != nil {
		return p, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)

	if ops {
		launch, err = launchChange(ctx, tx, "profiles", id, e.LaunchSet, e.LaunchUnset, schema.LevelContour)
		if err != nil {
			return p, err
		}
	}

	row := tx.QueryRow(ctx, `
		UPDATE profiles SET
			name       = COALESCE($2, name),
			config_dir = COALESCE($3, config_dir),
			prefix     = COALESCE($4, prefix),
			claude_bin = COALESCE($5, claude_bin),
			sort       = COALESCE($6, sort),
			launch     = COALESCE($7, launch)
		WHERE id = $1
		RETURNING `+profileCols, id, name, configDir, prefix, claudeBin, e.Sort, launch)
	p, err = scanProfile(rowOnly{row})
	if err != nil {
		return p, nameTaken(missing(err, "there is no profile %d", id))
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// DeleteProfile removes a profile together with its empty groups.
func (s *Store) DeleteProfile(ctx context.Context, id int, cascade bool) (err error) {
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
		if _, err := tx.Exec(ctx, `
			DELETE FROM profile_projects
			 WHERE group_id IN (SELECT id FROM profile_groups WHERE profile_id = $1)`, id); err != nil {
			return err
		}
	} else {
		var projects int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM profile_projects p
			JOIN profile_groups g ON g.id = p.group_id
			WHERE g.profile_id = $1`, id).Scan(&projects); err != nil {
			return err
		}
		if projects > 0 {
			return fmt.Errorf("%w: profile %d holds %d projects", ErrNotEmpty, id, projects)
		}
	}

	tag, err := tx.Exec(ctx, `DELETE FROM profiles WHERE id = $1`, id)
	if err != nil {
		return notEmpty(err, "a project appeared in profile %d", id)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: there is no profile %d", ErrNotFound, id)
	}
	return tx.Commit(ctx)
}
