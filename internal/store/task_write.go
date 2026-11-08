package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/taskreg"
	"github.com/jackc/pgx/v5"
)

func (s *Store) TaskWriter(ctx context.Context, project int) (writer string, err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return "", err
	}
	err = pool.QueryRow(ctx, `SELECT task_writer FROM profile_projects WHERE id=$1`, project).Scan(&writer)
	return writer, missing(err, "there is no project %d", project)
}

// The map row is shared with imports and writer switching. A single lock
// protects both the next number and the decision about who may write it.
func lockTaskProject(ctx context.Context, tx pgx.Tx, project int) error {
	var writer string
	err := tx.QueryRow(ctx, `SELECT task_writer FROM profile_projects WHERE id=$1 FOR UPDATE`, project).Scan(&writer)
	if err != nil {
		return missing(err, "there is no project %d", project)
	}
	if writer != taskreg.PanelWriter {
		return fmt.Errorf("%w: this project uses cctm files; switch its task writer before editing in the panel", ErrConflict)
	}
	return nil
}

func (s *Store) WriteTask(ctx context.Context, project int, number int64, actor taskreg.Actor, edit taskreg.Edit) (out taskreg.Task, err error) {
	defer func() { err = Unavailable(err) }()
	create := number == 0
	if number < 0 {
		return out, badRequest("a task number must be positive")
	}
	if err := edit.Validate(create); err != nil {
		return out, badRequest("%s", err)
	}
	if actor.Conversation == "" || actor.Name == "" {
		return out, badRequest("a task event needs its author")
	}
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = lockTaskProject(ctx, tx, project); err != nil {
		return out, err
	}
	if create {
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(number),0)+1 FROM project_tasks WHERE project_id=$1`, project).Scan(&number); err != nil {
			return out, err
		}
		out = taskreg.Task{ProjectID: project, Number: number, Status: "open", Tags: []string{}, BlockedBy: []int64{}, External: json.RawMessage(`null`), Body: json.RawMessage(`null`), CreatedBy: actor.Conversation, TouchedBy: []string{}}
	} else {
		out, err = scanTask(rowOnly{tx.QueryRow(ctx, `SELECT `+taskColumns+taskJoins+` WHERE t.project_id=$1 AND t.number=$2 AND t.deleted_at IS NULL`, project, number)})
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		if err != nil {
			return out, err
		}
	}
	was := out.Status
	changes := edit.Apply(&out)
	if slices.Contains(out.BlockedBy, number) {
		return out, badRequest("a task cannot block itself")
	}
	if len(out.BlockedBy) > 0 {
		blockers := slices.Compact(slices.Sorted(slices.Values(out.BlockedBy)))
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM project_tasks WHERE project_id=$1 AND number=ANY($2::bigint[]) AND deleted_at IS NULL`, project, blockers).Scan(&count); err != nil {
			return out, err
		}
		if count != len(blockers) {
			return out, badRequest("a blocking task does not exist in this project")
		}
	}
	if !create && len(changes) == 0 {
		return out, nil
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return out, err
	}
	at = at.UTC()
	out.UpdatedAt = &at
	if create {
		out.CreatedAt = &at
	}
	out.Session = actor.Conversation
	if !slices.Contains(out.TouchedBy, actor.Conversation) {
		out.TouchedBy = append(out.TouchedBy, actor.Conversation)
	}
	kind := "changed"
	if create {
		kind = "created"
	} else if was != out.Status {
		kind = "status_changed"
		if out.Status == "done" || out.Status == "cancelled" {
			kind = "closed"
		}
	}
	if out.Status == "done" || out.Status == "cancelled" {
		if create || was != out.Status {
			out.ClosedAt = &at
			out.ClosedBy = actor.Conversation
		}
	} else {
		out.ClosedAt = nil
		out.ClosedBy = ""
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_tasks(project_id,number,title,status,priority,position,tags,blocked_by,external,external_refs,external_url,
        created_at,updated_at,closed_at,created_by,closed_by,touched_by,session,body,markdown,source,source_json)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
        ON CONFLICT(project_id,number) DO UPDATE SET title=EXCLUDED.title,status=EXCLUDED.status,priority=EXCLUDED.priority,position=EXCLUDED.position,
        tags=EXCLUDED.tags,blocked_by=EXCLUDED.blocked_by,external=EXCLUDED.external,external_refs=EXCLUDED.external_refs,external_url=EXCLUDED.external_url,
        updated_at=EXCLUDED.updated_at,closed_at=EXCLUDED.closed_at,closed_by=EXCLUDED.closed_by,touched_by=EXCLUDED.touched_by,session=EXCLUDED.session,
        body=EXCLUDED.body,markdown=EXCLUDED.markdown`, project, number, out.Title, out.Status, out.Priority, out.Order, out.Tags, out.BlockedBy, out.External, out.ExternalRefs, out.ExternalURL,
		out.CreatedAt, out.UpdatedAt, out.ClosedAt, out.CreatedBy, out.ClosedBy, out.TouchedBy, out.Session, out.Body, out.Markdown, string(raw), raw)
	if err != nil {
		return out, err
	}
	payload, err := json.Marshal(map[string]any{"changes": changes, "from": was, "to": out.Status, "status": out.Status})
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO task_events(project_id,task_number,kind,at,precision,actor,actor_name,origin,payload)
        VALUES($1,$2,$3,$4,'instant',$5,$6,'panel',$7)`, project, number, kind, at, actor.Conversation, actor.Name, payload)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

