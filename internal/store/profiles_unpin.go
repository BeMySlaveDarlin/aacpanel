package store

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/schema"
)

// Unpin takes a launch key out of the projects of a contour that store the
// very value the contour stores: raised to the contour, the value no longer
// needs a copy in each of them, and a copy left there stops following the
// contour when it changes. A project that stores another value keeps it. Every
// project changed is journaled, all in one transaction; the number changed is
// returned.
func (s *Store) Unpin(ctx context.Context, contourID int, key string) (n int, err error) {
	defer func() { err = Unavailable(err) }()

	if _, ok := schema.Find(key); !ok {
		return 0, badRequest("%s is not a launch parameter", key)
	}
	pool, err := s.Pool()
	if err != nil {
		return 0, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var raw json.RawMessage
	err = tx.QueryRow(ctx, `SELECT launch FROM profiles WHERE id = $1 FOR UPDATE`, contourID).Scan(&raw)
	if err != nil {
		return 0, missing(err, "there is no contour %d", contourID)
	}
	contour, err := launchObject(raw, "the contour")
	if err != nil {
		return 0, err
	}
	want, ok := contour[key]
	if !ok {
		return 0, badRequest("the contour does not set %s: its projects have nothing to repeat", key)
	}

	rows, err := tx.Query(ctx, `
		SELECT r.id, r.group_id, r.name, r.path, r.session_name, r.base_branch, r.sort, r.launch
		FROM profile_projects r JOIN profile_groups g ON g.id = r.group_id
		WHERE g.profile_id = $1
		ORDER BY r.id
		FOR UPDATE OF r`, contourID)
	if err != nil {
		return 0, err
	}
	projects, err := pgx.CollectRows(rows, scanProject)
	if err != nil {
		return 0, err
	}

	for _, before := range projects {
		own, err := launchObject(before.Launch, "the project")
		if err != nil {
			return 0, err
		}
		if got, ok := own[key]; !ok || !reflect.DeepEqual(got, want) {
			continue
		}
		delete(own, key)
		launch, err := json.Marshal(own)
		if err != nil {
			return 0, err
		}
		after, err := scanProject(rowOnly{tx.QueryRow(ctx, `
			UPDATE profile_projects SET launch = $2 WHERE id = $1
			RETURNING id, group_id, name, path, session_name, base_branch, sort, launch`, before.ID, launch)})
		if err != nil {
			return 0, err
		}
		changes := changesOf(normalized(projectFields(before)), normalized(projectFields(after)))
		if _, err := journal(ctx, tx, journalRow{entity: journalProject, id: after.ID, name: after.Name, op: OpUpdate,
			changes: changes}); err != nil {
			return 0, err
		}
		n++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}
