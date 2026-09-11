package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

func TestAgentProbesPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() { pool.Exec(ctx, "DELETE FROM probes WHERE name LIKE 'test-agent%'") }
	cleanup()
	defer cleanup()

	w := NewWriter(s, "AGENT-PROBE-TEST")
	at := time.Now().Unix()
	snapshot := func(at int64, ok bool) []byte {
		return []byte(fmt.Sprintf(`{
			"at": %d,
			"host": {"cpuPct": 1, "load": [0.1], "mem": {"total": 2, "used": 1}},
			"probes": {"at": %d, "items": [
				{"name": "test-agent tunnel", "kind": "tcp", "target": "10.0.0.1:51820", "ok": %t,
				 "outcome": "%s", "latencyMs": null, "error": %s},
				{"name": "test-agent port", "kind": "tcp", "target": "127.0.0.1:3493", "ok": true,
				 "outcome": "ok", "latencyMs": 3, "error": null}
			]}
		}`, at, at, ok, map[bool]string{true: "ok", false: "network"}[ok],
			map[bool]string{true: "null", false: `"the port does not answer"`}[ok]))
	}

	w.AgentSnapshot(snapshot(at, false))
	if !w.flushOne(ctx) {
		t.Fatal("the batch with the snapshot was not written")
	}

	t.Run("the probe created itself", func(t *testing.T) {
		var kind, target, runner string
		if err := pool.QueryRow(ctx,
			"SELECT kind, target, runner FROM probes WHERE name = 'test-agent tunnel'").Scan(&kind, &target, &runner); err != nil {
			t.Fatal(err)
		}
		if kind != "tcp" || target != "10.0.0.1:51820" || runner != "agent" {
			t.Errorf("the probe was created as %s/%s/%s, tcp/10.0.0.1:51820/agent was expected", kind, target, runner)
		}
	})

	t.Run("the result is written with its reason", func(t *testing.T) {
		var ok bool
		var outcome string
		var errText *string
		if err := pool.QueryRow(ctx, `SELECT r.ok, r.outcome, r.error FROM probe_results r
			JOIN probes p ON p.id = r.probe_id WHERE p.name = 'test-agent tunnel'`).Scan(&ok, &outcome, &errText); err != nil {
			t.Fatal(err)
		}
		if ok || outcome != "network" {
			t.Errorf("ok=%v outcome=%q was written, false/network was expected", ok, outcome)
		}
		if errText == nil {
			t.Error("the error's text was lost on the way")
		}
	})

	t.Run("the same snapshot does not double the history", func(t *testing.T) {
		w.lastAgentAt = 0
		w.AgentSnapshot(snapshot(at, false))
		w.flushOne(ctx)

		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_results r
			JOIN probes p ON p.id = r.probe_id WHERE p.name = 'test-agent tunnel'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%d results, one was expected: a repeat of the snapshot doubled the history", n)
		}
	})

	t.Run("a disabled probe does not accumulate history", func(t *testing.T) {
		if _, err := pool.Exec(ctx, "UPDATE probes SET enabled = false WHERE name = 'test-agent port'"); err != nil {
			t.Fatal(err)
		}
		before := countResults(ctx, t, pool, "test-agent port")

		w.lastAgentAt = 0
		w.AgentSnapshot(snapshot(at+60, true))
		w.flushOne(ctx)

		if after := countResults(ctx, t, pool, "test-agent port"); after != before {
			t.Errorf("the disabled probe gained results: there were %d, now there are %d", before, after)
		}
		if n := countResults(ctx, t, pool, "test-agent tunnel"); n != 2 {
			t.Errorf("the enabled probe has %d results, 2 were expected", n)
		}
	})
}