const handoffColumns = `file,key,session,kind,at,date,title,cwd,git,tasks,task_numbers,body,source_json,deleted_at,session_name,next_step`

func scanHandoff(row pgx.CollectableRow) (h taskreg.Handoff, err error) {
	err = row.Scan(&h.File, &h.Key, &h.Session, &h.Kind, &h.At, &h.Date, &h.Title, &h.CWD, &h.Git, &h.Tasks, &h.TaskNumbers, &h.Body, &h.Source, &h.DeletedAt, &h.SessionName, &h.Next)
	taskTimesUTC(h.At, h.DeletedAt)
	return h, err
}

func (s *Store) Handoff(ctx context.Context, project int, key string) (h taskreg.Handoff, err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return h, err
	}
	h, err = scanHandoff(rowOnly{pool.QueryRow(ctx, `SELECT `+handoffColumns+` FROM project_handoffs
        WHERE project_id=$1 AND deleted_at IS NULL AND ($2='' OR key=$2) ORDER BY at DESC NULLS LAST,file DESC LIMIT 1`, project, key)})
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	return h, err
}

func (s *Store) WriteHandoff(ctx context.Context, project int, actor taskreg.Actor, cwd string, in taskreg.HandoffInput) (h taskreg.Handoff, err error) {
	defer func() { err = Unavailable(err) }()
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Markdown) == "" || actor.Conversation == "" || actor.Name == "" {
		return h, badRequest("a handoff needs a title, markdown and author")
	}
	pool, err := s.Pool()
	if err != nil {
		return h, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return h, err
	}
	defer tx.Rollback(ctx)
	if err = lockTaskProject(ctx, tx, project); err != nil {
		return h, err
	}
	rows, err := tx.Query(ctx, `SELECT e.task_number,e.kind FROM task_events e WHERE e.project_id=$1 AND e.actor=$2
        AND e.at > COALESCE((SELECT max(at) FROM project_handoffs WHERE project_id=$1 AND session=$2 AND deleted_at IS NULL),'-infinity'::timestamptz)
        ORDER BY e.task_number,e.id`, project, actor.Conversation)
	if err != nil {
		return h, err
	}
	groups := map[string][]int64{"created": {}, "closed": {}, "touched": {}}
	numbers := []int64{}
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var n int64
		var kind string
		if e := row.Scan(&n, &kind); e != nil {
			return struct{}{}, e
		}
		group := "touched"
		if kind == "created" || kind == "closed" {
			group = kind
		}
		if !slices.Contains(groups[group], n) {
			groups[group] = append(groups[group], n)
		}
		if !slices.Contains(numbers, n) {
			numbers = append(numbers, n)
		}
		return struct{}{}, nil
	})
	if err != nil {
		return h, err
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return h, err
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return h, err
	}
	at = at.UTC()
	key := hex.EncodeToString(token)
	body, _ := json.Marshal(in.Markdown)
	tasks, _ := json.Marshal(groups)
	h = taskreg.Handoff{File: "panel-" + key, Key: key, Session: actor.Conversation, SessionName: actor.Name, Kind: "session", At: &at, Date: at.Format(time.DateOnly), Title: in.Title, CWD: cwd, Git: json.RawMessage(`null`), Tasks: tasks, TaskNumbers: numbers, Body: body, Next: in.Next}
	raw, err := json.Marshal(h)
	if err != nil {
		return h, err
	}
	h.Source = raw
	_, err = tx.Exec(ctx, `INSERT INTO project_handoffs(project_id,file,key,session,session_name,kind,at,date,title,cwd,tasks,task_numbers,body,source,source_json,next_step)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, project, h.File, key, h.Session, h.SessionName, h.Kind, at, h.Date, h.Title, cwd, tasks, numbers, body, string(raw), raw, in.Next)
	if err != nil {
		return h, err
	}
	return h, tx.Commit(ctx)
}
