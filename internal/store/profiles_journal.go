package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// The entities of the map a journal entry is about.
const (
	journalContour = "contour"
	journalGroup   = "group"
	journalProject = "project"
)

// The ops a journal entry records.
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

const journalMax = 200

// Change is one field of an entry of the map that a save changed. A key of
// the launch is named launch.<key>; an absent value is null.
type Change struct {
	Key  string `json:"key"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

// JournalEntry is one change of the map: who made it and when, what it
// touched, and field by field what it changed.
type JournalEntry struct {
	ID       int64    `json:"id"`
	At       int64    `json:"at"`
	Actor    string   `json:"actor"`
	Entity   string   `json:"entity"`
	EntityID int      `json:"entityId"`
	Name     string   `json:"name"`
	Op       string   `json:"op"`
	Changes  []Change `json:"changes"`
	// Undoable says whether the entry can be taken back from the journal:
	// a deletion of a project not taken back yet.
	Undoable bool   `json:"undoable"`
	Undoes   *int64 `json:"undoes,omitempty"`
	UndoneAt *int64 `json:"undoneAt,omitempty"`
}

type actorKey struct{}

// WithActor names who makes the changes of the map done under ctx: the
// journal says it beside each of them.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorOf(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}

type journalRow struct {
	entity  string
	id      int
	name    string
	op      string
	changes []Change
	before  any
	undoes  *int64
}

// journal writes an entry in the transaction of the change itself: a change
// the journal did not take is not made, and a journal never names one that
// was rolled back.
func journal(ctx context.Context, tx pgx.Tx, e journalRow) (int64, error) {
	changes := e.changes
	if changes == nil {
		changes = []Change{}
	}
	raw, err := json.Marshal(changes)
	if err != nil {
		return 0, err
	}
	var before []byte
	if e.before != nil {
		if before, err = json.Marshal(e.before); err != nil {
			return 0, err
		}
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO profile_journal (actor, entity, entity_id, name, op, changes, row_before, undoes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		actorOf(ctx), e.entity, e.id, e.name, e.op, raw, before, e.undoes).Scan(&id)
	return id, err
}

// changesOf returns the fields that differ between two states of an entry, in
// key order; a missing state is an entry that did not exist.
func changesOf(before, after map[string]any) []Change {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	slices.Sort(sorted)
	var out []Change
	for _, k := range sorted {
		from, to := before[k], after[k]
		if reflect.DeepEqual(from, to) {
			continue
		}
		out = append(out, Change{Key: k, From: from, To: to})
	}
	return out
}

// fieldsWithLaunch returns an entry's fields with its launch spread as
// launch.<key>: a change of the effort reads as that, not as a new object.
func fieldsWithLaunch(fields map[string]any, launch json.RawMessage) map[string]any {
	out := map[string]any{}
	for k, v := range fields {
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		out[k] = v
	}
	var obj map[string]any
	if json.Unmarshal(launch, &obj) == nil {
		for k, v := range obj {
			out["launch."+k] = v
		}
	}
	return out
}

func contourFields(p Profile) map[string]any {
	return fieldsWithLaunch(map[string]any{"name": p.Name, "configDir": p.ConfigDir, "prefix": p.Prefix,
		"claudeBin": p.ClaudeBin}, p.Launch)
}

func groupFields(g ProfileGroup) map[string]any {
	return map[string]any{"name": g.Name, "contour": float64(g.ProfileID)}
}

func projectFields(p ProfileProject) map[string]any {
	return fieldsWithLaunch(map[string]any{"name": p.Name, "path": p.Path, "session": p.Session,
		"base": p.Base, "group": float64(p.GroupID)}, p.Launch)
}

// normalized returns fields the way they come back from the journal's json,
// so that a state read from the database and one built in memory compare
// equal where they are.
func normalized(fields map[string]any) map[string]any {
	raw, err := json.Marshal(fields)
	if err != nil {
		return fields
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return fields
	}
	return out
}

// deletedProject is what the journal keeps of a deleted project: everything
// an undo needs to put it back as it was, its identifier included.
type deletedProject struct {
	ID      int             `json:"id"`
	GroupID int             `json:"groupId"`
	Name    string          `json:"name"`
	Path    string          `json:"path"`
	Session string          `json:"session"`
	Base    string          `json:"base"`
	Sort    int             `json:"sort"`
	Launch  json.RawMessage `json:"launch"`
}

func keptProject(p ProfileProject) deletedProject {
	return deletedProject{ID: p.ID, GroupID: p.GroupID, Name: p.Name, Path: p.Path, Session: p.Session,
		Base: p.Base, Sort: p.Sort, Launch: p.Launch}
}

// journalDeletedProjects writes a deletion for every project a delete took,
// each with what an undo puts back.
func journalDeletedProjects(ctx context.Context, tx pgx.Tx, rows pgx.Rows) error {
	projects, err := pgx.CollectRows(rows, scanProject)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := journal(ctx, tx, journalRow{entity: journalProject, id: p.ID, name: p.Name, op: OpDelete,
			changes: changesOf(normalized(projectFields(p)), nil), before: keptProject(p)}); err != nil {
			return err
		}
	}
	return nil
}

// Journal returns the latest changes of the map, the newest first.
func (s *Store) Journal(ctx context.Context, limit int) (out []JournalEntry, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > journalMax {
		limit = journalMax
	}
	rows, err := pool.Query(ctx, `
		SELECT id, at, actor, entity, entity_id, name, op, changes, undoes, undone_at
		FROM profile_journal ORDER BY at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (JournalEntry, error) {
		var e JournalEntry
		var at time.Time
		var undone *time.Time
		var changes []byte
		if err := r.Scan(&e.ID, &at, &e.Actor, &e.Entity, &e.EntityID, &e.Name, &e.Op, &changes,
			&e.Undoes, &undone); err != nil {
			return e, err
		}
		e.At = at.Unix()
		if undone != nil {
			u := undone.Unix()
			e.UndoneAt = &u
		}
		if err := json.Unmarshal(changes, &e.Changes); err != nil {
			return e, err
		}
		e.Undoable = e.Entity == journalProject && e.Op == OpDelete && e.UndoneAt == nil
		return e, nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []JournalEntry{}
	}
	return out, nil
}

