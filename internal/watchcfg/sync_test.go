package watchcfg

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func open(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	s, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	return ctx, pool
}

// built reads the config the binary carries, so the tests below check the thing
// that ships rather than a fixture that resembles it.
func built(t *testing.T) Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSyncBringsAFreshDatabaseToTheConfig(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	if _, err := Sync(ctx, pool, cfg); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `
		SELECT key, name, subject, coalesce(target, ''), op, threshold, for_sec, severity, enabled
		  FROM rules ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	want := map[string]Rule{}
	for _, r := range cfg.Rules {
		want[r.Key] = r
	}
	seen := 0
	for rows.Next() {
		var got Rule
		var forSec int
		var enabled bool
		if err := rows.Scan(&got.Key, &got.Name, &got.Subject, &got.Target,
			&got.Op, &got.Threshold, &forSec, &got.Severity, &enabled); err != nil {
			t.Fatal(err)
		}
		seen++
		r, ok := want[got.Key]
		if !ok {
			t.Fatalf("the database holds the rule %q, the config does not", got.Key)
		}
		if !enabled {
			t.Fatalf("rule %q came out disabled", got.Key)
		}
		if got.Name != r.Name || got.Subject != r.Subject || got.Target != r.Target ||
			got.Op != r.Op || got.Threshold != r.Threshold || got.Severity != r.Severity ||
			forSec != int(r.For.std().Seconds()) {
			t.Fatalf("rule %q reads %+v, the config says %+v", got.Key, got, r)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(cfg.Rules) {
		t.Fatalf("the database holds %d rules, the config describes %d", seen, len(cfg.Rules))
	}

	probeRows, err := pool.Query(ctx, `
		SELECT key, name, kind, target, interval_sec, timeout_sec,
		       coalesce(expect_status, 0), coalesce(components, ''), enabled
		  FROM probes WHERE key IS NOT NULL ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer probeRows.Close()

	wantProbe := map[string]Probe{}
	for _, p := range cfg.Probes {
		wantProbe[p.Key] = p
	}
	seen = 0
	for probeRows.Next() {
		var got Probe
		var interval, timeout int
		var enabled bool
		if err := probeRows.Scan(&got.Key, &got.Name, &got.Kind, &got.Target,
			&interval, &timeout, &got.ExpectStatus, &got.Components, &enabled); err != nil {
			t.Fatal(err)
		}
		seen++
		p, ok := wantProbe[got.Key]
		if !ok {
			t.Fatalf("the database holds the probe %q, the config does not", got.Key)
		}
		if !enabled {
			t.Fatalf("probe %q came out disabled", got.Key)
		}
		if got.Name != p.Name || got.Kind != p.Kind || got.Target != p.Target ||
			got.ExpectStatus != p.ExpectStatus || got.Components != p.Components ||
			interval != int(p.Interval.std().Seconds()) || timeout != int(p.Timeout.std().Seconds()) {
			t.Fatalf("probe %q reads %+v, the config says %+v", got.Key, got, p)
		}
	}
	if err := probeRows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(cfg.Probes) {
		t.Fatalf("the database holds %d keyed probes, the config describes %d", seen, len(cfg.Probes))
	}
}

