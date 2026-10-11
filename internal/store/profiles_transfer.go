package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TransferProject locks the project, both groups and their accounts before
// preparing the map change. Files move only after every SQL statement succeeds;
// they retain an undo until COMMIT is known to have succeeded. The callback
// belongs to the service: the store has no access to the host filesystem.
func (s *Store) TransferProject(ctx context.Context, id, group int,
	move func(ProfileProject, string, string) (func() error, error)) (p ProfileProject, err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return p, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	before, err := scanProject(rowOnly{tx.QueryRow(ctx, `SELECT id, group_id, name, path, session_name, base_branch, sort, launch
  FROM profile_projects WHERE id=$1 FOR UPDATE`, id)})
	if err != nil {
		return p, missing(err, "there is no project %d", id)
	}
	// A group move or an account edit must not change what From/To mean while
	// the executor is copying the archive.
	rows, err := tx.Query(ctx, `SELECT g.id,p.id,p.config_dir FROM profile_groups g JOIN profiles p ON p.id=g.profile_id
  WHERE g.id=ANY($1::int[]) ORDER BY g.id FOR UPDATE OF g,p`, []int{before.GroupID, group})
	if err != nil {
		return p, err
	}
	var from, to string
	var sourceID, targetID int
	for rows.Next() {
		var gid, pid int
		var config string
		if err := rows.Scan(&gid, &pid, &config); err != nil {
			rows.Close()
			return p, err
		}
		if gid == before.GroupID {
			from, sourceID = config, pid
		}
		if gid == group {
			to, targetID = config, pid
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, err
	}
	if targetID == 0 {
		return p, fmt.Errorf("%w: there is no group %d", ErrNotFound, group)
	}
	if sourceID == targetID || from == to {
		return p, badRequest("pick a group in another contour")
	}
	p, err = scanProject(rowOnly{tx.QueryRow(ctx, `UPDATE profile_projects SET group_id=$2,
  sort=(SELECT COALESCE(MAX(sort),-1)+1 FROM profile_projects WHERE group_id=$2)
  WHERE id=$1 RETURNING id,group_id,name,path,session_name,base_branch,sort,launch`, id, group)})
	if err != nil {
		return ProfileProject{}, err
	}
	if _, err := journal(ctx, tx, journalRow{entity: journalProject, id: p.ID, name: p.Name, op: OpUpdate,
		changes: changesOf(normalized(projectFields(before)), normalized(projectFields(p)))}); err != nil {
		return ProfileProject{}, err
	}
	undo, err := move(before, from, to)
	if err != nil {
		return ProfileProject{}, err
	}
	if err = tx.Commit(ctx); err == nil {
		return p, nil
	}
	// A lost COMMIT reply is not a rollback. Resolve its outcome from a fresh
	// connection before undoing files, or leave the recoverable move pending.
	if !errors.Is(err, pgx.ErrTxCommitRollback) {
		check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		var actual int
		known := pool.QueryRow(check, `SELECT group_id FROM profile_projects WHERE id=$1`, id).Scan(&actual)
		if known != nil {
			return ProfileProject{}, fmt.Errorf("map commit outcome is unknown; keep the project move pending until the database answers: %w", errors.Join(err, known))
		}
		if actual == group {
			return p, nil
		}
		if actual != before.GroupID {
			return ProfileProject{}, fmt.Errorf("map changed during commit recovery; project move needs recovery: %w", err)
		}
	}
	return ProfileProject{}, errors.Join(err, undo())
}
