package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	retentionLockKey int64 = 0x64726f70
	retentionEvery         = time.Hour
	retentionDelay         = 5 * time.Minute
	detachTimeout          = 2 * time.Minute
	partBoundUpper         = `TO \('([^']+)'\)`
)

type dropped struct {
	table string
	part  string
	upper time.Time
	rows  int64
}

func (s *Store) keepRetention(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(retentionDelay):
	}

	t := time.NewTicker(retentionEvery)
	defer t.Stop()

	for {
		_, err := s.retainOnce(ctx)
		switch {
		case err == nil, ctx.Err() != nil:
		case errors.Is(err, ErrClosed):
			return
		default:
			log.Printf("store: retention: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Store) retainOnce(ctx context.Context) ([]dropped, error) {
	pool, err := s.ownerPool()
	if err != nil {
		return nil, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", retentionLockKey).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", retentionLockKey); err != nil {
			log.Printf("store: the retention lock was not released: %v", err)
		}
	}()

	marks, err := rollupMarks(ctx, conn.Conn())
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx,
		"SELECT relname, EXTRACT(epoch FROM keep)::bigint, rollup_dep FROM partition_config ORDER BY relname")
	if err != nil {
		return nil, err
	}
	type policy struct {
		table string
		keep  time.Duration
		dep   *string
	}
	policies, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (policy, error) {
		var p policy
		var sec int64
		err := r.Scan(&p.table, &sec, &p.dep)
		p.keep = time.Duration(sec) * time.Second
		return p, err
	})
	if err != nil {
		return nil, err
	}

	var out []dropped
	var errs []error
	for _, p := range policies {
		older := time.Now().Add(-p.keep)

		limit := older
		if p.dep != nil {
			if !declaredRollup(*p.dep) {
				errs = append(errs, fmt.Errorf(
					"%s: rollup %q is not among the jobs, partitions are never dropped", p.table, *p.dep))
				continue
			}
			mark, ok := marks[*p.dep]
			if !ok {
				continue
			}
			if mark.Before(limit) {
				limit = mark
			}
		}

		parts, err := partitions(ctx, conn.Conn(), p.table)
		if err != nil {
			return out, errors.Join(append(errs, fmt.Errorf("%s: %w", p.table, err))...)
		}
		for _, part := range parts {
			if !part.upper.After(limit) {
				d, err := s.dropPartition(ctx, conn.Conn(), p.table, part)
				if err != nil {
					return out, errors.Join(append(errs, fmt.Errorf("%s: %w", part.name, err))...)
				}
				out = append(out, d)
			}
			if ctx.Err() != nil {
				return out, errors.Join(errs...)
			}
		}
	}
	return out, errors.Join(errs...)
}

func declaredRollup(name string) bool {
	return slices.ContainsFunc(jobs, func(j job) bool { return j.name == name })
}

type partition struct {
	name    string
	upper   time.Time
	pending bool
}

func partitions(ctx context.Context, conn *pgx.Conn, table string) ([]partition, error) {
	rows, err := conn.Query(ctx, `
		SELECT child.relname,
		       i.inhdetachpending,
		       (regexp_match(pg_get_expr(child.relpartbound, child.oid), $2))[1]::timestamptz
		FROM pg_inherits i
		JOIN pg_class parent ON parent.oid = i.inhparent
		JOIN pg_class child  ON child.oid  = i.inhrelid
		WHERE parent.relname = $1
		ORDER BY 3`, table, partBoundUpper)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (partition, error) {
		var p partition
		var upper *time.Time
		err := r.Scan(&p.name, &p.pending, &upper)
		if upper != nil {
			p.upper = *upper
		}
		return p, err
	})
}

func (s *Store) dropPartition(ctx context.Context, conn *pgx.Conn, table string, p partition) (dropped, error) {
	d := dropped{table: table, part: p.name, upper: p.upper}

	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+quoteIdent(p.name)).Scan(&d.rows); err != nil {
		return d, err
	}
	log.Printf("store: retention %s: dropping partition %s, %d rows, everything older than %s",
		table, p.name, d.rows, p.upper.UTC().Format("2006-01-02 15:04"))

	ctx, cancel := context.WithTimeout(ctx, detachTimeout)
	defer cancel()

	stmt := "ALTER TABLE " + quoteIdent(table) + " DETACH PARTITION " + quoteIdent(p.name)
	if p.pending {
		if _, err := conn.Exec(ctx, stmt+" FINALIZE"); err != nil {
			return d, err
		}
	} else if _, err := conn.Exec(ctx, stmt+" CONCURRENTLY"); err != nil {
		return d, err
	}

	if _, err := conn.Exec(ctx, "DROP TABLE IF EXISTS "+quoteIdent(p.name)); err != nil {
		return d, err
	}
	return d, nil
}

func quoteIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

func rollupMarks(ctx context.Context, conn *pgx.Conn) (map[string]time.Time, error) {
	rows, err := conn.Query(ctx, "SELECT name, last_bucket FROM rollup_state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]time.Time{}
	for rows.Next() {
		var name string
		var mark time.Time
		if err := rows.Scan(&name, &mark); err != nil {
			return nil, err
		}
		out[name] = mark
	}
	return out, rows.Err()
}
