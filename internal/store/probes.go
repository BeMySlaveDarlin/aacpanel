package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ProbeState is a probe together with its latest result.
type ProbeState struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Runner     string `json:"runner"`
	Enabled    bool   `json:"enabled"`
	Interval   int    `json:"intervalSec"`
	Last       *Check `json:"last"`
	FailStreak int    `json:"failStreak"`
	LastOK     *int64 `json:"lastOk"`
}

// Check is one probe result.
type Check struct {
	TS        int64   `json:"ts"`
	OK        bool    `json:"ok"`
	Outcome   string  `json:"outcome"`
	LatencyMS *int    `json:"latencyMs"`
	Error     *string `json:"error"`
}

// Probes returns the state of every probe.
func (s *Store) Probes(ctx context.Context) (out []ProbeState, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		WITH recent AS (
			SELECT r.probe_id, r.ts, r.ok, r.outcome, r.latency_ms, r.error,
			       row_number() OVER (PARTITION BY r.probe_id ORDER BY r.ts DESC) AS rn,
			       sum(CASE WHEN r.ok THEN 1 ELSE 0 END)
			           OVER (PARTITION BY r.probe_id ORDER BY r.ts DESC) AS ok_after
			FROM probe_results r
			WHERE r.ts >= now() - interval '1 day'
		)
		SELECT p.id, p.name, p.kind, p.target, p.runner, p.enabled, p.interval_sec,
		       last.ts, last.ok, last.outcome, last.latency_ms, last.error,
		       coalesce(streak.n, 0), lastok.ts
		FROM probes p
		LEFT JOIN recent last ON last.probe_id = p.id AND last.rn = 1
		LEFT JOIN LATERAL (
			SELECT count(*) AS n FROM recent
			WHERE probe_id = p.id AND NOT ok AND ok_after = 0
		) streak ON true
		LEFT JOIN LATERAL (
			SELECT max(ts) AS ts FROM recent WHERE probe_id = p.id AND ok
		) lastok ON true
		ORDER BY p.runner, p.name`)
	if err != nil {
		return nil, err
	}

	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ProbeState, error) {
		var p ProbeState
		var ts *time.Time
		var ok *bool
		var outcome *string
		var latency *int
		var errText *string
		var lastOK *time.Time
		if err := r.Scan(&p.ID, &p.Name, &p.Kind, &p.Target, &p.Runner, &p.Enabled, &p.Interval,
			&ts, &ok, &outcome, &latency, &errText, &p.FailStreak, &lastOK); err != nil {
			return p, err
		}
		if lastOK != nil {
			u := lastOK.Unix()
			p.LastOK = &u
		}
		if ts != nil && ok != nil && outcome != nil {
			p.Last = &Check{TS: ts.Unix(), OK: *ok, Outcome: *outcome, LatencyMS: latency, Error: errText}
		}
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []ProbeState{}
	}
	return list, nil
}
