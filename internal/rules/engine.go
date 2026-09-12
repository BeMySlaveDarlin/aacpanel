package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/action"
	"aacpanel/internal/store"
)

const (
	checkEvery    = 30 * time.Second
	recoverWindow = time.Minute
	sampleStep    = 10 * time.Second
	stormLimit    = 5
	stormWindow   = time.Hour
	checkTimeout  = 30 * time.Second
)

type how int

const (
	perSample how = iota
	perWindow
)

type query struct {
	how     how
	table   string
	subject string
	value   string
	running string
	sql     string
	window  time.Duration
	suggest action.Kind
}

var queries = map[string]query{
	"container.cpu_pct":   {table: "metrics_container_raw", subject: "container", value: "cpu_pct"},
	"container.mem_pct":   {table: "metrics_container_raw", subject: "container", value: "mem_pct"},
	"container.mem_bytes": {table: "metrics_container_raw", subject: "container", value: "mem_bytes"},
	"container.unhealthy": {
		table: "metrics_container_raw", subject: "container",
		value: "(health = 'unhealthy')::int", suggest: action.ContainerRestart,
	},
	"stack.running_pct": {
		how: perWindow, table: "metrics_container_raw", subject: "stack",
		running: "state = 'running'", suggest: action.StackUp,
	},
	"host.cpu_pct":    {table: "metrics_host_raw", subject: "'host'", value: "cpu_pct"},
	"host.mem_pct":    {table: "metrics_host_raw", subject: "'host'", value: "100.0 * mem_used / nullif(mem_total, 0)"},
	"host.load1":      {table: "metrics_host_raw", subject: "'host'", value: "load1"},
	"host.swap_bytes": {table: "metrics_host_raw", subject: "'host'", value: "swap_used"},
	"host.cpu_temp":   {table: "metrics_host_raw", subject: "'host'", value: "cpu_temp"},
	"disk.used_pct":   {table: "metrics_disk_raw", subject: "mount", value: "100.0 * used / nullif(total, 0)"},
	"session.pct":     {table: "sessions_raw", subject: "name", value: "pct"},
	"probe.fail_streak": {
		how: perWindow, window: 2 * time.Hour,
		sql: `
			WITH r AS (
				SELECT p.name AS subject, res.ts, res.ok,
				       sum(CASE WHEN res.ok THEN 1 ELSE 0 END)
				           OVER (PARTITION BY res.probe_id ORDER BY res.ts DESC) AS ok_after
				FROM probe_results res
				JOIN probes p ON p.id = res.probe_id
				WHERE res.ts >= $1 AND p.enabled
			)
			SELECT subject,
			       count(*) FILTER (WHERE NOT ok AND ok_after = 0)::float8,
			       count(*) FILTER (WHERE NOT ok AND ok_after = 0)::float8,
			       count(*), min(ts)
			FROM r GROUP BY subject`,
	},
}

// Engine runs the alert lifecycle: it opens, extends and closes alerts.
type Engine struct {
	store    *store.Store
	hostName string
}

func NewEngine(s *store.Store, hostName string) *Engine {
	return &Engine{store: s, hostName: hostName}
}

// Result is the outcome of one pass.
type Result struct {
	Opened     int
	Closed     int
	Kept       int
	Suppressed int
}