// ErrNotUndoable means the journal entry cannot be taken back.
var ErrNotUndoable = errors.New("the entry cannot be taken back")

// UndoDelete puts back the project a journal entry deleted, as it was: the
// same identifier, group, place in the group and launch. It fails where the
// group is gone or the directory went to another project since, and says so.
func (s *Store) UndoDelete(ctx context.Context, entryID int64) (p ProfileProject, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return p, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)

	var entity, op string
	var before []byte
	var undone *time.Time
	err = tx.QueryRow(ctx, `
		SELECT entity, op, row_before, undone_at FROM profile_journal WHERE id = $1 FOR UPDATE`,
		entryID).Scan(&entity, &op, &before, &undone)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, fmt.Errorf("%w: there is no journal entry %d", ErrNotFound, entryID)
	}
	if err != nil {
		return p, err
	}
	if entity != journalProject || op != OpDelete || len(before) == 0 {
		return p, fmt.Errorf("%w: entry %d is not a deletion of a project", ErrNotUndoable, entryID)
	}
	if undone != nil {
		return p, fmt.Errorf("%w: entry %d was taken back already", ErrNotUndoable, entryID)
	}
	var kept deletedProject
	if err := json.Unmarshal(before, &kept); err != nil {
		return p, err
	}
	var groupAlive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profile_groups WHERE id = $1)`,
		kept.GroupID).Scan(&groupAlive); err != nil {
		return p, err
	}
	if !groupAlive {
		return p, fmt.Errorf("%w: the group of %q is deleted too — add the project again", ErrNotUndoable, kept.Name)
	}
	launch := kept.Launch
	if len(launch) == 0 {
		launch = json.RawMessage(`{}`)
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO profile_projects (id, group_id, name, path, session_name, base_branch, sort, launch)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, group_id, name, path, session_name, base_branch, sort, launch`,
		kept.ID, kept.GroupID, kept.Name, kept.Path, kept.Session, kept.Base, kept.Sort, launch)
	p, err = scanProject(rowOnly{row})
	if err != nil {
		return p, s.pathTaken(ctx, err, kept.Path)
	}
	if _, err := tx.Exec(ctx, `UPDATE profile_journal SET undone_at = now() WHERE id = $1`, entryID); err != nil {
		return p, err
	}
	if _, err := journal(ctx, tx, journalRow{entity: journalProject, id: p.ID, name: p.Name, op: OpCreate,
		changes: changesOf(nil, normalized(projectFields(p))), undoes: &entryID}); err != nil {
		return p, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProfileProject{}, err
	}
	return p, nil
}
