package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"aacpanel/internal/taskreg"
	"github.com/jackc/pgx/v5"
)

type TaskSummary struct {
	Text      string    `json:"text"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"createdAt"`
}

type SummarySource struct {
	Records     map[string][]SummaryEvent
	Fingerprint string
	Empty       bool
	Actors      []string
}

type SummaryEvent struct {
	Project string        `json:"project"`
	Number  int64         `json:"number"`
	Title   string        `json:"title"`
	URL     string        `json:"url,omitempty"`
	Issues  []string      `json:"issues"`
	Event   taskreg.Event `json:"event"`
}

// A single ordered read covers the whole period, including deleted tasks.
// The fingerprint uses recorded facts before names are resolved for display:
// archive availability must not change a period's event identity.
func (s *Store) TaskSummarySource(ctx context.Context, f TaskFilter) (out SummarySource, err error) {
	defer func() { err = Unavailable(err) }()
	if f.ContourID <= 0 || f.From == nil || f.Until == nil || !f.From.Before(*f.Until) {
		return out, badRequest("a summary needs a contour and an increasing period")
	}
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	rows, err := pool.Query(ctx, `SELECT p.name,t.number,t.title,t.external_url,t.external_refs,
        e.id,e.kind,e.at,e.precision,e.actor,e.actor_name,e.origin,e.payload
        FROM task_events e JOIN project_tasks t ON t.project_id=e.project_id AND t.number=e.task_number
        JOIN profile_projects p ON p.id=t.project_id JOIN profile_groups g ON g.id=p.group_id
        WHERE g.profile_id=$1 AND e.at >= $2 AND e.at < $3
        ORDER BY e.at,e.id`, f.ContourID, *f.From, *f.Until)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	groups := map[string][]SummaryEvent{}
	actors := map[string]bool{}
	size := 0
	for rows.Next() {
		var row SummaryEvent
		e := &row.Event
		if err = rows.Scan(&row.Project, &row.Number, &row.Title, &row.URL, &row.Issues,
			&e.ID, &e.Kind, &e.At, &e.Precision, &e.Actor, &e.ActorName, &e.Origin, &e.Payload); err != nil {
			return out, err
		}
		e.At = e.At.UTC()
		if e.Actor != "" && !actors[e.Actor] {
			actors[e.Actor] = true
			out.Actors = append(out.Actors, e.Actor)
		}
		refs := row.Issues
		if len(refs) == 0 {
			refs = []string{"Other work"}
		}
		raw, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return out, marshalErr
		}
		size += (len(raw) + 1) * len(refs)
		if size > 240<<10 {
			return out, badRequest("the period has too much activity for a summary; choose a shorter period")
		}
		for _, ref := range refs {
			groups[ref] = append(groups[ref], row)
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.Empty = len(groups) == 0
	out.Records = groups
	raw, err := json.Marshal(groups)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out, nil
}

func (s *Store) TaskSummary(ctx context.Context, f TaskFilter, fingerprint string) (out *TaskSummary, err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	var row TaskSummary
	err = pool.QueryRow(ctx, `SELECT text,model,created_at FROM task_summaries
        WHERE contour_id=$1 AND period_start=$2 AND period_end=$3 AND fingerprint=$4`, f.ContourID, f.From, f.Until, fingerprint).Scan(&row.Text, &row.Model, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	row.CreatedAt = row.CreatedAt.UTC()
	return &row, err
}

func (s *Store) SaveTaskSummary(ctx context.Context, f TaskFilter, fingerprint, text, model string) (out TaskSummary, err error) {
	defer func() { err = Unavailable(err) }()
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	out.Text, out.Model = text, model
	err = pool.QueryRow(ctx, `INSERT INTO task_summaries(contour_id,period_start,period_end,fingerprint,text,model)
        VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(contour_id,period_start,period_end,fingerprint)
        DO UPDATE SET text=EXCLUDED.text,model=EXCLUDED.model,created_at=clock_timestamp()
        RETURNING created_at`, f.ContourID, f.From, f.Until, fingerprint, text, model).Scan(&out.CreatedAt)
	out.CreatedAt = out.CreatedAt.UTC()
	return out, err
}
