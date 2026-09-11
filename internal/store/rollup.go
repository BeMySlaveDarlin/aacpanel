package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	rollupLockKey int64 = 0x726f6c6c
	rollupEvery         = time.Minute
	batchPause          = 200 * time.Millisecond
	maxBatches          = 48
	batchTimeout        = 30 * time.Second
)

type job struct {
	name    string
	sql     string
	bucket  time.Duration
	window  time.Duration
	overlap time.Duration
	lag     time.Duration
	every   time.Duration
	src     string
	srcCol  string
}

var jobs = []job{
	{
		name: "container_1m", bucket: time.Minute, window: 30 * time.Minute,
		overlap: 5 * time.Minute, lag: 2 * time.Minute,
		src: "metrics_container_raw", srcCol: "ts",
		sql: `
			INSERT INTO metrics_container_1m
				(bucket, host_id, container, cpu_avg, cpu_max, mem_avg, mem_max, samples,
				 disk_rw_min, disk_rw_max)
			SELECT date_trunc('minute', ts), host_id, container,
			       avg(cpu_pct)::real, max(cpu_pct),
			       avg(mem_bytes)::bigint, max(mem_bytes),
			       count(cpu_pct),
			       min(disk_rw), max(disk_rw)
			FROM metrics_container_raw
			WHERE ts >= $1 AND ts < $2
			GROUP BY 1, 2, 3
			HAVING count(cpu_pct) > 0
			ON CONFLICT (host_id, container, bucket) DO UPDATE SET
				cpu_avg = excluded.cpu_avg, cpu_max = excluded.cpu_max,
				mem_avg = excluded.mem_avg, mem_max = excluded.mem_max,
				samples = excluded.samples,
				disk_rw_min = excluded.disk_rw_min, disk_rw_max = excluded.disk_rw_max`,
	},
	{
		name: "host_1m", bucket: time.Minute, window: 30 * time.Minute,
		overlap: 5 * time.Minute, lag: 2 * time.Minute,
		src: "metrics_host_raw", srcCol: "ts",
		sql: `
			INSERT INTO metrics_host_1m
				(bucket, host_id, cpu_avg, cpu_max, load1_avg, load1_max,
				 mem_used_avg, mem_used_max, mem_total, swap_used_avg, swap_used_max, samples)
			SELECT date_trunc('minute', ts), host_id,
			       avg(cpu_pct)::real, max(cpu_pct),
			       avg(load1)::real, max(load1),
			       avg(mem_used)::bigint, max(mem_used), max(mem_total),
			       avg(swap_used)::bigint, max(swap_used),
			       count(cpu_pct)
			FROM metrics_host_raw
			WHERE ts >= $1 AND ts < $2
			GROUP BY 1, 2
			HAVING count(cpu_pct) > 0
			ON CONFLICT (host_id, bucket) DO UPDATE SET
				cpu_avg = excluded.cpu_avg, cpu_max = excluded.cpu_max,
				load1_avg = excluded.load1_avg, load1_max = excluded.load1_max,
				mem_used_avg = excluded.mem_used_avg, mem_used_max = excluded.mem_used_max,
				mem_total = excluded.mem_total,
				swap_used_avg = excluded.swap_used_avg, swap_used_max = excluded.swap_used_max,
				samples = excluded.samples`,
	},
	{
		name: "container_1h", bucket: time.Hour, window: 12 * time.Hour,
		overlap: time.Hour, lag: 5 * time.Minute, every: 10 * time.Minute,
		src: "metrics_container_1m", srcCol: "bucket",
		sql: `
			INSERT INTO metrics_container_1h
				(bucket, host_id, container, cpu_avg, cpu_max, mem_avg, mem_max, samples,
				 disk_rw_min, disk_rw_max)
			SELECT date_trunc('hour', bucket), host_id, container,
			       (sum(cpu_avg::double precision * samples) / nullif(sum(samples), 0))::real,
			       max(cpu_max),
			       (sum(mem_avg::double precision * samples) / nullif(sum(samples), 0))::bigint,
			       max(mem_max),
			       sum(samples),
			       min(disk_rw_min), max(disk_rw_max)
			FROM metrics_container_1m
			WHERE bucket >= $1 AND bucket < $2
			GROUP BY 1, 2, 3
			HAVING sum(samples) > 0
			ON CONFLICT (host_id, container, bucket) DO UPDATE SET
				cpu_avg = excluded.cpu_avg, cpu_max = excluded.cpu_max,
				mem_avg = excluded.mem_avg, mem_max = excluded.mem_max,
				samples = excluded.samples,
				disk_rw_min = excluded.disk_rw_min, disk_rw_max = excluded.disk_rw_max`,
	},
	{
		name: "host_1h", bucket: time.Hour, window: 12 * time.Hour,
		overlap: time.Hour, lag: 5 * time.Minute, every: 10 * time.Minute,
		src: "metrics_host_1m", srcCol: "bucket",
		sql: `
			INSERT INTO metrics_host_1h
				(bucket, host_id, cpu_avg, cpu_max, load1_avg, load1_max,
				 mem_used_avg, mem_used_max, mem_total, swap_used_avg, swap_used_max, samples)
			SELECT date_trunc('hour', bucket), host_id,
			       (sum(cpu_avg::double precision * samples) / nullif(sum(samples), 0))::real,
			       max(cpu_max),
			       (sum(load1_avg::double precision * samples) / nullif(sum(samples), 0))::real,
			       max(load1_max),
			       (sum(mem_used_avg::double precision * samples) / nullif(sum(samples), 0))::bigint,
			       max(mem_used_max), max(mem_total),
			       (sum(swap_used_avg::double precision * samples) / nullif(sum(samples), 0))::bigint,
			       max(swap_used_max),
			       sum(samples)
			FROM metrics_host_1m
			WHERE bucket >= $1 AND bucket < $2
			GROUP BY 1, 2
			HAVING sum(samples) > 0
			ON CONFLICT (host_id, bucket) DO UPDATE SET
				cpu_avg = excluded.cpu_avg, cpu_max = excluded.cpu_max,
				load1_avg = excluded.load1_avg, load1_max = excluded.load1_max,
				mem_used_avg = excluded.mem_used_avg, mem_used_max = excluded.mem_used_max,
				mem_total = excluded.mem_total,
				swap_used_avg = excluded.swap_used_avg, swap_used_max = excluded.swap_used_max,
				samples = excluded.samples`,
	},
	{
		name: "disk_1m", bucket: time.Minute, window: 30 * time.Minute,
		overlap: 5 * time.Minute, lag: 2 * time.Minute,
		src: "metrics_disk_raw", srcCol: "ts",
		sql: `
			INSERT INTO metrics_disk_1m
				(bucket, host_id, mount, used_last, used_max, used_avg, total, samples)
			SELECT date_trunc('minute', ts), host_id, mount,
			       (array_agg(used ORDER BY ts DESC))[1],
			       max(used), avg(used)::bigint,
			       (array_agg(total ORDER BY ts DESC))[1],
			       count(used)
			FROM metrics_disk_raw
			WHERE ts >= $1 AND ts < $2
			GROUP BY 1, 2, 3
			HAVING count(used) > 0
			ON CONFLICT (host_id, mount, bucket) DO UPDATE SET
				used_last = excluded.used_last, used_max = excluded.used_max,
				used_avg = excluded.used_avg, total = excluded.total,
				samples = excluded.samples`,
	},
	{
		name: "net_1m", bucket: time.Minute, window: 30 * time.Minute,
		overlap: 5 * time.Minute, lag: 2 * time.Minute,
		src: "metrics_net_raw", srcCol: "ts",
		sql: `
			WITH d AS (
				SELECT ts, host_id, iface, rx_rate, tx_rate,
				       rx_total - lag(rx_total) OVER w AS rx_delta,
				       tx_total - lag(tx_total) OVER w AS tx_delta
				FROM metrics_net_raw
				WHERE ts >= $1::timestamptz - interval '1 minute' AND ts < $2
				WINDOW w AS (PARTITION BY host_id, iface ORDER BY ts)
			)
			INSERT INTO metrics_net_1m
				(bucket, host_id, iface, rx_avg, rx_max, tx_avg, tx_max, rx_bytes, tx_bytes, samples)
			SELECT date_trunc('minute', ts), host_id, iface,
			       avg(rx_rate)::bigint, max(rx_rate),
			       avg(tx_rate)::bigint, max(tx_rate),
			       sum(greatest(rx_delta, 0)), sum(greatest(tx_delta, 0)),
			       count(*)
			FROM d
			WHERE ts >= $1 AND ts < $2
			GROUP BY 1, 2, 3
			HAVING count(*) > 0
			ON CONFLICT (host_id, iface, bucket) DO UPDATE SET
				rx_avg = excluded.rx_avg, rx_max = excluded.rx_max,
				tx_avg = excluded.tx_avg, tx_max = excluded.tx_max,
				rx_bytes = excluded.rx_bytes, tx_bytes = excluded.tx_bytes,
				samples = excluded.samples`,
	},
	{
		name: "net_1h", bucket: time.Hour, window: 12 * time.Hour,
		overlap: time.Hour, lag: 5 * time.Minute, every: 10 * time.Minute,
		src: "metrics_net_1m", srcCol: "bucket",
		sql: `
			INSERT INTO metrics_net_1h
				(bucket, host_id, iface, rx_avg, rx_max, tx_avg, tx_max, rx_bytes, tx_bytes, samples)
			SELECT date_trunc('hour', bucket), host_id, iface,
			       (sum(rx_avg::double precision * samples) / nullif(sum(samples), 0))::bigint,
			       max(rx_max),
			       (sum(tx_avg::double precision * samples) / nullif(sum(samples), 0))::bigint,
			       max(tx_max),
			       sum(rx_bytes), sum(tx_bytes),
			       sum(samples)
			FROM metrics_net_1m
			WHERE bucket >= $1 AND bucket < $2
			GROUP BY 1, 2, 3
			HAVING sum(samples) > 0
			ON CONFLICT (host_id, iface, bucket) DO UPDATE SET
				rx_avg = excluded.rx_avg, rx_max = excluded.rx_max,
				tx_avg = excluded.tx_avg, tx_max = excluded.tx_max,
				rx_bytes = excluded.rx_bytes, tx_bytes = excluded.tx_bytes,
				samples = excluded.samples`,
	},
	{
		name: "sessions_1m", bucket: time.Minute, window: 30 * time.Minute,
		overlap: 5 * time.Minute, lag: 2 * time.Minute,
		src: "sessions_raw", srcCol: "ts",
		sql: `
			INSERT INTO sessions_1m
				(bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples,
				 session_id, cwd)
			SELECT date_trunc('minute', ts), host_id, name,
			       max(tokens), avg(pct)::real, max(pct), max(messages),
			       count(pct),
			       (array_agg(session_id ORDER BY ts DESC) FILTER (WHERE session_id IS NOT NULL))[1],
			       (array_agg(cwd ORDER BY ts DESC) FILTER (WHERE cwd IS NOT NULL))[1]
			FROM sessions_raw
			WHERE ts >= $1 AND ts < $2
			GROUP BY 1, 2, 3
			HAVING count(pct) > 0
			ON CONFLICT (host_id, name, bucket) DO UPDATE SET
				tokens_max = excluded.tokens_max, pct_avg = excluded.pct_avg,
				pct_max = excluded.pct_max, messages_max = excluded.messages_max,
				samples = excluded.samples,
				session_id = excluded.session_id, cwd = excluded.cwd`,
	},
	{
		name: "disk_1h", bucket: time.Hour, window: 12 * time.Hour,
		overlap: time.Hour, lag: 5 * time.Minute, every: 10 * time.Minute,
		src: "metrics_disk_1m", srcCol: "bucket",
		sql: `
			INSERT INTO metrics_disk_1h
				(bucket, host_id, mount, used_last, used_max, used_avg, total, samples)
			SELECT date_trunc('hour', bucket), host_id, mount,
			       (array_agg(used_last ORDER BY bucket DESC))[1],
			       max(used_max),
			       (sum(used_avg::double precision * samples) / nullif(sum(samples), 0))::bigint,
			       (array_agg(total ORDER BY bucket DESC))[1],
			       sum(samples)
			FROM metrics_disk_1m
			WHERE bucket >= $1 AND bucket < $2
			GROUP BY 1, 2, 3
			HAVING sum(samples) > 0
			ON CONFLICT (host_id, mount, bucket) DO UPDATE SET
				used_last = excluded.used_last, used_max = excluded.used_max,
				used_avg = excluded.used_avg, total = excluded.total,
				samples = excluded.samples`,
	},
	{
		name: "sessions_1h", bucket: time.Hour, window: 12 * time.Hour,
		overlap: time.Hour, lag: 5 * time.Minute, every: 10 * time.Minute,
		src: "sessions_1m", srcCol: "bucket",
		sql: `
			INSERT INTO sessions_1h
				(bucket, host_id, name, tokens_max, pct_avg, pct_max, messages_max, samples,
				 session_id, cwd)
			SELECT date_trunc('hour', bucket), host_id, name,
			       max(tokens_max),
			       (sum(pct_avg::double precision * samples) / nullif(sum(samples), 0))::real,
			       max(pct_max), max(messages_max),
			       sum(samples),
			       (array_agg(session_id ORDER BY bucket DESC) FILTER (WHERE session_id IS NOT NULL))[1],
			       (array_agg(cwd ORDER BY bucket DESC) FILTER (WHERE cwd IS NOT NULL))[1]
			FROM sessions_1m
			WHERE bucket >= $1 AND bucket < $2
			GROUP BY 1, 2, 3
			HAVING sum(samples) > 0
			ON CONFLICT (host_id, name, bucket) DO UPDATE SET
				tokens_max = excluded.tokens_max, pct_avg = excluded.pct_avg,
				pct_max = excluded.pct_max, messages_max = excluded.messages_max,
				session_id = excluded.session_id, cwd = excluded.cwd,
				samples = excluded.samples`,
	},
}