// Run performs the check on a schedule.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(checkEvery)
	defer t.Stop()

	for {
		res, err := e.Once(ctx)
		switch {
		case err == nil, ctx.Err() != nil:
		case isUnavailable(err):
		default:
			log.Printf("rules: check: %v", err)
		}
		if res.Opened > 0 || res.Closed > 0 || res.Suppressed > 0 {
			log.Printf("rules: opened %d, closed %d, suppressed %d", res.Opened, res.Closed, res.Suppressed)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func isUnavailable(err error) bool {
	return errors.Is(err, store.ErrUnavailable) || errors.Is(err, store.ErrClosed)
}

// Once makes a single pass over all enabled rules.
func (e *Engine) Once(ctx context.Context) (Result, error) {
	var res Result

	pool, err := e.store.Pool()
	if err != nil {
		return res, err
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	hostID, err := e.store.HostID(ctx, e.hostName)
	if err != nil {
		return res, err
	}

	list, bad, err := Load(ctx, pool)
	if err != nil {
		return res, err
	}
	for _, e := range bad {
		log.Printf("rules: %v", e)
	}

	for _, rule := range Enabled(list) {
		r, err := e.check(ctx, pool, hostID, rule)
		if err != nil {
			return res, fmt.Errorf("rule %q: %w", rule.Name, err)
		}
		res.Opened += r.Opened
		res.Closed += r.Closed
		res.Kept += r.Kept
		res.Suppressed += r.Suppressed
	}
	return res, nil
}

type measurement struct {
	subject   string
	blind     bool
	held      bool
	recovered bool
	last      float64
}

func (e *Engine) check(ctx context.Context, pool *pgxpool.Pool, hostID int, rule Rule) (Result, error) {
	var res Result

	found, err := measure(ctx, pool, hostID, rule)
	if err != nil {
		return res, err
	}

	open, err := openAlerts(ctx, pool, rule.ID)
	if err != nil {
		return res, err
	}

	for _, m := range found {
		alert, isOpen := open[m.subject]
		switch {
		case m.blind:
		case m.held && !isOpen:
			ok, err := e.open(ctx, pool, rule, m)
			if err != nil {
				return res, err
			}
			if ok {
				res.Opened++
			} else {
				res.Suppressed++
			}
		case m.held && isOpen:
			if err := keep(ctx, pool, alert, rule, m); err != nil {
				return res, err
			}
			res.Kept++
		case isOpen && m.recovered:
			if err := closeAlert(ctx, pool, alert.id, rule, m.subject); err != nil {
				return res, err
			}
			res.Closed++
		}
		delete(open, m.subject)
	}

	for subject, alert := range open {
		if err := closeAlert(ctx, pool, alert.id, rule, subject); err != nil {
			return res, err
		}
		res.Closed++
	}
	return res, nil
}

func measure(ctx context.Context, pool *pgxpool.Pool, hostID int, rule Rule) ([]measurement, error) {
	q, ok := queries[rule.Subject]
	if !ok {
		return nil, fmt.Errorf("nothing to measure %q with", rule.Subject)
	}

	hold := time.Duration(rule.ForSec) * time.Second
	window := max(hold, recoverWindow)
	if q.window > 0 {
		window = q.window
	}
	now := time.Now()

	var sql string
	switch {
	case q.sql != "":
		sql = q.sql
	case q.how == perWindow:
		sql = fmt.Sprintf(`
			SELECT %[1]s AS subject,
			       100.0 * count(*) FILTER (WHERE %[2]s AND ts >= $3) / nullif(count(*) FILTER (WHERE ts >= $3), 0),
			       100.0 * count(*) FILTER (WHERE %[2]s AND ts >= $4) / nullif(count(*) FILTER (WHERE ts >= $4), 0),
			       count(*) FILTER (WHERE ts >= $3), min(ts) FILTER (WHERE ts >= $3)
			FROM %[3]s
			WHERE host_id = $1 AND ts >= $2 AND %[1]s IS NOT NULL
			GROUP BY 1`, q.subject, q.running, q.table)
	default:
		sql = fmt.Sprintf(`
			WITH s AS (
				SELECT %[1]s AS subject, ts, (%[2]s)::float8 AS v
				FROM %[3]s WHERE host_id = $1 AND ts >= $2
			)
			SELECT subject,
			       min(v) FILTER (WHERE ts >= $3), max(v) FILTER (WHERE ts >= $3),
			       count(*) FILTER (WHERE ts >= $3), min(ts) FILTER (WHERE ts >= $3),
			       min(v) FILTER (WHERE ts >= $4), max(v) FILTER (WHERE ts >= $4),
			       (array_agg(v ORDER BY ts DESC))[1]
			FROM s GROUP BY subject`, q.subject, q.value, q.table)
	}

	var rows pgx.Rows
	var err error
	if q.sql != "" {
		rows, err = pool.Query(ctx, sql, now.Add(-window))
	} else {
		rows, err = pool.Query(ctx, sql, hostID, now.Add(-window), now.Add(-hold), now.Add(-recoverWindow))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	need := int(hold/sampleStep)/2 + 1
	begin := now.Add(-hold).Add(sampleStep + sampleStep/2)

	var out []measurement
	for rows.Next() {
		var m measurement
		if q.how == perWindow {
			var held, recov *float64
			var n int
			var first *time.Time
			if err := rows.Scan(&m.subject, &held, &recov, &n, &first); err != nil {
				return nil, err
			}
			switch {
			case held == nil || n < 1:
				m.blind = true
			case q.sql == "" && (n < need || first == nil || first.After(begin)):
				m.blind = true
			default:
				m.last = *held
				m.held = rule.Match(*held)
				m.recovered = recov == nil || !rule.Match(*recov)
			}
		} else {
			var lo, hi, rlo, rhi, last *float64
			var n int
			var first *time.Time
			if err := rows.Scan(&m.subject, &lo, &hi, &n, &first, &rlo, &rhi, &last); err != nil {
				return nil, err
			}
			if last != nil {
				m.last = *last
			}
			if lo == nil || hi == nil || n < need || first == nil || first.After(begin) {
				m.blind = true
			} else {
				m.held = rule.Match(*lo) && rule.Match(*hi)
				m.recovered = rlo == nil || rhi == nil || (!rule.Match(*rlo) && !rule.Match(*rhi))
			}
		}

		if rule.Target != nil && *rule.Target != "" && *rule.Target != m.subject {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type alertRow struct {
	id       int64
	severity Severity
}

func openAlerts(ctx context.Context, pool *pgxpool.Pool, ruleID int) (map[string]alertRow, error) {
	rows, err := pool.Query(ctx,
		"SELECT id, subject, severity FROM alerts WHERE rule_id = $1 AND closed_at IS NULL", ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]alertRow{}
	for rows.Next() {
		var id int64
		var subject string
		var sev Severity
		if err := rows.Scan(&id, &subject, &sev); err != nil {
			return nil, err
		}
		out[subject] = alertRow{id: id, severity: sev}
	}
	return out, rows.Err()
}

func (e *Engine) open(ctx context.Context, pool *pgxpool.Pool, rule Rule, m measurement) (bool, error) {
	var recent int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alerts
		WHERE rule_id = $1 AND subject = $2 AND opened_at > now() - $3::interval`,
		rule.ID, m.subject, stormWindow).Scan(&recent); err != nil {
		return false, err
	}
	if recent >= stormLimit {
		return false, nil
	}

	payload, err := json.Marshal(payloadFor(rule, m))
	if err != nil {
		return false, err
	}

	tag, err := pool.Exec(ctx, `
		INSERT INTO alerts (rule_id, subject, severity, value, worst, payload)
		VALUES ($1, $2, $3, $4, $4, $5)
		ON CONFLICT (rule_id, subject) WHERE closed_at IS NULL DO NOTHING`,
		rule.ID, m.subject, rule.Severity, m.last, payload)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	log.Printf("rules: alert \"%s\" on %s (%s, value %.1f)", rule.Name, m.subject, rule.Severity, m.last)
	return true, nil
}

func keep(ctx context.Context, pool *pgxpool.Pool, a alertRow, rule Rule, m measurement) error {
	_, err := pool.Exec(ctx, `
		UPDATE alerts SET last_seen = now(), severity = $2,
		       worst = CASE WHEN $3 > coalesce(worst, $3) THEN $3 ELSE worst END
		WHERE id = $1`, a.id, rule.Severity, m.last)
	return err
}

func closeAlert(ctx context.Context, pool *pgxpool.Pool, id int64, rule Rule, subject string) error {
	_, err := pool.Exec(ctx, "UPDATE alerts SET closed_at = now(), last_seen = now() WHERE id = $1", id)
	if err == nil {
		log.Printf("rules: closed alert \"%s\" on %s", rule.Name, subject)
	}
	return err
}

func payloadFor(rule Rule, m measurement) map[string]any {
	out := map[string]any{
		"rule":      rule.Name,
		"subject":   m.subject,
		"value":     math.Round(m.last*100) / 100,
		"threshold": rule.Threshold,
		"op":        string(rule.Op),
		"forSec":    rule.ForSec,
	}
	if src, ok := Sources[rule.Subject]; ok && src.Unit != "" {
		out["unit"] = src.Unit
	}
	if q, ok := queries[rule.Subject]; ok && q.suggest != "" {
		out["suggest"] = map[string]string{
			"kind":   string(q.suggest),
			"target": m.subject,
		}
	}
	return out
}