func TestSyncIsQuietOnASecondRun(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	if _, err := Sync(ctx, pool, cfg); err != nil {
		t.Fatal(err)
	}
	rep, err := Sync(ctx, pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.quiet() {
		t.Fatalf("the second run changed something: %+v", rep)
	}
}

func TestSyncWritesWhatTheConfigSays(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	if _, err := pool.Exec(ctx, `UPDATE rules SET threshold = 1, name = 'stale' WHERE key = 'disk.full'`); err != nil {
		t.Fatal(err)
	}
	rep, err := Sync(ctx, pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RulesUpdated != 1 {
		t.Fatalf("a changed rule was updated %d times", rep.RulesUpdated)
	}

	var name string
	var threshold float64
	if err := pool.QueryRow(ctx,
		`SELECT name, threshold FROM rules WHERE key = 'disk.full'`).Scan(&name, &threshold); err != nil {
		t.Fatal(err)
	}
	if name != "Disk is almost full" || threshold != 95 {
		t.Fatalf("the rule reads %q at %v", name, threshold)
	}
}

func TestARenamedRuleKeepsItsAlerts(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	var ruleID int
	if err := pool.QueryRow(ctx, `SELECT id FROM rules WHERE key = 'host.cpu'`).Scan(&ruleID); err != nil {
		t.Fatal(err)
	}
	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (rule_id, subject, severity, opened_at, last_seen, value, worst)
		VALUES ($1, 'host', 'warning', now(), now(), 99, 99) RETURNING id`, ruleID).Scan(&alertID); err != nil {
		t.Fatal(err)
	}

	// The config is the one that renames; the key stays put.
	for i := range cfg.Rules {
		if cfg.Rules[i].Key == "host.cpu" {
			cfg.Rules[i].Name = "The host CPU is pinned"
		}
	}
	if _, err := Sync(ctx, pool, cfg); err != nil {
		t.Fatal(err)
	}

	var name string
	var sameRule int
	if err := pool.QueryRow(ctx, `
		SELECT r.name, r.id FROM alerts a JOIN rules r ON r.id = a.rule_id
		 WHERE a.id = $1`, alertID).Scan(&name, &sameRule); err != nil {
		t.Fatalf("the alert lost its rule: %v", err)
	}
	if sameRule != ruleID {
		t.Fatalf("the alert moved from rule %d to %d", ruleID, sameRule)
	}
	if name != "The host CPU is pinned" {
		t.Fatalf("the alert shows the rule as %q", name)
	}
}

func TestARuleThatLeftTheConfigIsDisabledNotDeleted(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	var ruleID int
	if err := pool.QueryRow(ctx, `SELECT id FROM rules WHERE key = 'host.load'`).Scan(&ruleID); err != nil {
		t.Fatal(err)
	}
	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (rule_id, subject, severity, opened_at, last_seen, value, worst)
		VALUES ($1, 'host', 'info', now(), now(), 40, 40) RETURNING id`, ruleID).Scan(&alertID); err != nil {
		t.Fatal(err)
	}

	short := Config{Probes: cfg.Probes}
	for _, r := range cfg.Rules {
		if r.Key != "host.load" {
			short.Rules = append(short.Rules, r)
		}
	}
	rep, err := Sync(ctx, pool, short)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RulesDisabled != 1 {
		t.Fatalf("%d rules were disabled, expected the one that left", rep.RulesDisabled)
	}

	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM rules WHERE id = $1`, ruleID).Scan(&enabled); err != nil {
		t.Fatalf("the rule was deleted, not disabled: %v", err)
	}
	if enabled {
		t.Fatal("the rule that left the config is still watched")
	}
	var alerts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE id = $1`, alertID).Scan(&alerts); err != nil {
		t.Fatal(err)
	}
	if alerts != 1 {
		t.Fatal("the alert history went with the rule")
	}
}

func TestARuleThatCameBackIsWatchedAgain(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	short := Config{Probes: cfg.Probes}
	for _, r := range cfg.Rules {
		if r.Key != "host.load" {
			short.Rules = append(short.Rules, r)
		}
	}
	if _, err := Sync(ctx, pool, short); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(ctx, pool, cfg); err != nil {
		t.Fatal(err)
	}

	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM rules WHERE key = 'host.load'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("the rule returned to the config and stayed dark")
	}
}

func TestSyncLeavesTheProbesOfTheCollectorAlone(t *testing.T) {
	ctx, pool := open(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO probes (name, kind, target, interval_sec, timeout_sec, runner)
		VALUES ('collector: disk smart', 'http', 'http://127.0.0.1/smart', 60, 5, 'agent')`); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(ctx, pool, built(t)); err != nil {
		t.Fatal(err)
	}

	var enabled bool
	var key *string
	if err := pool.QueryRow(ctx,
		`SELECT enabled, key FROM probes WHERE name = 'collector: disk smart'`).Scan(&enabled, &key); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("the synchronisation disabled a probe the collector owns")
	}
	if key != nil {
		t.Fatalf("a probe of the collector was given the key %q", *key)
	}
}

func TestSyncSeedsAnEmptyTable(t *testing.T) {
	ctx, pool := open(t)
	cfg := built(t)

	if _, err := pool.Exec(ctx, `DELETE FROM alerts`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM rules`); err != nil {
		t.Fatal(err)
	}
	rep, err := Sync(ctx, pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RulesAdded != len(cfg.Rules) {
		t.Fatalf("%d rules of %d were written", rep.RulesAdded, len(cfg.Rules))
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM rules`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(cfg.Rules) {
		t.Fatalf("the table holds %d rules, the config describes %d", count, len(cfg.Rules))
	}
}
