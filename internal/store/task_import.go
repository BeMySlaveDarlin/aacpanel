package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/taskreg"
	"github.com/jackc/pgx/v5"
)

type TaskImportResult struct {
	ProjectID int      `json:"projectId"`
	Root      string   `json:"root"`
	Tasks     int      `json:"tasks"`
	Handoffs  int      `json:"handoffs"`
	Changed   int      `json:"changed"`
	Deleted   int64    `json:"deleted"`
	Errors    []string `json:"errors"`
}

// ImportTasks mirrors one mapped project. Locking its map row serializes
// concurrent imports and prevents a stale path from importing into a moved map.
func (s *Store) ImportTasks(ctx context.Context, projectID int, expectedPath string, snapshot taskreg.Snapshot) (out TaskImportResult, err error) {
	defer func() { err = Unavailable(err) }()
	out = TaskImportResult{ProjectID: projectID, Root: snapshot.Root, Errors: append([]string{}, snapshot.Errors...)}
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var current string
	err = tx.QueryRow(ctx, `SELECT path FROM profile_projects WHERE id=$1 FOR UPDATE`, projectID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if current != expectedPath {
		return out, ErrConflict
	}
	if !snapshot.Present {
		return out, nil
	}
	meta := snapshot.Meta
	if len(meta) == 0 {
		meta = json.RawMessage("null")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO task_stores(project_id,root,meta) VALUES($1,$2,$3)
		ON CONFLICT(project_id) DO UPDATE SET root=EXCLUDED.root,meta=CASE WHEN EXCLUDED.meta='null'::jsonb THEN task_stores.meta ELSE EXCLUDED.meta END
		WHERE task_stores.root IS DISTINCT FROM EXCLUDED.root OR (EXCLUDED.meta<>'null'::jsonb AND task_stores.meta IS DISTINCT FROM EXCLUDED.meta)`, projectID, snapshot.Root, meta); err != nil {
		return out, err
	}
	seen := []int64{}
	handoffs := []string{}
	for _, file := range snapshot.Files {
		dir, name := path.Split(file.Name)
		var number int64
		switch dir {
		case "tasks/":
			number, err = strconv.ParseInt(strings.TrimSuffix(name, ".json"), 10, 64)
			if err != nil || number <= 0 || !strings.HasSuffix(name, ".json") {
				out.Errors = append(out.Errors, file.Name+": invalid task filename")
				continue
			}
			seen = append(seen, number)
		case "handoff/":
			handoffs = append(handoffs, name)
		default:
			out.Errors = append(out.Errors, file.Name+": unknown store file")
			continue
		}
		if file.Error != "" {
			out.Errors = append(out.Errors, file.Name+": "+file.Error)
			continue
		}
		// A malformed source or an unsupported jsonb value affects only this
		// file; PostgreSQL errors otherwise poison the entire transaction.
		part, beginErr := tx.Begin(ctx)
		if beginErr != nil {
			return out, beginErr
		}
		changed := false
		if dir == "tasks/" {
			var task taskreg.Task
			task, err = taskreg.ParseTask(file.Raw)
			if err == nil && task.Number != number {
				err = errors.New("the task number differs from its filename")
			}
			if err == nil {
				changed, err = importTask(ctx, part, projectID, task, file.Raw)
			}
			if err == nil {
				out.Tasks++
			}
		} else {
			var h taskreg.Handoff
			h, err = taskreg.ParseHandoff(name, file.Raw)
			if err == nil {
				changed, err = importHandoff(ctx, part, projectID, h, file.Raw)
			}
			if err == nil {
				out.Handoffs++
			}
		}
		if err != nil {
			if rollbackErr := part.Rollback(ctx); rollbackErr != nil {
				return out, rollbackErr
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", file.Name, err))
			continue
		}
		if err = part.Commit(ctx); err != nil {
			return out, err
		}
		if changed {
			out.Changed++
		}
	}
	if snapshot.TasksComplete {
		tag, e := tx.Exec(ctx, `UPDATE project_tasks SET deleted_at=now() WHERE project_id=$1 AND NOT(number=ANY($2::bigint[])) AND deleted_at IS NULL`, projectID, seen)
		if e != nil {
			return out, e
		}
		out.Deleted = tag.RowsAffected()
	}
	if snapshot.HandoffsComplete {
		if _, err = tx.Exec(ctx, `UPDATE project_handoffs SET deleted_at=now() WHERE project_id=$1 AND NOT(file=ANY($2::text[])) AND deleted_at IS NULL`, projectID, handoffs); err != nil {
			return out, err
		}
	}
	err = tx.Commit(ctx)
	return out, err
}

func importTask(ctx context.Context, tx pgx.Tx, project int, t taskreg.Task, raw string) (bool, error) {
	var oldStatus string
	var oldUpdated *time.Time
	err := tx.QueryRow(ctx, `SELECT status,updated_at FROM project_tasks WHERE project_id=$1 AND number=$2`, project, t.Number).Scan(&oldStatus, &oldUpdated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO project_tasks(project_id,number,title,status,priority,position,tags,blocked_by,external,external_refs,external_url,
		created_at,updated_at,closed_at,created_by,closed_by,touched_by,session,body,markdown,source,source_json)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		ON CONFLICT(project_id,number) DO UPDATE SET title=EXCLUDED.title,status=EXCLUDED.status,priority=EXCLUDED.priority,
		position=EXCLUDED.position,tags=EXCLUDED.tags,blocked_by=EXCLUDED.blocked_by,external=EXCLUDED.external,
		external_refs=EXCLUDED.external_refs,external_url=EXCLUDED.external_url,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at,
		closed_at=EXCLUDED.closed_at,created_by=EXCLUDED.created_by,closed_by=EXCLUDED.closed_by,touched_by=EXCLUDED.touched_by,
		session=EXCLUDED.session,body=EXCLUDED.body,markdown=EXCLUDED.markdown,source=EXCLUDED.source,source_json=EXCLUDED.source_json,
		imported_at=now(),deleted_at=NULL WHERE project_tasks.source IS DISTINCT FROM EXCLUDED.source OR project_tasks.deleted_at IS NOT NULL`,
		project, t.Number, t.Title, t.Status, t.Priority, t.Order, t.Tags, t.BlockedBy, t.External, t.ExternalRefs, t.ExternalURL,
		t.CreatedAt, t.UpdatedAt, t.ClosedAt, t.CreatedBy, t.ClosedBy, t.TouchedBy, t.Session, t.Body, t.Markdown, raw, t.Source)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(t.Source, &fields); err != nil {
		return false, err
	}
	for _, fact := range []struct{ key, kind, actor string }{{"created", "created", t.CreatedBy}, {"updated", "changed", t.Session}, {"closed", "closed", t.ClosedBy}} {
		var date string
		_ = json.Unmarshal(fields[fact.key], &date)
		at, precision, e := taskreg.Date(date)
		if e != nil {
			return false, e
		}
		if at == nil || (fact.kind == "changed" && t.CreatedAt != nil && at.Equal(*t.CreatedAt)) {
			continue
		}
		payload := json.RawMessage(`{}`)
		if fact.kind == "closed" && (t.Status == "done" || t.Status == "cancelled") {
			payload, _ = json.Marshal(map[string]string{"status": t.Status})
		}
		key := fact.key
		if fact.kind != "created" {
			// A later edit cannot move an already observed fact out of a past
			// report. The source dates only one last edit per session and day.
			key += "//" + date + "//" + fact.actor
		}
		if err = importEvent(ctx, tx, project, t.Number, key, taskreg.Event{Kind: fact.kind, At: *at, Precision: precision, Actor: fact.actor, Payload: payload}); err != nil {
			return false, err
		}
	}
	if oldStatus != "" && oldStatus != t.Status && t.UpdatedAt != nil && (oldUpdated == nil || !t.UpdatedAt.Before(*oldUpdated)) {
		var date string
		_ = json.Unmarshal(fields["updated"], &date)
		_, precision, _ := taskreg.Date(date)
		payload, _ := json.Marshal(map[string]string{"from": oldStatus, "to": t.Status})
		key := fmt.Sprintf("status:%x", sha256.Sum256([]byte(raw)))
		if err = importEvent(ctx, tx, project, t.Number, key, taskreg.Event{Kind: "status_changed", At: *t.UpdatedAt, Precision: precision, Actor: t.Session, Payload: payload}); err != nil {
			return false, err
		}
	}
	return true, nil
}

func importEvent(ctx context.Context, tx pgx.Tx, project int, number int64, key string, e taskreg.Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO task_events(project_id,task_number,kind,at,precision,actor,origin,source_key,payload)
		VALUES($1,$2,$3,$4,$5,$6,'cctm',$7,$8) ON CONFLICT(project_id,task_number,source_key)
		DO UPDATE SET kind=EXCLUDED.kind,at=EXCLUDED.at,precision=EXCLUDED.precision,actor=EXCLUDED.actor,payload=EXCLUDED.payload
		WHERE (task_events.kind,task_events.at,task_events.precision,task_events.actor,task_events.payload)
		IS DISTINCT FROM (EXCLUDED.kind,EXCLUDED.at,EXCLUDED.precision,EXCLUDED.actor,EXCLUDED.payload)`, project, number, e.Kind, e.At, e.Precision, e.Actor, key, e.Payload)
	return err
}

func importHandoff(ctx context.Context, tx pgx.Tx, project int, h taskreg.Handoff, raw string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO project_handoffs(project_id,file,key,session,kind,at,date,title,cwd,git,tasks,task_numbers,body,source,source_json)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT(project_id,file) DO UPDATE SET key=EXCLUDED.key,session=EXCLUDED.session,kind=EXCLUDED.kind,at=EXCLUDED.at,date=EXCLUDED.date,
		title=EXCLUDED.title,cwd=EXCLUDED.cwd,git=EXCLUDED.git,tasks=EXCLUDED.tasks,task_numbers=EXCLUDED.task_numbers,body=EXCLUDED.body,
		source=EXCLUDED.source,source_json=EXCLUDED.source_json,imported_at=now(),deleted_at=NULL
		WHERE project_handoffs.source IS DISTINCT FROM EXCLUDED.source OR project_handoffs.deleted_at IS NOT NULL`,
		project, h.File, h.Key, h.Session, h.Kind, h.At, h.Date, h.Title, h.CWD, h.Git, h.Tasks, h.TaskNumbers, h.Body, raw, h.Source)
	return tag.RowsAffected() > 0, err
}
