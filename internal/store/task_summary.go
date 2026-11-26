package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
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
	Records     map[string][]SummaryTask
	Fingerprint string
	Empty       bool
	Actors      []string
}

type SummaryTask struct {
	Project string            `json:"project"`
	Number  int64             `json:"number"`
	Title   string            `json:"title"`
	URL     string            `json:"url,omitempty"`
	Events  []SummaryActivity `json:"events"`
}

type SummaryActivity struct {
	Kind    string                     `json:"kind"`
	At      time.Time                  `json:"at"`
	Actor   string                     `json:"actor,omitempty"`
	Changes map[string]json.RawMessage `json:"changes,omitempty"`
	From    string                     `json:"from,omitempty"`
	To      string                     `json:"to,omitempty"`
	ActorID string                     `json:"-"`
}

type SummaryEvent struct {
	projectID int
	Project   string        `json:"project"`
	Number    int64         `json:"number"`
	Title     string        `json:"title"`
	URL       string        `json:"url,omitempty"`
	Issues    []string      `json:"issues"`
	Event     taskreg.Event `json:"event"`
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
	rows, err := pool.Query(ctx, `SELECT p.id,p.name,t.number,t.title,t.external_url,t.external_refs,
        e.id,e.kind,e.at,e.precision,e.actor,e.actor_name,e.origin,e.payload
        FROM task_events e JOIN project_tasks t ON t.project_id=e.project_id AND t.number=e.task_number
        JOIN profile_projects p ON p.id=t.project_id JOIN profile_groups g ON g.id=p.group_id
        WHERE g.profile_id=$1 AND e.at >= $2 AND e.at < $3
        ORDER BY e.at,e.id`, f.ContourID, *f.From, *f.Until)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	events := []SummaryEvent{}
	for rows.Next() {
		var row SummaryEvent
		e := &row.Event
		if err = rows.Scan(&row.projectID, &row.Project, &row.Number, &row.Title, &row.URL, &row.Issues,
			&e.ID, &e.Kind, &e.At, &e.Precision, &e.Actor, &e.ActorName, &e.Origin, &e.Payload); err != nil {
			return out, err
		}
		e.At = e.At.UTC()
		events = append(events, row)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	return taskSummarySource(events)
}

const summaryRecordsBudget = 240 << 10

func taskSummarySource(events []SummaryEvent) (out SummarySource, err error) {
	groups := groupSummaryEvents(events)
	raw, err := json.Marshal(groups)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	out.Fingerprint = hex.EncodeToString(sum[:])
	out.Empty = len(events) == 0
	actors := map[string]bool{}
	for _, row := range events {
		e := row.Event
		if e.Actor != "" && !actors[e.Actor] {
			actors[e.Actor] = true
			out.Actors = append(out.Actors, e.Actor)
		}
	}
	// Cache identity covers the recorded bodies; only the model's copy is shortened.
	for _, limit := range []int{800, 300, 100, 0} {
		records, err := compactSummaryRecords(events, limit)
		if err != nil {
			return out, err
		}
		raw, err = json.Marshal(records)
		if err != nil {
			return out, err
		}
		if len(raw) <= summaryRecordsBudget {
			out.Records = records
			return out, nil
		}
	}
	return out, badRequest("the period has too much activity for a summary; choose a shorter period")
}

func groupSummaryEvents(events []SummaryEvent) map[string][]SummaryEvent {
	groups := map[string][]SummaryEvent{}
	for _, row := range events {
		refs := row.Issues
		if len(refs) == 0 {
			refs = []string{"Other work"}
		}
		for _, ref := range refs {
			groups[ref] = append(groups[ref], row)
		}
	}
	return groups
}

func compactSummaryRecords(events []SummaryEvent, limit int) (map[string][]SummaryTask, error) {
	type taskKey struct {
		project int
		number  int64
	}
	bodies := map[taskKey]string{}
	groups := map[string][]SummaryTask{}
	indices := map[string]map[taskKey]int{}
	for _, row := range events {
		key := taskKey{row.projectID, row.Number}
		var payload struct {
			Changes map[string]json.RawMessage `json:"changes"`
			From    string                     `json:"from"`
			To      string                     `json:"to"`
			Status  string                     `json:"status"`
		}
		if err := json.Unmarshal(row.Event.Payload, &payload); err != nil {
			return nil, err
		}
		changes := payload.Changes
		var body string
		if len(changes["body"]) > 0 && changes["body"][0] == '"' &&
			json.Unmarshal(changes["body"], &body) == nil {
			previous, seen := bodies[key]
			bodies[key] = body
			text := body
			if seen && strings.HasPrefix(body, previous) {
				text = body[len(previous):]
			} else if start := strings.LastIndex(body, "\n## "); start >= 0 {
				text = body[start+1:]
			}
			delete(changes, "body")
			if limit > 0 {
				runes := []rune(strings.TrimSpace(text))
				text = string(runes)
				if len(runes) > limit {
					text = string(runes[:limit]) + "…"
				}
				changes["appendedText"], _ = json.Marshal(text)
			}
		}
		activity := SummaryActivity{Kind: row.Event.Kind, At: row.Event.At, Actor: row.Event.ActorName,
			ActorID: row.Event.Actor, Changes: changes}
		if payload.From != payload.To {
			activity.From, activity.To = payload.From, payload.To
			delete(changes, "status")
		} else if row.Event.Kind == "closed" && payload.From == "" && payload.To == "" && payload.Status != "" {
			// Imported closures record only their terminal status, including cancellation.
			activity.To = payload.Status
		}
		refs := row.Issues
		if len(refs) == 0 {
			refs = []string{"Other work"}
		}
		for _, ref := range refs {
			if indices[ref] == nil {
				indices[ref] = map[taskKey]int{}
			}
			index, found := indices[ref][key]
			if !found {
				index = len(groups[ref])
				indices[ref][key] = index
				groups[ref] = append(groups[ref], SummaryTask{Project: row.Project, Number: row.Number, Title: row.Title, URL: row.URL})
			}
			groups[ref][index].Events = append(groups[ref][index].Events, activity)
		}
	}
	return groups, nil
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