func (s *Store) keepRollups(ctx context.Context) {
	t := time.NewTicker(rollupEvery)
	defer t.Stop()

	for {
		err := s.rollupOnce(ctx)
		switch {
		case err == nil, ctx.Err() != nil:
		case errors.Is(err, ErrClosed):
			return
		default:
			log.Printf("store: rollup: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Store) rollupOnce(ctx context.Context) error {
	pool, err := s.Pool()
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", rollupLockKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", rollupLockKey); err != nil {
			log.Printf("store: the rollup lock was not released: %v", err)
		}
	}()

	for _, j := range jobs {
		if err := s.runJob(ctx, conn.Conn(), j); err != nil {
			return fmt.Errorf("%s: %w", j.name, err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	return nil
}

func (s *Store) runJob(ctx context.Context, conn *pgx.Conn, j job) error {
	to := time.Now().Add(-j.lag).Truncate(j.bucket)

	mark, updated, err := j.state(ctx, conn)
	if err != nil {
		return err
	}
	if j.every > 0 && updated != nil && time.Since(*updated) < j.every {
		return nil
	}

	from, ok, err := j.from(ctx, conn, mark)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	started := time.Now()
	var total int64
	var batches int

	for from.Before(to) && batches < maxBatches {
		end := from.Add(j.window)
		if end.After(to) {
			end = to
		}

		n, err := j.batch(ctx, conn, from, end)
		if err != nil {
			return err
		}
		total += n
		batches++
		from = end

		if from.Before(to) {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(batchPause):
			}
		}
	}

	if total > 0 && batches > 1 {
		log.Printf("store: rollup %s: %d rows in %s, windows %d",
			j.name, total, time.Since(started).Round(time.Millisecond), batches)
	}
	return nil
}

func (j job) state(ctx context.Context, conn *pgx.Conn) (mark, updated *time.Time, err error) {
	err = conn.QueryRow(ctx,
		"SELECT last_bucket, updated_at FROM rollup_state WHERE name = $1", j.name).Scan(&mark, &updated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	return mark, updated, nil
}

func (j job) from(ctx context.Context, conn *pgx.Conn, mark *time.Time) (time.Time, bool, error) {
	if mark != nil {
		return mark.Add(-j.overlap).Truncate(j.bucket), true, nil
	}

	var first *time.Time
	q := fmt.Sprintf("SELECT min(%s) FROM %s", j.srcCol, j.src)
	if err := conn.QueryRow(ctx, q).Scan(&first); err != nil {
		return time.Time{}, false, err
	}
	if first == nil {
		return time.Time{}, false, nil
	}
	return first.Truncate(j.bucket), true, nil
}

func (j job) batch(ctx context.Context, conn *pgx.Conn, from, to time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())

	started := time.Now()
	tag, err := tx.Exec(ctx, j.sql, from, to)
	if err != nil {
		return 0, err
	}
	took := time.Since(started).Milliseconds()

	if _, err := tx.Exec(ctx, `
		INSERT INTO rollup_state (name, last_bucket, rows_last, took_ms)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE SET
			last_bucket = excluded.last_bucket,
			updated_at = now(),
			rows_last = CASE WHEN excluded.rows_last > 0 THEN excluded.rows_last ELSE rollup_state.rows_last END,
			took_ms   = CASE WHEN excluded.rows_last > 0 THEN excluded.took_ms  ELSE rollup_state.took_ms  END`,
		j.name, to, tag.RowsAffected(), took); err != nil {
		return 0, err
	}

	return tag.RowsAffected(), tx.Commit(ctx)
}
