package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Alert is an alert for reading.
type Alert struct {
	ID        int64           `json:"id"`
	Rule      string          `json:"rule"`
	Subject   string          `json:"subject"`
	Severity  string          `json:"severity"`
	OpenedAt  int64           `json:"openedAt"`
	ClosedAt  *int64          `json:"closedAt"`
	AckedAt   *int64          `json:"ackedAt"`
	LastSeen  int64           `json:"lastSeen"`
	Value     *float64        `json:"value"`
	Worst     *float64        `json:"worst"`
	Payload   json.RawMessage `json:"payload"`
	Suggested *Suggestion     `json:"suggested"`
}

// Suggestion is a suggested action pulled out of the payload.
type Suggestion struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

type AlertsReq struct {
	Limit  int
	Before int64
}

// Alerts reads the alerts: the open ones and the recently closed ones.
func (s *Store) Alerts(ctx context.Context, req AlertsReq) (out []Alert, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	rows, err := pool.Query(ctx, `
		SELECT a.id, r.name, a.subject, a.severity,
		       a.opened_at, a.closed_at, a.acknowledged_at, a.last_seen,
		       a.value, a.worst, a.payload
		FROM alerts a
		JOIN rules r ON r.id = a.rule_id
		WHERE $2 = 0 OR (a.closed_at IS NOT NULL AND a.id < $2)
		ORDER BY (a.closed_at IS NULL) DESC, a.id DESC
		LIMIT $1`, limit, req.Before)
	if err != nil {
		return nil, err
	}

	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) {
		var a Alert
		var opened, lastSeen time.Time
		var closed, acked *time.Time
		if err := r.Scan(&a.ID, &a.Rule, &a.Subject, &a.Severity,
			&opened, &closed, &acked, &lastSeen, &a.Value, &a.Worst, &a.Payload); err != nil {
			return a, err
		}
		a.OpenedAt, a.LastSeen = opened.Unix(), lastSeen.Unix()
		if closed != nil {
			ts := closed.Unix()
			a.ClosedAt = &ts
		}
		if acked != nil {
			ts := acked.Unix()
			a.AckedAt = &ts
		}
		a.Suggested = suggestionFrom(a.Payload)
		return a, nil
	})
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []Alert{}
	}
	return list, nil
}

func suggestionFrom(payload json.RawMessage) *Suggestion {
	if len(payload) == 0 {
		return nil
	}
	var body struct {
		Suggest *Suggestion `json:"suggest"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil
	}
	return body.Suggest
}

// Ack marks an alert as seen.
func (s *Store) Ack(ctx context.Context, id int64) (ok bool, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return false, err
	}
	tag, err := pool.Exec(ctx, `
		UPDATE alerts SET acknowledged_at = now()
		WHERE id = $1 AND closed_at IS NULL AND acknowledged_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
