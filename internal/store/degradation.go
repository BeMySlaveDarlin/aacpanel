package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	degRatio       = 1.5
	degWindow      = 30 * time.Minute
	degMinFraction = 2
	degMinSamples  = 3
	degLookback    = 7 * 24 * time.Hour
	degNormSamples = 3
	degSearch      = 6 * time.Hour

	degFloorCPU = 2.0
	degFloorMem = 64 * 1024 * 1024
)

// Degradation is one degradation that was found.
type Degradation struct {
	Subject string  `json:"subject"`
	Metric  string  `json:"metric"`
	Current float64 `json:"current"`
	Norm    float64 `json:"norm"`
	Ratio   float64 `json:"ratio"`
	Since   int64   `json:"since"`
	Samples int     `json:"samples"`
	Basis   string  `json:"basis"`
}

// DegradationReq holds the search's parameters.
type DegradationReq struct {
	HostID   int
	Ratio    float64
	Window   time.Duration
	Lookback time.Duration
	Now      time.Time
}

func (r DegradationReq) minSamples() int {
	return max(degMinSamples, int(r.Window/time.Minute)/degMinFraction)
}

func (r *DegradationReq) fill() {
	if r.Ratio <= 1 {
		r.Ratio = degRatio
	}
	if r.Window <= 0 {
		r.Window = degWindow
	}
	if r.Lookback <= 0 {
		r.Lookback = degLookback
	}
	if r.Now.IsZero() {
		r.Now = time.Now()
	}
}

// Degradations returns the list of what is currently worse than its own norm.
func (s *Store) Degradations(ctx context.Context, req DegradationReq) (list []Degradation, err error) {
	defer func() { err = Unavailable(err) }()

	req.fill()
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}

	out := make([]Degradation, 0, 8)
	containers, err := s.containerDegradations(ctx, req)
	if err != nil {
		return nil, err
	}
	out = append(out, containers...)

	hosts, err := s.hostDegradations(ctx, req)
	if err != nil {
		return nil, err
	}
	out = append(out, hosts...)

	for i := range out {
		since, err := s.degradationStart(ctx, pool, req, out[i])
		if err != nil {
			return nil, err
		}
		out[i].Since = since
	}
	return out, nil
}

const containerDegradationsSQL = `
WITH cur AS (
    SELECT container,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY cpu_avg)::float8 AS cpu,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY mem_avg)::float8 AS mem,
           count(*)::int AS samples
      FROM metrics_container_1m
     WHERE host_id = $1 AND bucket >= $2 AND bucket <= $3
     GROUP BY container
), past AS (
    SELECT container, cpu_avg, mem_avg,
           LEAST((extract(hour FROM bucket AT TIME ZONE 'UTC')::int
                  - extract(hour FROM $3::timestamptz AT TIME ZONE 'UTC')::int + 24) % 24,
                 (extract(hour FROM $3::timestamptz AT TIME ZONE 'UTC')::int
                  - extract(hour FROM bucket AT TIME ZONE 'UTC')::int + 24) % 24) <= 1 AS same_hour
      FROM metrics_container_1h
     WHERE host_id = $1 AND bucket >= $4 AND bucket < date_trunc('hour', $3::timestamptz)
), agg AS (
    SELECT container,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY cpu_avg) FILTER (WHERE same_hour)::float8 AS cpu_hour,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY mem_avg) FILTER (WHERE same_hour)::float8 AS mem_hour,
           count(*) FILTER (WHERE same_hour)::int AS samples_hour,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY cpu_avg)::float8 AS cpu_day,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY mem_avg)::float8 AS mem_day,
           count(*)::int AS samples_day
      FROM past
     GROUP BY container
), norm AS (
    SELECT container,
           CASE WHEN samples_hour >= $9 THEN cpu_hour ELSE cpu_day END AS cpu,
           CASE WHEN samples_hour >= $9 THEN mem_hour ELSE mem_day END AS mem,
           CASE WHEN samples_hour >= $9 THEN 'hour' ELSE 'day' END AS basis
      FROM agg
     WHERE samples_hour >= $9 OR samples_day >= $9
)
SELECT cur.container, m.metric, m.current, m.norm, m.current / m.norm AS ratio, cur.samples, norm.basis
  FROM cur
  JOIN norm USING (container)
 CROSS JOIN LATERAL (VALUES
        ('cpu', cur.cpu, norm.cpu, $6::float8),
        ('mem', cur.mem, norm.mem, $7::float8)
    ) AS m(metric, current, norm, floor)
 WHERE cur.samples >= $8
   AND m.norm >= m.floor AND m.current >= m.norm * $5
 ORDER BY ratio DESC`

func (s *Store) containerDegradations(ctx context.Context, req DegradationReq) ([]Degradation, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, containerDegradationsSQL,
		req.HostID, req.Now.Add(-req.Window), req.Now, req.Now.Add(-req.Lookback),
		req.Ratio, degFloorCPU, float64(degFloorMem), req.minSamples(), degNormSamples)
	if err != nil {
		return nil, fmt.Errorf("container degradations: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Degradation, error) {
		var (
			d         Degradation
			container string
		)
		err := row.Scan(&container, &d.Metric, &d.Current, &d.Norm, &d.Ratio, &d.Samples, &d.Basis)
		d.Subject = "container:" + container
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("container degradations: %w", err)
	}
	return list, nil
}

