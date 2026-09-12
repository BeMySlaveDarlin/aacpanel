package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	ResRaw = "raw"
	Res1m  = "1m"
	Res1h  = "1h"
)

const (
	maxPointsDefault = 500
	maxPointsLimit   = 2000
	binOrigin        = "2000-01-01 00:00:00+00"
	keepCacheFor     = 5 * time.Minute
)

// Series is a series for the chart.
type Series struct {
	Subject    string     `json:"subject"`
	Metric     string     `json:"metric"`
	Resolution string     `json:"resolution"`
	StepSec    int        `json:"stepSec"`
	From       int64      `json:"from"`
	To         int64      `json:"to"`
	T          []int64    `json:"t"`
	Avg        []*float64 `json:"avg"`
	Max        []*float64 `json:"max"`
}

// TopRow is a row of the "top consumers" list.
type TopRow struct {
	Container string   `json:"container"`
	Avg       float64  `json:"avg"`
	Max       float64  `json:"max"`
	Delta     *float64 `json:"delta,omitempty"`
	Samples   int      `json:"samples"`
	Coverage  float64  `json:"coverage"`
	LastState string   `json:"lastState,omitempty"`
	LastSeen  int64    `json:"lastSeen"`
	Alive     bool     `json:"alive"`
}

// Top is the top for a period.
type Top struct {
	Metric     string   `json:"metric"`
	Resolution string   `json:"resolution"`
	SortBy     string   `json:"sortBy"`
	From       int64    `json:"from"`
	To         int64    `json:"to"`
	Rows       []TopRow `json:"rows"`
}

type columns struct{ raw, avg, max string }

var containerMetrics = map[string]columns{
	"cpu": {raw: "cpu_pct", avg: "cpu_avg", max: "cpu_max"},
	"mem": {raw: "mem_bytes", avg: "mem_avg", max: "mem_max"},
}

var hostMetrics = map[string]columns{
	"cpu":       {raw: "cpu_pct", avg: "cpu_avg", max: "cpu_max"},
	"load":      {raw: "load1", avg: "load1_avg", max: "load1_max"},
	"mem":       {raw: "mem_used", avg: "mem_used_avg", max: "mem_used_max"},
	"swap":      {raw: "swap_used", avg: "swap_used_avg", max: "swap_used_max"},
	"cpu_temp":  {raw: "cpu_temp", avg: "cpu_temp_avg", max: "cpu_temp_max"},
	"mem_temp":  {raw: "mem_temp", avg: "mem_temp_avg", max: "mem_temp_max"},
	"disk_temp": {raw: "disk_temp", avg: "disk_temp_avg", max: "disk_temp_max"},
}

// ErrBadRequest is a metric or a resolution that does not exist.
type ErrBadRequest struct{ msg string }

func (e ErrBadRequest) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return ErrBadRequest{msg: fmt.Sprintf(format, args...)}
}

type keepCache struct {
	mu   sync.Mutex
	at   time.Time
	keep map[string]time.Duration
}

var keeps keepCache

func (s *Store) retention(ctx context.Context) map[string]time.Duration {
	keeps.mu.Lock()
	defer keeps.mu.Unlock()
	if time.Since(keeps.at) < keepCacheFor && keeps.keep != nil {
		return keeps.keep
	}

	out := map[string]time.Duration{}
	if pool, err := s.Pool(); err == nil {
		rows, err := pool.Query(ctx, "SELECT relname, EXTRACT(epoch FROM keep)::bigint FROM partition_config")
		if err == nil {
			for rows.Next() {
				var name string
				var sec int64
				if err := rows.Scan(&name, &sec); err == nil {
					out[name] = time.Duration(sec) * time.Second
				}
			}
			rows.Close()
		}
	}
	if len(out) == 0 {
		return keeps.keep
	}
	keeps.at, keeps.keep = time.Now(), out
	return out
}

func (s *Store) pickResolution(ctx context.Context, prefix string, from, to time.Time) string {
	period := to.Sub(from)
	keep := s.retention(ctx)
	age := time.Since(from)

	fits := func(table string, def time.Duration) bool {
		d, ok := keep[table]
		if !ok {
			d = def
		}
		return age < d-time.Hour
	}

	switch {
	case period <= 6*time.Hour && fits(prefix+"_raw", 48*time.Hour):
		return ResRaw
	case period <= 14*24*time.Hour && fits(prefix+"_1m", 14*24*time.Hour):
		return Res1m
	default:
		return Res1h
	}
}

// SeriesReq says what to show.
type SeriesReq struct {
	HostID     int
	Subject    string
	Metric     string
	From, To   time.Time
	MaxPoints  int
	Resolution string
}

