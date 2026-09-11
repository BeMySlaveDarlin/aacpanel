package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionsHistory is the archive of claude sessions over a period.
type SessionsHistory struct {
	Resolution string `json:"resolution"`
	StepSec    int    `json:"stepSec"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`

	T      []int64    `json:"t"`
	Live   []*int     `json:"live"`
	PctMax []*float64 `json:"pctMax"`

	Rows   []SessionRow `json:"rows"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
	Total  int          `json:"total"`
}

// SessionRow is one session over the period.
type SessionRow struct {
	Name        string  `json:"name"`
	PctMax      float64 `json:"pctMax"`
	PctAvg      float64 `json:"pctAvg"`
	TokensMax   int64   `json:"tokensMax"`
	MessagesMax int     `json:"messagesMax"`
	Samples     int     `json:"samples"`
	FirstSeen   int64   `json:"firstSeen"`
	LastSeen    int64   `json:"lastSeen"`
	SessionID   string  `json:"sessionId,omitempty"`
	CWD         string  `json:"cwd,omitempty"`
}

// SessionsReq says for which period, at which resolution and how many rows.
type SessionsReq struct {
	HostID     int
	From, To   time.Time
	MaxPoints  int
	Limit      int
	Offset     int
	Resolution string
}

const (
	sessionRowsDefault = 20
	sessionRowsLimit   = 100
)

// SessionsFor returns the session history for a period.
func (s *Store) SessionsFor(ctx context.Context, req SessionsReq) (*SessionsHistory, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	from, to := req.From, req.To

	res := req.Resolution
	if res == "" {
		res = s.pickRollupRes(ctx, "sessions_1m", 14*24*time.Hour, from, to)
	}
	var table string
	switch res {
	case Res1m:
		table = "sessions_1m"
	case Res1h:
		table = "sessions_1h"
	default:
		return nil, badRequest("resolution %q is not supported for sessions: there are %s and %s", res, Res1m, Res1h)
	}

	limit := req.Limit
	if limit <= 0 || limit > sessionRowsLimit {
		limit = sessionRowsDefault
	}

	step := stepFor(res, from, to, req.MaxPoints)
	out := &SessionsHistory{
		Resolution: res, StepSec: int(step.Seconds()),
		From: from.Unix(), To: to.Unix(),
		T: []int64{}, Live: []*int{}, PctMax: []*float64{},
		Rows: []SessionRow{},
	}

	out.Limit, out.Offset = limit, max(req.Offset, 0)

	if err := s.sessionRows(ctx, pool, out, table, req, limit); err != nil {
		return nil, err
	}
	if err := s.sessionSeries(ctx, pool, out, table, req, step); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) sessionRows(ctx context.Context, pool *pgxpool.Pool, out *SessionsHistory, table string, req SessionsReq, limit int) error {
	q := fmt.Sprintf(`
		SELECT name, max(pct_max)::float8,
		       (sum(pct_avg::float8 * samples) / nullif(sum(samples), 0))::float8,
		       max(tokens_max), max(messages_max), sum(samples)::int,
		       extract(epoch FROM min(bucket))::bigint,
		       extract(epoch FROM max(bucket))::bigint,
		       (array_agg(session_id ORDER BY bucket DESC) FILTER (WHERE session_id IS NOT NULL))[1],
		       (array_agg(cwd ORDER BY bucket DESC) FILTER (WHERE cwd IS NOT NULL))[1],
		       count(*) OVER () AS total
		FROM %s WHERE host_id = $1 AND bucket >= $2 AND bucket < $3
		GROUP BY name HAVING sum(samples) > 0
		ORDER BY max(bucket) DESC, name
		LIMIT %d OFFSET %d`, table, limit, out.Offset)

	rows, err := pool.Query(ctx, q, req.HostID, req.From, req.To)
	if err != nil {
		return Unavailable(err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SessionRow, error) {
		var row SessionRow
		var peak, avg *float64
		var tokens *int64
		var messages *int
		var sessionID, cwd *string
		var total int
		if err := r.Scan(&row.Name, &peak, &avg, &tokens, &messages,
			&row.Samples, &row.FirstSeen, &row.LastSeen, &sessionID, &cwd, &total); err != nil {
			return row, err
		}
		if sessionID != nil {
			row.SessionID = *sessionID
		}
		if cwd != nil {
			row.CWD = *cwd
		}
		if peak != nil {
			row.PctMax = *peak
		}
		if avg != nil {
			row.PctAvg = *avg
		}
		if tokens != nil {
			row.TokensMax = *tokens
		}
		if messages != nil {
			row.MessagesMax = *messages
		}
		out.Total = total
		return row, nil
	})
	if err != nil {
		return Unavailable(err)
	}
	if list != nil {
		out.Rows = list
	}

	if len(list) == 0 && out.Offset > 0 {
		q := fmt.Sprintf(`
			SELECT count(*) FROM (
				SELECT name FROM %s WHERE host_id = $1 AND bucket >= $2 AND bucket < $3
				GROUP BY name HAVING sum(samples) > 0
			) g`, table)
		if err := pool.QueryRow(ctx, q, req.HostID, req.From, req.To).Scan(&out.Total); err != nil {
			return Unavailable(err)
		}
	}
	return nil
}

func (s *Store) sessionSeries(ctx context.Context, pool *pgxpool.Pool, out *SessionsHistory, table string, req SessionsReq, step time.Duration) error {
	q := fmt.Sprintf(`
		WITH d AS (
			SELECT date_bin($1, bucket, timestamptz '%s') AS b,
			       count(DISTINCT name)::int AS live,
			       max(pct_max)::float8      AS peak
			FROM %s
			WHERE host_id = $2 AND bucket >= $3 AND bucket < $4
			GROUP BY 1
		), span AS (
			SELECT min(b) AS lo, max(b) AS hi FROM d
		)
		SELECT extract(epoch FROM g)::bigint, d.live, d.peak
		FROM span
		CROSS JOIN LATERAL generate_series(span.lo, span.hi, $1) AS g
		LEFT JOIN d ON d.b = g
		ORDER BY 1`, binOrigin, table)

	rows, err := pool.Query(ctx, q, step, req.HostID, req.From, req.To)
	if err != nil {
		return Unavailable(err)
	}
	defer rows.Close()

	for rows.Next() {
		var t int64
		var live *int
		var peak *float64
		if err := rows.Scan(&t, &live, &peak); err != nil {
			return Unavailable(err)
		}
		out.T = append(out.T, t)
		out.Live = append(out.Live, live)
		out.PctMax = append(out.PctMax, peak)
	}
	return Unavailable(rows.Err())
}

// SessionResume returns the identifier and directory to resume a session with.
func (s *Store) SessionResume(ctx context.Context, hostID int, name string) (sessionID, cwd string, err error) {
	pool, err := s.Pool()
	if err != nil {
		return "", "", err
	}
	for _, table := range []string{"sessions_1m", "sessions_1h"} {
		var sid, dir *string
		q := `SELECT session_id, cwd FROM ` + table + `
		      WHERE host_id = $1 AND name = $2 AND session_id IS NOT NULL
		      ORDER BY bucket DESC LIMIT 1`
		switch err := pool.QueryRow(ctx, q, hostID, name).Scan(&sid, &dir); {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return "", "", Unavailable(err)
		}
		if sid != nil && *sid != "" {
			if dir != nil {
				cwd = *dir
			}
			return *sid, cwd, nil
		}
	}
	return "", "", nil
}
