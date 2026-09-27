package store

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/schema"
)

// A group is a shelf, not a level of the launch: what is said for all of its
// projects is written into each of them, and a project moved off it keeps what
// it stores. Every project changed is journaled, all in one transaction.

// SetForGroup gives every project of a group the same value of a launch key;
// a nil value takes the key out of all of them, so they follow their contour
// again. A project already storing the value is left as it is. The number of
// projects changed is returned.
func (s *Store) SetForGroup(ctx context.Context, groupID int, key string, value any) (n int, err error) {
	defer func() { err = Unavailable(err) }()

	p, ok := schema.Find(key)
	if !ok || !p.At(schema.LevelProject) {
		return 0, badRequest("%s is not a launch parameter of a project", key)
	}
	if value != nil {
		if problems := schema.Check(schema.LevelProject, map[string]any{key: value}); len(problems) > 0 {
			return 0, badRequest("the launch would not take it — %s", problems[0])
		}
	}
	tx, projects, err := s.shelf(ctx, groupID)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	for _, before := range projects {
		own, err := launchObject(before.Launch, "the project")
		if err != nil {
			return 0, err
		}
		if own == nil {
			own = map[string]any{}
		}
		got, has := own[key]
		switch {
		case value == nil && !has, value != nil && has && reflect.DeepEqual(got, value):
			continue
		case value == nil:
			delete(own, key)
		default:
			own[key] = value
		}
		launch, err := json.Marshal(own)
		if err != nil {
			return 0, err
		}
		if err := rewrite(ctx, tx, before, `UPDATE profile_projects SET launch = $2 WHERE id = $1
			RETURNING id, group_id, name, path, session_name, base_branch, sort, launch`, launch); err != nil {
			return 0, err
		}
		n++
	}
	return n, tx.Commit(ctx)
}

// MoveShelf moves every project of a group onto another group of the same
// contour, after the ones already there and in their order: an emptied group
// is one that can be deleted. The number of projects moved is returned.
func (s *Store) MoveShelf(ctx context.Context, from, to int) (n int, err error) {
	defer func() { err = Unavailable(err) }()

	if from == to {
		return 0, badRequest("the projects are already on this group")
	}
	tx, projects, err := s.shelf(ctx, from)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var fromContour, toContour *int
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT profile_id FROM profile_groups WHERE id = $1),
		       (SELECT profile_id FROM profile_groups WHERE id = $2)`, from, to).Scan(&fromContour, &toContour); err != nil {
		return 0, err
	}
	if toContour == nil {
		return 0, missing(pgx.ErrNoRows, "there is no group %d", to)
	}
	if *fromContour != *toContour {
		return 0, badRequest("group %d belongs to another contour: its projects would move on the map while "+
			"their sessions, history and transcripts stay with the old account — a whole group moves as one", to)
	}
	var sort int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sort), -1) + 1 FROM profile_projects WHERE group_id = $1`,
		to).Scan(&sort); err != nil {
		return 0, err
	}
	for _, before := range projects {
		if err := rewrite(ctx, tx, before, `UPDATE profile_projects SET group_id = $2, sort = $3 WHERE id = $1
			RETURNING id, group_id, name, path, session_name, base_branch, sort, launch`, to, sort); err != nil {
			return 0, err
		}
		sort++
		n++
	}
	return n, tx.Commit(ctx)
}

// shelf opens a transaction and returns the projects of a group in their
// order, locked for the change.
func (s *Store) shelf(ctx context.Context, groupID int) (pgx.Tx, []ProfileProject, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	var id int
	if err := tx.QueryRow(ctx, `SELECT id FROM profile_groups WHERE id = $1 FOR UPDATE`, groupID).Scan(&id); err != nil {
		tx.Rollback(ctx)
		return nil, nil, missing(err, "there is no group %d", groupID)
	}
	rows, err := tx.Query(ctx, `
		SELECT id, group_id, name, path, session_name, base_branch, sort, launch
		FROM profile_projects WHERE group_id = $1 ORDER BY sort, id FOR UPDATE`, groupID)
	if err != nil {
		tx.Rollback(ctx)
		return nil, nil, err
	}
	projects, err := pgx.CollectRows(rows, scanProject)
	if err != nil {
		tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, projects, nil
}

// rewrite runs an update of one project and journals what it changed.
func rewrite(ctx context.Context, tx pgx.Tx, before ProfileProject, query string, args ...any) error {
	after, err := scanProject(rowOnly{tx.QueryRow(ctx, query, append([]any{before.ID}, args...)...)})
	if err != nil {
		return err
	}
	changes := changesOf(normalized(projectFields(before)), normalized(projectFields(after)))
	if len(changes) == 0 {
		return nil
	}
	_, err = journal(ctx, tx, journalRow{entity: journalProject, id: after.ID, name: after.Name, op: OpUpdate, changes: changes})
	return err
}
