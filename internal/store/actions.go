package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// ActionOK — the executor reported success.
	ActionOK = "ok"
	// ActionFailed — it tried and could not.
	ActionFailed = "failed"
	// ActionTimeout — it did not answer within the time allowed.
	ActionTimeout = "timeout"
	// ActionRejected — it refused the command.
	ActionRejected = "rejected"
)

// ErrActionFinished means the outcome has already been written.
var ErrActionFinished = errors.New("the action outcome is already written")

// Action is a record of the log.
type Action struct {
	ID         int64          `json:"id"`
	TS         time.Time      `json:"ts"`
	DeviceID   *int64         `json:"device_id"`
	DeviceName string         `json:"device_name"`
	Kind       string         `json:"kind"`
	Target     string         `json:"target"`
	Params     map[string]any `json:"params"`
	Result     string         `json:"result"`
	Error      string         `json:"error,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	DurationMS *int           `json:"duration_ms"`
	FinishedAt *time.Time     `json:"finished_at"`
}

// ActionOutcome is how it ended.
type ActionOutcome struct {
	Result   string
	Error    string
	Detail   string
	Duration time.Duration
}

func (o ActionOutcome) valid() error {
	switch o.Result {
	case ActionOK, ActionFailed, ActionTimeout, ActionRejected:
		return nil
	default:
		return badRequest("unknown action outcome %q", o.Result)
	}
}

// LogAttempt creates a record of an action that has begun and returns its id.
func (s *Store) LogAttempt(ctx context.Context, a Action) (newID int64, err error) {
	defer func() { err = Unavailable(err) }()

	if a.Kind == "" || a.Target == "" {
		return 0, badRequest("an action without a kind or a target is not written to the log")
	}
	if a.DeviceName == "" {
		return 0, badRequest("an action without a device is not written to the log")
	}
	pool, err := s.Pool()
	if err != nil {
		return 0, err
	}
	params := a.Params
	if params == nil {
		params = map[string]any{}
	}

	var id int64
	err = pool.QueryRow(ctx, `
		INSERT INTO actions (device_id, device_name, kind, target, params)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		a.DeviceID, a.DeviceName, a.Kind, a.Target, params).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("writing the action: %w", err)
	}
	return id, nil
}

// LogResult appends the outcome.
func (s *Store) LogResult(ctx context.Context, id int64, out ActionOutcome) (err error) {
	defer func() { err = Unavailable(err) }()

	if err := out.valid(); err != nil {
		return err
	}
	pool, err := s.Pool()
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, `
		UPDATE actions
		   SET result = $2, error = $3, detail = $4, duration_ms = $5, finished_at = now()
		 WHERE id = $1`,
		id, out.Result, nullable(out.Error), nullable(out.Detail),
		int(out.Duration.Milliseconds()))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23001" {
			return fmt.Errorf("%w: action %d", ErrActionFinished, id)
		}
		return fmt.Errorf("the action outcome: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("there is no action %d in the log", id)
	}
	return nil
}

// CloseStaleAttachments closes the terminal attachments left without a partner.
func (s *Store) CloseStaleAttachments(ctx context.Context) (n int64, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return 0, err
	}
	return closeStaleAttachments(ctx, pool)
}

func closeStaleAttachments(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE actions
		   SET result = $1, detail = $2, finished_at = now()
		 WHERE kind = 'term.attach' AND result IS NULL`,
		ActionFailed, "the bridge was cut by a service restart")
	if err != nil {
		return 0, fmt.Errorf("closing stale terminal attachments: %w", err)
	}
	return tag.RowsAffected(), nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ActionsReq says what to show.
type ActionsReq struct {
	Limit  int
	Before int64
	Result string
}

// ActionResults are the outcomes the log can filter by.
var ActionResults = []string{"ok", "failed", "pending"}

// Actions reads the log.
func (s *Store) Actions(ctx context.Context, req ActionsReq) (out []Action, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	query := `
		SELECT id, ts, device_id, device_name, kind, target, params,
		       coalesce(result, ''), coalesce(error, ''), coalesce(detail, ''),
		       duration_ms, finished_at
		  FROM actions`
	args := []any{limit}
	where := []string{}
	if req.Before > 0 {
		args = append(args, req.Before)
		where = append(where, fmt.Sprintf("id < $%d", len(args)))
	}
	switch req.Result {
	case "":
	case "pending":
		where = append(where, "result IS NULL")
	case "ok", "failed", "timeout":
		args = append(args, req.Result)
		where = append(where, fmt.Sprintf("result = $%d", len(args)))
	default:
		return nil, badRequest("unknown outcome %q", req.Result)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY id DESC LIMIT $1`

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("the action log: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Action, error) {
		var a Action
		err := row.Scan(&a.ID, &a.TS, &a.DeviceID, &a.DeviceName, &a.Kind, &a.Target,
			&a.Params, &a.Result, &a.Error, &a.Detail, &a.DurationMS, &a.FinishedAt)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("the action log: %w", err)
	}
	return list, nil
}

// ActedOn answers whether this target has been touched recently.
func (s *Store) ActedOn(ctx context.Context, target string, within time.Duration) (acted bool, err error) {
	defer func() { err = Unavailable(err) }()

	if target == "" || within <= 0 {
		return false, badRequest("a target and a time window are required")
	}
	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	var found bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM actions
		     WHERE target = $1 AND ts >= now() - $2::interval
		)`, target, within.String()).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("actions on %q: %w", target, err)
	}
	return found, nil
}
