package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/taskreg"
	"github.com/jackc/pgx/v5"
)

type TaskFilter struct {
	ContourID      int
	ProjectID      int
	Statuses       []string
	Tag            string
	Text           string
	External       string
	IncludeDeleted bool
	Limit          int
	Offset         int
	From           *time.Time
	Until          *time.Time
}

type TaskPage struct {
	Tasks  []taskreg.Task `json:"tasks"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

const taskJoins = ` FROM project_tasks t JOIN profile_projects p ON p.id=t.project_id
	JOIN profile_groups g ON g.id=p.group_id JOIN profiles c ON c.id=g.profile_id `
const taskColumns = `t.project_id,p.name,c.id,c.name,t.number,t.title,t.status,t.priority,t.position,t.tags,t.blocked_by,
	t.external,t.external_refs,t.external_url,t.created_at,t.updated_at,t.closed_at,t.created_by,t.closed_by,t.touched_by,t.session,
	t.body,t.markdown,t.source_json,t.imported_at,t.deleted_at`

func taskWhere(f TaskFilter) (string, []any, error) {
	parts := []string{"true"}
	args := []any{}
	add := func(expr string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf(expr, len(args)))
	}
	if f.ContourID < 0 || f.ProjectID < 0 || f.Offset < 0 {
		return "", nil, badRequest("task ids and offsets must not be negative")
	}
	if f.ContourID > 0 {
		add("c.id=$%d", f.ContourID)
	}
	if f.ProjectID > 0 {
		add("p.id=$%d", f.ProjectID)
	}
	if !f.IncludeDeleted {
		parts = append(parts, "t.deleted_at IS NULL")
	}
	if len(f.Statuses) > 0 {
		for _, status := range f.Statuses {
			if !slices.Contains([]string{"open", "deferred", "done", "cancelled"}, status) {
				return "", nil, badRequest("unknown task status %q", status)
			}
		}
		add("t.status=ANY($%d::text[])", f.Statuses)
	}
	if f.Tag != "" {
		add("$%d=ANY(t.tags)", f.Tag)
	}
	if f.Text != "" {
		add("strpos(lower(t.title || E'\\n' || t.markdown),lower($%d))>0", f.Text)
	}
	if f.External != "" {
		add("upper($%d)=ANY(t.external_refs)", f.External)
	}
	if (f.From == nil) != (f.Until == nil) {
		return "", nil, badRequest("both period boundaries are required")
	}
	if f.From != nil {
		if !f.From.Before(*f.Until) {
			return "", nil, badRequest("the period must end after it starts")
		}
		args = append(args, *f.From, *f.Until)
		parts = append(parts, fmt.Sprintf(`EXISTS(SELECT 1 FROM task_events e WHERE e.project_id=t.project_id AND e.task_number=t.number AND e.at >= $%d AND e.at < $%d)`, len(args)-1, len(args)))
	}
	return " WHERE " + strings.Join(parts, " AND "), args, nil
}

func scanTask(row pgx.CollectableRow) (taskreg.Task, error) {
	var t taskreg.Task
	err := row.Scan(&t.ProjectID, &t.Project, &t.ContourID, &t.Contour, &t.Number, &t.Title, &t.Status, &t.Priority, &t.Order, &t.Tags, &t.BlockedBy,
		&t.External, &t.ExternalRefs, &t.ExternalURL, &t.CreatedAt, &t.UpdatedAt, &t.ClosedAt, &t.CreatedBy, &t.ClosedBy, &t.TouchedBy, &t.Session,
		&t.Body, &t.Markdown, &t.Source, &t.ImportedAt, &t.DeletedAt)
	taskTimesUTC(t.CreatedAt, t.UpdatedAt, t.ClosedAt, &t.ImportedAt, t.DeletedAt)
	return t, err
}

func taskTimesUTC(values ...*time.Time) {
	for _, value := range values {
		if value != nil {
			*value = value.UTC()
		}
	}
}

func (s *Store) Tasks(ctx context.Context, f TaskFilter) (out TaskPage, err error) {
	defer func() { err = Unavailable(err) }()
	out = TaskPage{Tasks: []taskreg.Task{}, Limit: f.Limit, Offset: f.Offset}
	if out.Limit <= 0 {
		out.Limit = 100
	}
	if out.Limit > 500 {
		out.Limit = 500
	}
	where, args, err := taskWhere(f)
	if err != nil {
		return out, err
	}
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	if err = pool.QueryRow(ctx, `SELECT count(*)`+taskJoins+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	args = append(args, out.Limit, out.Offset)
	rows, err := pool.Query(ctx, `SELECT `+taskColumns+taskJoins+where+fmt.Sprintf(` ORDER BY c.sort,c.id,g.sort,g.id,p.sort,p.id,t.position NULLS LAST,t.number LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return out, err
	}
	out.Tasks, err = pgx.CollectRows(rows, scanTask)
	return out, err
}