// TopReq says for which period and by what to compute the top consumers.
type TopReq struct {
	HostID     int
	Metric     string
	From, To   time.Time
	Limit      int
	Resolution string
	SortBy     string
}

// SeriesFor returns the series for the chart.
func (s *Store) SeriesFor(ctx context.Context, req SeriesReq) (*Series, error) {
	subject, metric, from, to := req.Subject, req.Metric, req.From, req.To
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}

	var prefix, container string
	var metrics map[string]columns
	switch {
	case subject == "host":
		prefix, metrics = "metrics_host", hostMetrics
	case strings.HasPrefix(subject, "container:"):
		prefix, metrics = "metrics_container", containerMetrics
		container = strings.TrimPrefix(subject, "container:")
		if container == "" {
			return nil, badRequest("the container name is not set")
		}
	default:
		return nil, badRequest("unknown subject %q", subject)
	}

	cols, ok := metrics[metric]
	if !ok {
		return nil, badRequest("%q has no metric %q", subject, metric)
	}

	res := req.Resolution
	if res == "" {
		res = s.pickResolution(ctx, prefix, from, to)
	}
	table, tsCol, err := tableFor(prefix, res)
	if err != nil {
		return nil, err
	}

	step := stepFor(res, from, to, req.MaxPoints)
	out := &Series{
		Subject: subject, Metric: metric, Resolution: res,
		StepSec: int(step.Seconds()), From: from.Unix(), To: to.Unix(),
		T: []int64{}, Avg: []*float64{}, Max: []*float64{},
	}

	var value, peak string
	if res == ResRaw {
		value = fmt.Sprintf("avg(%s)::float8", cols.raw)
		peak = fmt.Sprintf("max(%s)::float8", cols.raw)
	} else {
		value = fmt.Sprintf("(sum(%s::float8 * samples) / nullif(sum(samples), 0))::float8", cols.avg)
		peak = fmt.Sprintf("max(%s)::float8", cols.max)
	}

	where := "host_id = $2 AND " + tsCol + " >= $3 AND " + tsCol + " < $4"
	args := []any{step, req.HostID, from, to}
	switch {
	case container != "":
		where += " AND container = $5"
		args = append(args, container)
	}

	q := fmt.Sprintf(`
		WITH d AS (
			SELECT date_bin($1, %s, timestamptz '%s') AS bucket, %s AS v, %s AS peak
			FROM %s WHERE %s GROUP BY 1
		), span AS (
			SELECT min(bucket) AS lo, max(bucket) AS hi FROM d
		)
		SELECT extract(epoch FROM g)::bigint, d.v, d.peak
		FROM span
		CROSS JOIN LATERAL generate_series(span.lo, span.hi, $1) AS g
		LEFT JOIN d ON d.bucket = g
		ORDER BY 1`,
		tsCol, binOrigin, value, peak, table, where)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, Unavailable(err)
	}
	defer rows.Close()

	for rows.Next() {
		var t int64
		var avg, max *float64
		if err := rows.Scan(&t, &avg, &max); err != nil {
			return nil, Unavailable(err)
		}
		out.T = append(out.T, t)
		out.Avg = append(out.Avg, avg)
		out.Max = append(out.Max, max)
	}
	return out, Unavailable(rows.Err())
}