func countResults(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM probe_results r
		JOIN probes p ON p.id = r.probe_id WHERE p.name = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAgentSnapshotBrokenBlockKeepsRestPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}

	const hostName = "AGENT-BROKEN-BLOCK-TEST"
	hostID, err := s.HostID(ctx, hostName)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		for _, tbl := range []string{"metrics_host_raw", "metrics_disk_raw", "metrics_net_raw", "sessions_raw"} {
			pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE host_id = $1", hostID)
		}
	}
	cleanup()
	defer cleanup()

	at := time.Now().Truncate(time.Second)
	raw := fmt.Appendf(nil, `{
		"at": %d,
		"host": {
			"cpuPct": 12.5, "load": [1.5, 1.2, 1.0],
			"mem": {"total": 1000, "used": 400, "swapUsed": 10},
			"disks": [{"mount": "/", "total": 100, "used": 30.5}],
			"net": [{"name": "eth0", "rxRate": 111, "txRate": 222}]
		},
		"sessions": [{"session": "test-session", "pct": 10, "tokens": 5, "messages": 2, "model": "opus",
		              "sessionId": "cccccccc-0000-0000-0000-000000000003", "cwd": "/srv/proj/test"}]
	}`, at.Unix())

	w := NewWriter(s, hostName)
	w.AgentSnapshot(raw)
	if !w.flushOne(ctx) {
		t.Fatal("the batch with the snapshot was not written")
	}

	count := func(t *testing.T, table string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM "+table+" WHERE host_id = $1 AND ts = $2", hostID, at).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("everything else arrived", func(t *testing.T) {
		for _, tbl := range []string{"metrics_host_raw", "metrics_net_raw", "sessions_raw"} {
			if n := count(t, tbl); n != 1 {
				t.Errorf("%s: %d rows, wanted 1 — the block left along with the disks", tbl, n)
			}
		}
		var cpu float64
		if err := pool.QueryRow(ctx,
			"SELECT cpu_pct FROM metrics_host_raw WHERE host_id = $1 AND ts = $2", hostID, at).Scan(&cpu); err != nil {
			t.Fatal(err)
		}
		if cpu < 12 || cpu > 13 {
			t.Errorf("cpu_pct = %v, wanted 12.5", cpu)
		}

		var sessionID, cwd *string
		if err := pool.QueryRow(ctx,
			"SELECT session_id, cwd FROM sessions_raw WHERE host_id = $1 AND ts = $2",
			hostID, at).Scan(&sessionID, &cwd); err != nil {
			t.Fatal(err)
		}
		if sessionID == nil || *sessionID != "cccccccc-0000-0000-0000-000000000003" {
			t.Errorf("the conversation's uuid is %v, wanted cccccccc-…-000000000003", sessionID)
		}
		if cwd == nil || *cwd != "/srv/proj/test" {
			t.Errorf("directory %v, wanted /srv/proj/test — there is nowhere to launch the resume", cwd)
		}
	})

	t.Run("the disks are not written", func(t *testing.T) {
		if n := count(t, "metrics_disk_raw"); n != 0 {
			t.Errorf("%d rows for the disks, yet the block did not parse", n)
		}
	})

	t.Run("the failure is reported outwards", func(t *testing.T) {
		faults := w.SnapshotFaults()
		if len(faults) != 1 || faults[0].Block != blockDisks {
			t.Fatalf("wanted one failure on the disks, got %+v", faults)
		}
		if !strings.Contains(faults[0].Error, "used") {
			t.Errorf("the field is not visible in the failure's text: %q", faults[0].Error)
		}
	})
}

func TestAgentProbeRowFollowsTheAgentPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { pool.Exec(ctx, "DELETE FROM probes WHERE name LIKE 'test-catchup%'") }
	cleanup()
	t.Cleanup(cleanup)

	w := NewWriter(s, "AGENT-PROBE-FOLLOW")
	snapshot := func(at int64, target string, interval, timeout int) []byte {
		return []byte(fmt.Sprintf(`{
			"at": %d,
			"host": {"cpuPct": 1, "load": [0.1], "mem": {"total": 2, "used": 1}},
			"probes": {"at": %d, "items": [
				{"name": "test-catchup port", "kind": "tcp", "target": %q,
				 "intervalSec": %d, "timeoutSec": %d,
				 "ok": true, "outcome": "ok", "latencyMs": 3, "error": null}
			]}
		}`, at, at, target, interval, timeout))
	}

	at := time.Now().Unix()
	w.AgentSnapshot(snapshot(at, "127.0.0.1:3493", 60, 3))
	if !w.flushOne(ctx) {
		t.Fatal("the batch with the snapshot was not written")
	}

	read := func(t *testing.T, name string) (target string, interval, timeout int, enabled bool) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			"SELECT target, interval_sec, timeout_sec, enabled FROM probes WHERE name = $1",
			name).Scan(&target, &interval, &timeout, &enabled); err != nil {
			t.Fatal(err)
		}
		return
	}

	t.Run("it was created with what the agent sent", func(t *testing.T) {
		target, interval, timeout, _ := read(t, "test-catchup port")
		if target != "127.0.0.1:3493" || interval != 60 || timeout != 3 {
			t.Errorf("created as %s/%ds/%ds, 127.0.0.1:3493/60s/3s was expected", target, interval, timeout)
		}
	})

	t.Run("the address and the pace catch up with the machine's settings", func(t *testing.T) {
		w.lastAgentAt = 0
		w.AgentSnapshot(snapshot(at+60, "127.0.0.1:9999", 30, 7))
		if !w.flushOne(ctx) {
			t.Fatal("the batch with the snapshot was not written: the subtest would be checking an untouched row and would always pass")
		}

		target, interval, timeout, _ := read(t, "test-catchup port")
		if target != "127.0.0.1:9999" {
			t.Errorf("the address stayed %q: the panel shows a probe that does not exist", target)
		}
		if interval != 30 || timeout != 7 {
			t.Errorf("the pace stayed %ds/%ds, the agent probes at 30s/7s", interval, timeout)
		}
	})

	t.Run("a disabled one stays disabled", func(t *testing.T) {
		if _, err := pool.Exec(ctx,
			"UPDATE probes SET enabled = false WHERE name = 'test-catchup port'"); err != nil {
			t.Fatal(err)
		}
		w.lastAgentAt = 0
		w.AgentSnapshot(snapshot(at+120, "127.0.0.1:1234", 15, 2))
		if !w.flushOne(ctx) {
			t.Fatal("the batch with the snapshot was not written: the subtest would be checking an untouched row and would always pass")
		}

		target, _, _, enabled := read(t, "test-catchup port")
		if enabled {
			t.Error("updating the description enabled a disabled probe: that is a person's manual setting")
		}
		if target != "127.0.0.1:1234" {
			t.Errorf("the disabled probe's address did not catch up with the agent: %q", target)
		}
	})

	t.Run("a pace outside the schema's bounds does not bring the batch down", func(t *testing.T) {
		w.lastAgentAt = 0
		w.AgentSnapshot(snapshot(at+150, "127.0.0.1:5555", 5, 900))
		if !w.flushOne(ctx) {
			t.Fatal("the batch was not written: the agent's out-of-bounds value brought the machine's metrics down too")
		}

		_, interval, timeout, _ := read(t, "test-catchup port")
		if interval < 10 || timeout > 60 {
			t.Errorf("%ds/%ds in the database — the schema does not accept such values", interval, timeout)
		}
	})

	t.Run("the agent does not overwrite somebody else's probe", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `INSERT INTO probes (name, kind, target, interval_sec, timeout_sec, runner)
			VALUES ('test-catchup foreign', 'http', 'https://example.invalid', 120, 9, 'service')`); err != nil {
			t.Fatal(err)
		}
		w.lastAgentAt = 0
		w.AgentSnapshot([]byte(fmt.Sprintf(`{
			"at": %d,
			"host": {"cpuPct": 1, "load": [0.1], "mem": {"total": 2, "used": 1}},
			"probes": {"at": %d, "items": [
				{"name": "test-catchup foreign", "kind": "tcp", "target": "127.0.0.1:1",
				 "intervalSec": 15, "timeoutSec": 2,
				 "ok": true, "outcome": "ok", "latencyMs": 1, "error": null}
			]}
		}`, at+180, at+180)))
		if !w.flushOne(ctx) {
			t.Fatal("the batch with the snapshot was not written: the subtest would be checking an untouched row and would always pass")
		}

		target, interval, timeout, _ := read(t, "test-catchup foreign")
		if target != "https://example.invalid" || interval != 120 || timeout != 9 {
			t.Errorf("a service probe was overwritten by the agent: %s/%ds/%ds", target, interval, timeout)
		}
	})
}