const hostDegradationsSQL = `
WITH cur AS (
    SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY cpu_avg)::float8 AS cpu,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY mem_used_avg)::float8 AS mem,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY load1_avg)::float8 AS load,
           count(*)::int AS samples
      FROM metrics_host_1m
     WHERE host_id = $1 AND bucket >= $2 AND bucket <= $3
), past AS (
    SELECT cpu_avg, mem_used_avg, load1_avg,
           LEAST((extract(hour FROM bucket AT TIME ZONE 'UTC')::int
                  - extract(hour FROM $3::timestamptz AT TIME ZONE 'UTC')::int + 24) % 24,
                 (extract(hour FROM $3::timestamptz AT TIME ZONE 'UTC')::int
                  - extract(hour FROM bucket AT TIME ZONE 'UTC')::int + 24) % 24) <= 1 AS same_hour
      FROM metrics_host_1h
     WHERE host_id = $1 AND bucket >= $4 AND bucket < date_trunc('hour', $3::timestamptz)
), agg AS (
    SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY cpu_avg) FILTER (WHERE same_hour)::float8 AS cpu_hour,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY mem_used_avg) FILTER (WHERE same_hour)::float8 AS mem_hour,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY load1_avg) FILTER (WHERE same_hour)::float8 AS load_hour,
           count(*) FILTER (WHERE same_hour)::int AS samples_hour,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY cpu_avg)::float8 AS cpu_day,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY mem_used_avg)::float8 AS mem_day,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY load1_avg)::float8 AS load_day,
           count(*)::int AS samples_day
      FROM past
), norm AS (
    SELECT CASE WHEN samples_hour >= $9 THEN cpu_hour  ELSE cpu_day  END AS cpu,
           CASE WHEN samples_hour >= $9 THEN mem_hour  ELSE mem_day  END AS mem,
           CASE WHEN samples_hour >= $9 THEN load_hour ELSE load_day END AS load,
           CASE WHEN samples_hour >= $9 THEN 'hour' ELSE 'day' END AS basis
      FROM agg
     WHERE samples_hour >= $9 OR samples_day >= $9
)
SELECT m.metric, m.current, m.norm, m.current / m.norm AS ratio, cur.samples, norm.basis
  FROM cur, norm
 CROSS JOIN LATERAL (VALUES
        ('cpu',  cur.cpu,  norm.cpu,  $6::float8),
        ('mem',  cur.mem,  norm.mem,  $7::float8),
        ('load', cur.load, norm.load, $10::float8)
    ) AS m(metric, current, norm, floor)
 WHERE cur.samples >= $8
   AND m.norm >= m.floor AND m.current >= m.norm * $5
 ORDER BY ratio DESC`

const degFloorLoad = 0.5

func (s *Store) hostDegradations(ctx context.Context, req DegradationReq) ([]Degradation, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, hostDegradationsSQL,
		req.HostID, req.Now.Add(-req.Window), req.Now, req.Now.Add(-req.Lookback),
		req.Ratio, degFloorCPU, float64(degFloorMem), req.minSamples(), degNormSamples, degFloorLoad)
	if err != nil {
		return nil, fmt.Errorf("host degradations: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Degradation, error) {
		var d Degradation
		d.Subject = "host"
		return d, row.Scan(&d.Metric, &d.Current, &d.Norm, &d.Ratio, &d.Samples, &d.Basis)
	})
	if err != nil {
		return nil, fmt.Errorf("host degradations: %w", err)
	}
	return list, nil
}

func (s *Store) degradationStart(ctx context.Context, pool *pgxpool.Pool, req DegradationReq, d Degradation) (int64, error) {
	table, column, container := "metrics_host_1m", "", ""
	switch d.Metric {
	case "cpu":
		column = "cpu_avg"
	case "mem":
		column = "mem_used_avg"
	case "load":
		column = "load1_avg"
	}
	if after, ok := strings.CutPrefix(d.Subject, "container:"); ok {
		table, container = "metrics_container_1m", after
		if d.Metric == "mem" {
			column = "mem_avg"
		}
	}

	threshold := d.Norm * req.Ratio
	from := req.Now.Add(-degSearch)

	query := fmt.Sprintf(`SELECT max(bucket) FROM %s
		 WHERE host_id = $1 AND bucket >= $2 AND bucket <= $3 AND %s < $4`, table, column)
	args := []any{req.HostID, from, req.Now, threshold}
	if container != "" {
		query = fmt.Sprintf(`SELECT max(bucket) FROM %s
			 WHERE host_id = $1 AND bucket >= $2 AND bucket <= $3 AND %s < $4 AND container = $5`, table, column)
		args = append(args, container)
	}

	var last *time.Time
	if err := pool.QueryRow(ctx, query, args...).Scan(&last); err != nil {
		return 0, fmt.Errorf("the start of the degradation: %w", err)
	}
	if last == nil {
		return 0, nil
	}
	return last.Add(time.Minute).Unix(), nil
}