// TopFor reports who ate the most over the period.
func (s *Store) TopFor(ctx context.Context, req TopReq) (*Top, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	metric, from, to := req.Metric, req.From, req.To
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	cols, ok := containerMetrics[metric]
	if !ok && metric != "disk" {
		return nil, badRequest("there is no metric %q for the top list", metric)
	}

	res := req.Resolution
	if res == "" {
		res = s.pickResolution(ctx, "metrics_container", from, to)
	}
	table, tsCol, err := tableFor("metrics_container", res)
	if err != nil {
		return nil, err
	}

	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = defaultSort(metric)
	}

	var value, peak, delta string
	switch {
	case metric == "disk" && res == ResRaw:
		value = "avg(disk_rw)::float8"
		peak = "max(disk_rw)::float8"
		delta = "(max(disk_rw)::float8 - min(disk_rw)::float8)"
	case metric == "disk":
		value = "NULL::float8"
		peak = "max(disk_rw_max)::float8"
		delta = "(max(disk_rw_max)::float8 - min(disk_rw_min)::float8)"
	case res == ResRaw:
		value = fmt.Sprintf("avg(%s)::float8", cols.raw)
		peak = fmt.Sprintf("max(%s)::float8", cols.raw)
		delta = "NULL::float8"
	default:
		value = fmt.Sprintf("(sum(%s::float8 * samples) / nullif(sum(samples), 0))::float8", cols.avg)
		peak = fmt.Sprintf("max(%s)::float8", cols.max)
		delta = "NULL::float8"
	}

	lastState, lastSeen := "''::text", "extract(epoch FROM max("+tsCol+"))::bigint"
	if res == ResRaw {
		lastState = "(array_agg(state ORDER BY ts DESC))[1]"
	}

	counted := value
	if metric == "disk" {
		counted = "disk_rw"
		if res != ResRaw {
			counted = "disk_rw_max"
		}
	} else if res != ResRaw {
		counted = cols.avg
	} else {
		counted = cols.raw
	}

	order := map[string]string{"avg": "v", "max": "peak", "delta": "delta"}[sortBy]
	if order == "" {
		return nil, badRequest("sorting by %q is not supported: avg, max or delta", sortBy)
	}

	q := fmt.Sprintf(`
		SELECT container, %s AS v, %s AS peak, %s AS delta,
		       count(%s) AS samples, coalesce(%s, '') AS last_state, %s AS last_seen
		FROM %s WHERE host_id = $1 AND %s >= $2 AND %s < $3
		GROUP BY container HAVING count(%s) > 0
		ORDER BY %s DESC NULLS LAST LIMIT %d`,
		value, peak, delta, counted, lastState, lastSeen,
		table, tsCol, tsCol, counted, order, limit)

	rows, err := pool.Query(ctx, q, req.HostID, from, to)
	if err != nil {
		return nil, Unavailable(err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TopRow, error) {
		var row TopRow
		var avg, peak *float64
		if err := r.Scan(&row.Container, &avg, &peak, &row.Delta,
			&row.Samples, &row.LastState, &row.LastSeen); err != nil {
			return row, err
		}
		if avg != nil {
			row.Avg = *avg
		}
		if peak != nil {
			row.Max = *peak
		}
		row.Alive = alive(row.LastState, row.LastSeen, res, to)
		row.Coverage = coverage(row.Samples, res, metric, from, to)
		return row, nil
	})
	if err != nil {
		return nil, Unavailable(err)
	}
	if list == nil {
		list = []TopRow{}
	}
	return &Top{
		Metric: metric, Resolution: res, SortBy: sortBy,
		From: from.Unix(), To: to.Unix(), Rows: list,
	}, nil
}

func defaultSort(metric string) string {
	switch metric {
	case "mem":
		return "max"
	case "disk":
		return "delta"
	default:
		return "avg"
	}
}

func alive(lastState string, lastSeen int64, res string, to time.Time) bool {
	if lastState != "running" {
		return false
	}
	step := 10 * time.Second
	switch res {
	case Res1m:
		step = time.Minute
	case Res1h:
		step = time.Hour
	}
	return to.Unix()-lastSeen <= int64((6 * step).Seconds())
}

func coverage(samples int, res, metric string, from, to time.Time) float64 {
	step := 10 * time.Second
	switch {
	case metric == "disk" && res == ResRaw:
		step = 5 * time.Minute
	case res == Res1m:
		step = time.Minute
	case res == Res1h:
		step = time.Hour
	}
	expected := to.Sub(from) / step
	if expected <= 0 {
		return 0
	}
	c := float64(samples) / float64(expected)
	if c > 1 {
		c = 1
	}
	return c
}

func tableFor(prefix, res string) (table, tsCol string, err error) {
	switch res {
	case ResRaw:
		return prefix + "_raw", "ts", nil
	case Res1m:
		return prefix + "_1m", "bucket", nil
	case Res1h:
		return prefix + "_1h", "bucket", nil
	default:
		return "", "", badRequest("unknown resolution %q", res)
	}
}

func stepFor(res string, from, to time.Time, maxPoints int) time.Duration {
	if maxPoints <= 0 {
		maxPoints = maxPointsDefault
	}
	if maxPoints > maxPointsLimit {
		maxPoints = maxPointsLimit
	}

	native := 10 * time.Second
	switch res {
	case Res1m:
		native = time.Minute
	case Res1h:
		native = time.Hour
	}

	period := to.Sub(from)
	if period <= 0 {
		return native
	}
	step := period / time.Duration(maxPoints)
	if step <= native {
		return native
	}
	return ((step + native - 1) / native) * native
}

func (s *Store) pickRollupRes(ctx context.Context, table string, def time.Duration, from, to time.Time) string {
	keep := s.retention(ctx)
	d, ok := keep[table]
	if !ok {
		d = def
	}
	if to.Sub(from) <= 14*24*time.Hour && time.Since(from) < d-time.Hour {
		return Res1m
	}
	return Res1h
}