func (s *Store) Task(ctx context.Context, project int, number int64) (out taskreg.Detail, err error) {
	defer func() { err = Unavailable(err) }()
	out.Events = []taskreg.Event{}
	out.Handoffs = []taskreg.Handoff{}
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	out.Task, err = scanTask(rowOnly{pool.QueryRow(ctx, `SELECT `+taskColumns+taskJoins+` WHERE t.project_id=$1 AND t.number=$2`, project, number)})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.Events, err = s.taskEvents(ctx, project, number, nil, nil)
	if err != nil {
		return out, err
	}
	rows, err := pool.Query(ctx, `SELECT file,key,session,kind,at,date,title,cwd,git,tasks,task_numbers,body,source_json,deleted_at
		FROM project_handoffs WHERE project_id=$1 AND $2=ANY(task_numbers) ORDER BY at DESC NULLS LAST,file`, project, number)
	if err != nil {
		return out, err
	}
	out.Handoffs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (taskreg.Handoff, error) {
		var h taskreg.Handoff
		e := row.Scan(&h.File, &h.Key, &h.Session, &h.Kind, &h.At, &h.Date, &h.Title, &h.CWD, &h.Git, &h.Tasks, &h.TaskNumbers, &h.Body, &h.Source, &h.DeletedAt)
		taskTimesUTC(h.At, h.DeletedAt)
		return h, e
	})
	return out, err
}

func (s *Store) taskEvents(ctx context.Context, project int, number int64, from, until *time.Time) ([]taskreg.Event, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT id,kind,at,precision,actor,origin,payload FROM task_events
		WHERE project_id=$1 AND task_number=$2 AND ($3::timestamptz IS NULL OR at >= $3) AND ($4::timestamptz IS NULL OR at < $4) ORDER BY at,id`, project, number, from, until)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (taskreg.Event, error) {
		var e taskreg.Event
		err := row.Scan(&e.ID, &e.Kind, &e.At, &e.Precision, &e.Actor, &e.Origin, &e.Payload)
		e.At = e.At.UTC()
		return e, err
	})
}

type TaskActivity struct {
	Task   taskreg.Task    `json:"task"`
	Events []taskreg.Event `json:"events"`
}
type TaskPeriodGroup struct {
	External string         `json:"external"`
	Tasks    []TaskActivity `json:"tasks"`
}
type TaskPeriod struct {
	From       time.Time         `json:"from"`
	Until      time.Time         `json:"until"`
	Groups     []TaskPeriodGroup `json:"groups"`
	TotalTasks int               `json:"totalTasks"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
}

// TasksPeriod includes every kind of activity, even on tasks currently open.
// A task naming several issues appears under each; unlinked tasks use "".
func (s *Store) TasksPeriod(ctx context.Context, f TaskFilter) (out TaskPeriod, err error) {
	if f.ContourID <= 0 || f.From == nil || f.Until == nil {
		return out, badRequest("a contour and both period boundaries are required")
	}
	out = TaskPeriod{From: *f.From, Until: *f.Until, Groups: []TaskPeriodGroup{}}
	page, err := s.Tasks(ctx, f)
	if err != nil {
		return out, err
	}
	out.TotalTasks = page.Total
	out.Limit = page.Limit
	out.Offset = page.Offset
	groups := map[string][]TaskActivity{}
	for _, t := range page.Tasks {
		events, e := s.taskEvents(ctx, t.ProjectID, t.Number, f.From, f.Until)
		if e != nil {
			return out, Unavailable(e)
		}
		refs := t.ExternalRefs
		if len(refs) == 0 {
			refs = []string{""}
		}
		for _, ref := range refs {
			groups[ref] = append(groups[ref], TaskActivity{Task: t, Events: events})
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		out.Groups = append(out.Groups, TaskPeriodGroup{External: key, Tasks: groups[key]})
	}
	return out, nil
}
