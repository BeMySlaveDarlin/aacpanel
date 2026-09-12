package watchcfg

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/store"
)

const (
	syncTimeout = 30 * time.Second
	retryEvery  = 15 * time.Second
)

// Report says what the synchronisation changed. Nothing changed on a run that
// follows an unchanged config, which is the normal case.
type Report struct {
	RulesAdded     int
	RulesUpdated   int
	RulesDisabled  int
	ProbesAdded    int
	ProbesUpdated  int
	ProbesDisabled int
}

func (r Report) quiet() bool {
	return r == Report{}
}

// Apply brings the database to the built-in config, waiting for the database to
// come up. It returns once the config is applied, or when the context ends.
//
// A failure here is not fatal: the panel keeps watching whatever the database
// already holds. A config that does not parse is caught by the tests long before
// this, since it is built into the binary.
func Apply(ctx context.Context, s *store.Store) {
	cfg, err := Load()
	if err != nil {
		log.Printf("watchcfg: %v", err)
		return
	}

	t := time.NewTicker(retryEvery)
	defer t.Stop()

	for {
		pool, err := s.Pool()
		if err == nil {
			var rep Report
			rep, err = Sync(ctx, pool, cfg)
			switch {
			case err == nil && rep.quiet():
				return
			case err == nil:
				log.Printf("watchcfg: rules +%d ~%d -%d, probes +%d ~%d -%d",
					rep.RulesAdded, rep.RulesUpdated, rep.RulesDisabled,
					rep.ProbesAdded, rep.ProbesUpdated, rep.ProbesDisabled)
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		if !unavailable(err) {
			log.Printf("watchcfg: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func unavailable(err error) bool {
	return errors.Is(err, store.ErrUnavailable) || errors.Is(err, store.ErrClosed)
}

// Sync writes the config into the database in one transaction.
func Sync(ctx context.Context, pool *pgxpool.Pool, cfg Config) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rep Report
	if err := syncRules(ctx, tx, cfg, &rep); err != nil {
		return Report{}, err
	}
	if err := syncProbes(ctx, tx, cfg, &rep); err != nil {
		return Report{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Report{}, err
	}
	return rep, nil
}

func syncRules(ctx context.Context, tx pgx.Tx, cfg Config, rep *Report) error {
	keys := make([]string, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		keys = append(keys, r.Key)

		var target *string
		if r.Target != "" {
			target = &r.Target
		}
		var written string
		err := tx.QueryRow(ctx, `
			INSERT INTO rules (key, name, subject, target, op, threshold, for_sec, severity, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (key) DO UPDATE SET
				name      = excluded.name,
				subject   = excluded.subject,
				target    = excluded.target,
				op        = excluded.op,
				threshold = excluded.threshold,
				for_sec   = excluded.for_sec,
				severity  = excluded.severity,
				enabled   = excluded.enabled
			WHERE (rules.name, rules.subject, rules.target, rules.op,
			       rules.threshold, rules.for_sec, rules.severity, rules.enabled)
			   IS DISTINCT FROM
			      (excluded.name, excluded.subject, excluded.target, excluded.op,
			       excluded.threshold, excluded.for_sec, excluded.severity, excluded.enabled)
			RETURNING CASE WHEN xmax = 0 THEN 'added' ELSE 'updated' END`,
			r.Key, r.Name, r.Subject, target, r.Op, r.Threshold,
			int(r.For.std().Seconds()), r.Severity, on(r.Enabled)).Scan(&written)
		switch {
		case errors.Is(err, pgx.ErrNoRows): // already as the config says
		case err != nil:
			return fmt.Errorf("rule %q: %w", r.Key, err)
		case written == "added":
			rep.RulesAdded++
		default:
			rep.RulesUpdated++
		}
	}

	// A rule that lost its entry in the config stops firing but keeps its alerts:
	// alerts reference it with ON DELETE CASCADE, and deleting it would take the
	// history along.
	tag, err := tx.Exec(ctx, `
		UPDATE rules SET enabled = false
		 WHERE enabled AND NOT (key = ANY($1))`, keys)
	if err != nil {
		return err
	}
	rep.RulesDisabled = int(tag.RowsAffected())
	return nil
}

func syncProbes(ctx context.Context, tx pgx.Tx, cfg Config, rep *Report) error {
	keys := make([]string, 0, len(cfg.Probes))
	for _, p := range cfg.Probes {
		keys = append(keys, p.Key)

		var expect *int
		if p.ExpectStatus != 0 {
			expect = &p.ExpectStatus
		}
		var components *string
		if p.Components != "" {
			components = &p.Components
		}
		var written string
		err := tx.QueryRow(ctx, `
			INSERT INTO probes (key, name, kind, target, interval_sec, timeout_sec,
			                    expect_status, components, runner, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'service', $9)
			ON CONFLICT (key) DO UPDATE SET
				name          = excluded.name,
				kind          = excluded.kind,
				target        = excluded.target,
				interval_sec  = excluded.interval_sec,
				timeout_sec   = excluded.timeout_sec,
				expect_status = excluded.expect_status,
				components    = excluded.components,
				enabled       = excluded.enabled
			WHERE (probes.name, probes.kind, probes.target, probes.interval_sec,
			       probes.timeout_sec, probes.expect_status, probes.components, probes.enabled)
			   IS DISTINCT FROM
			      (excluded.name, excluded.kind, excluded.target, excluded.interval_sec,
			       excluded.timeout_sec, excluded.expect_status, excluded.components, excluded.enabled)
			RETURNING CASE WHEN xmax = 0 THEN 'added' ELSE 'updated' END`,
			p.Key, p.Name, p.Kind, p.Target,
			int(p.Interval.std().Seconds()), int(p.Timeout.std().Seconds()),
			expect, components, on(p.Enabled)).Scan(&written)
		switch {
		case errors.Is(err, pgx.ErrNoRows): // already as the config says
		case err != nil:
			return fmt.Errorf("probe %q: %w", p.Key, err)
		case written == "added":
			rep.ProbesAdded++
		default:
			rep.ProbesUpdated++
		}
	}

	// Only the probes this config owns are touched. The ones the collector on the
	// host sends carry no key and are none of its business.
	tag, err := tx.Exec(ctx, `
		UPDATE probes SET enabled = false
		 WHERE enabled AND key IS NOT NULL AND NOT (key = ANY($1))`, keys)
	if err != nil {
		return err
	}
	rep.ProbesDisabled = int(tag.RowsAffected())
	return nil
}
