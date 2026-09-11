package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

const (
	usageParent  = "6a0f1e2c-0000-4000-8000-000000000001"
	usageSecond  = "6a0f1e2c-0000-4000-8000-000000000002"
	usageThird   = "6a0f1e2c-0000-4000-8000-000000000003"
	usageFourth  = "6a0f1e2c-0000-4000-8000-000000000004"
	usageFifth   = "6a0f1e2c-0000-4000-8000-000000000005"
	usageSixth   = "6a0f1e2c-0000-4000-8000-000000000006"
	usageSeven   = "6a0f1e2c-0000-4000-8000-000000000007"
	usageEighth  = "6a0f1e2c-0000-4000-8000-000000000008"
	usageNinth   = "6a0f1e2c-0000-4000-8000-000000000009"
	usageTenth   = "6a0f1e2c-0000-4000-8000-00000000000a"
	usageEleven  = "6a0f1e2c-0000-4000-8000-00000000000b"
	usageTwelfth = "6a0f1e2c-0000-4000-8000-00000000000c"
)

func TestUsageToolsKeepAgentApartPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC)
	err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageParent, Contour: "home", CWD: "/srv/proj"},
		Tools: []UsageToolRow{
			{Bucket: hour, Agent: "", Tool: "Read", Calls: 3},
			{Bucket: hour, Agent: "agent-01J8QK2M", Tool: "Read", Calls: 5, Errors: 1},
		},
		Scan: UsageScanPoint{Path: "/t/parent.jsonl", Contour: "home", Inode: 11, Size: 900, Offset: 900},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	if got := toolCalls(t, ctx, pool, usageParent, hour, "", "Read"); got != 3 {
		t.Errorf("the main session's calls: %d, wanted 3", got)
	}
	if got := toolCalls(t, ctx, pool, usageParent, hour, "agent-01J8QK2M", "Read"); got != 5 {
		t.Errorf("the subagent's calls: %d, wanted 5", got)
	}
	if n := rowCount(t, ctx, pool, "usage_tools_1h", usageParent); n != 2 {
		t.Fatalf("tool rows %d, wanted two: a parent and a subagent within one hour are different rows", n)
	}

	err = s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageParent, Contour: "home"},
		Rows: []UsageRow{
			{Bucket: hour, Model: "claude-opus-5", Agent: "", Answers: 4, OutputTokens: 400},
			{Bucket: hour, Model: "claude-opus-5", Agent: "agent-01J8QK2M", Answers: 9, OutputTokens: 900},
		},
		Scan: UsageScanPoint{Path: "/t/parent.jsonl", Contour: "home", Inode: 11, Size: 1800, Offset: 1800},
	})
	if err != nil {
		t.Fatalf("writing the aggregate: %v", err)
	}
	if got := answersOf(t, ctx, pool, usageParent, hour, "claude-opus-5", ""); got != 4 {
		t.Errorf("the main session's answers: %d, wanted 4", got)
	}
	if got := answersOf(t, ctx, pool, usageParent, hour, "claude-opus-5", "agent-01J8QK2M"); got != 9 {
		t.Errorf("the subagent's answers: %d, wanted 9", got)
	}
}

func TestUsageWriteRepeatsWithoutAddingPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	file := UsageFile{
		Session: UsageSession{SessionID: usageSecond, Contour: "home", CWD: "/srv/proj/pets"},
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 7, InputTokens: 700, OutputTokens: 70}},
		Events:  []UsageEventRow{{Bucket: hour, Messages: 40, Compacts: 1}},
		Tools:   []UsageToolRow{{Bucket: hour, Tool: "Bash", Calls: 12, Errors: 2}},
		Scan:    UsageScanPoint{Path: "/t/repeat.jsonl", Contour: "home", Inode: 21, Size: 500, Offset: 500},
	}
	if err := s.WriteUsageFile(ctx, file); err != nil {
		t.Fatalf("the first write: %v", err)
	}
	if err := s.WriteUsageFile(ctx, file); err != nil {
		t.Fatalf("the repeat: %v", err)
	}

	if got := answersOf(t, ctx, pool, usageSecond, hour, "claude-opus-5", ""); got != 7 {
		t.Errorf("after the repeat there are %d answers, wanted 7 — the values must be replaced, not added up", got)
	}
	if got := toolCalls(t, ctx, pool, usageSecond, hour, "", "Bash"); got != 12 {
		t.Errorf("after the repeat there are %d calls, wanted 12", got)
	}
	if got := eventSum(t, ctx, pool, usageSecond, "messages"); got != 40 {
		t.Errorf("after the repeat there are %d messages, wanted 40", got)
	}

	next := hour.Add(time.Hour)
	err := s.WriteUsageFile(ctx, UsageFile{
		Session: file.Session,
		Rows:    []UsageRow{{Bucket: next, Model: "claude-opus-5", Answers: 2, OutputTokens: 20}},
		Events:  []UsageEventRow{{Bucket: next, Messages: 6}},
		Since:   next,
		Scan:    UsageScanPoint{Path: "/t/repeat.jsonl", Contour: "home", Inode: 21, Size: 800, Offset: 800},
	})
	if err != nil {
		t.Fatalf("the top-up: %v", err)
	}
	if got := answersOf(t, ctx, pool, usageSecond, hour, "claude-opus-5", ""); got != 7 {
		t.Errorf("topping up the tail touched the previous hour: %d answers, wanted 7", got)
	}
	if got := answersOf(t, ctx, pool, usageSecond, next, "claude-opus-5", ""); got != 2 {
		t.Errorf("the hour from the tail was not written: %d answers, wanted 2", got)
	}
	if got := eventSum(t, ctx, pool, usageSecond, "messages"); got != 46 {
		t.Errorf("%d messages for the session, wanted 46 — the hours are added up by the query", got)
	}

	points, err := s.UsageScanPoints(ctx)
	if err != nil {
		t.Fatalf("the scan points: %v", err)
	}
	p, ok := points["/t/repeat.jsonl"]
	if !ok {
		t.Fatal("there is no scan point for the file")
	}
	if p.Offset != 800 || p.Size != 800 {
		t.Errorf("the scan point: offset %d, size %d; wanted 800/800", p.Offset, p.Size)
	}
	if p.SessionID != usageSecond {
		t.Errorf("the scan point did not name the session: %q", p.SessionID)
	}
}

func TestUsageRewriteClearsOnlyItsAgentPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	session := UsageSession{SessionID: usageThird, Contour: "home"}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 5}},
		Events:  []UsageEventRow{{Bucket: hour, Messages: 30, Compacts: 1}},
		Tools:   []UsageToolRow{{Bucket: hour, Tool: "Read", Calls: 4}},
		Scan:    UsageScanPoint{Path: "/t/root.jsonl", Contour: "home", Inode: 31, Size: 100, Offset: 100},
	}); err != nil {
		t.Fatalf("the parent: %v", err)
	}
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows: []UsageRow{
			{Bucket: hour, Model: "claude-opus-5", Agent: "agent-A", Answers: 8},
			{Bucket: hour.Add(-time.Hour), Model: "claude-opus-5", Agent: "agent-A", Answers: 3},
		},
		Events: []UsageEventRow{{Bucket: hour, Agent: "agent-A", Messages: 12, Compacts: 2}},
		Tools:  []UsageToolRow{{Bucket: hour, Agent: "agent-A", Tool: "Read", Calls: 6}},
		Scan:   UsageScanPoint{Path: "/t/sub-a.jsonl", Contour: "home", Inode: 32, Size: 200, Offset: 200},
	}); err != nil {
		t.Fatalf("the subagent: %v", err)
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Agent: "agent-A", Answers: 1}},
		Scan:    UsageScanPoint{Path: "/t/sub-a.jsonl", Contour: "home", Inode: 32, Size: 50, Offset: 50},
		Rewrite: true,
		Agents:  []string{"agent-A"},
	}); err != nil {
		t.Fatalf("recounting the subagent: %v", err)
	}

	if got := answersOf(t, ctx, pool, usageThird, hour, "claude-opus-5", ""); got != 5 {
		t.Errorf("recounting the subagent took the parent's rows: %d answers, wanted 5", got)
	}
	if got := toolCalls(t, ctx, pool, usageThird, hour, "", "Read"); got != 4 {
		t.Errorf("recounting the subagent took the parent's tools: %d calls, wanted 4", got)
	}
	if got := eventSum(t, ctx, pool, usageThird, "compacts"); got != 1 {
		t.Errorf("%d compactions for the session, wanted one: the parent kept its own, the other one was wiped", got)
	}
	if got := answersOf(t, ctx, pool, usageThird, hour, "claude-opus-5", "agent-A"); got != 1 {
		t.Errorf("after the recount the subagent has %d answers, wanted 1", got)
	}
	var stale int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM usage_1h
		 WHERE session_id = $1 AND agent = 'agent-A' AND bucket = $2`,
		usageThird, hour.Add(-time.Hour)).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("after the recount an hour that is no longer in the file remains: %d rows", stale)
	}
	if got := toolCalls(t, ctx, pool, usageThird, hour, "agent-A", "Read"); got != 0 {
		t.Errorf("the subagent's tools survived the recount: %d calls, wanted 0", got)
	}
}

func TestUsageEventsSumAcrossAgentsPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	session := UsageSession{SessionID: usageTenth, Contour: "home"}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Events: []UsageEventRow{{
			Bucket: hour, Messages: 120, Compacts: 1,
			Interrupts: 3, InterruptsTool: 1, APIErrors: 2,
		}},
		Scan: UsageScanPoint{Path: "/t/ev-root.jsonl", Contour: "home", Inode: 101, Size: 10, Offset: 10},
	}); err != nil {
		t.Fatalf("the parent: %v", err)
	}
	for i, agent := range []string{"agent-D", "agent-E"} {
		if err := s.WriteUsageFile(ctx, UsageFile{
			Session: session,
			Events: []UsageEventRow{{
				Bucket: hour, Agent: agent, Messages: 20, Compacts: 8, APIErrors: 1,
			}},
			Scan: UsageScanPoint{
				Path:    "/t/ev-sub-" + agent + ".jsonl",
				Contour: "home", Inode: int64(102 + i), Size: 10, Offset: 10,
			},
		}); err != nil {
			t.Fatalf("subagent %s: %v", agent, err)
		}
	}

	if got := eventSum(t, ctx, pool, usageTenth, "compacts"); got != 17 {
		t.Errorf("%d compactions for the session, wanted 17", got)
	}
	if got := eventSum(t, ctx, pool, usageTenth, "messages"); got != 160 {
		t.Errorf("%d messages for the session, wanted 160", got)
	}
	if got := eventSum(t, ctx, pool, usageTenth, "api_errors"); got != 4 {
		t.Errorf("%d API errors for the session, wanted 4", got)
	}
	if got := eventSum(t, ctx, pool, usageTenth, "interrupts"); got != 3 {
		t.Errorf("%d interrupts, wanted 3", got)
	}
	if got := eventSum(t, ctx, pool, usageTenth, "interrupts_tool"); got != 1 {
		t.Errorf("%d refusals of a call, wanted 1", got)
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Events:  []UsageEventRow{{Bucket: hour, Agent: "agent-D", Messages: 25, Compacts: 9}},
		Since:   hour,
		Agents:  []string{"agent-D"},
		Scan: UsageScanPoint{
			Path: "/t/ev-sub-agent-D.jsonl", Contour: "home", Inode: 102, Size: 20, Offset: 20,
		},
	}); err != nil {
		t.Fatalf("the subagent repeat: %v", err)
	}
	if got := eventSum(t, ctx, pool, usageTenth, "compacts"); got != 18 {
		t.Errorf("after recounting one subagent there are %d compactions, wanted 18", got)
	}
}

func TestUsageSinceClearsPhantomRowPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 21, 0, 0, 0, time.UTC)
	session := UsageSession{SessionID: usageEleven, Contour: "home"}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 1, OutputTokens: 5}},
		Scan:    UsageScanPoint{Path: "/t/torn-answer.jsonl", Contour: "home", Inode: 111, Size: 100, Offset: 100},
	}); err != nil {
		t.Fatalf("the first scan: %v", err)
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows: []UsageRow{{
			Bucket: hour, Model: "claude-opus-5", Speed: "standard", ServiceTier: "standard",
			Answers: 1, OutputTokens: 358,
		}},
		Since: hour,
		Scan:  UsageScanPoint{Path: "/t/torn-answer.jsonl", Contour: "home", Inode: 111, Size: 400, Offset: 400},
	}); err != nil {
		t.Fatalf("the top-up: %v", err)
	}

	if n := rowCount(t, ctx, pool, "usage_1h", usageEleven); n != 1 {
		t.Fatalf("%d aggregate rows, wanted one: the phantom of the half-written reply is still there", n)
	}
	var answers int
	var out int64
	if err := pool.QueryRow(ctx, `
		SELECT sum(answers)::int, sum(output_tokens) FROM usage_1h WHERE session_id = $1`,
		usageEleven).Scan(&answers, &out); err != nil {
		t.Fatal(err)
	}
	if answers != 1 || out != 358 {
		t.Errorf("after the top-up there are %d answers and %d output, wanted 1 and 358", answers, out)
	}

	older := hour.Add(-2 * time.Hour)
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows:    []UsageRow{{Bucket: older, Model: "claude-opus-5", Answers: 4}},
		Scan:    UsageScanPoint{Path: "/t/torn-answer.jsonl", Contour: "home", Inode: 111, Size: 400, Offset: 400},
	}); err != nil {
		t.Fatalf("the old hour: %v", err)
	}
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: session,
		Rows: []UsageRow{{
			Bucket: hour, Model: "claude-opus-5", Speed: "standard", ServiceTier: "standard", Answers: 2,
		}},
		Since: hour,
		Scan:  UsageScanPoint{Path: "/t/torn-answer.jsonl", Contour: "home", Inode: 111, Size: 500, Offset: 500},
	}); err != nil {
		t.Fatalf("the repeated top-up: %v", err)
	}
	if got := answersOf(t, ctx, pool, usageEleven, older, "claude-opus-5", ""); got != 4 {
		t.Errorf("the wipe from the boundary took an hour older than it: %d answers, wanted 4", got)
	}
}

func TestUsageIdlePausesKeepMedianPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 22, 0, 0, 0, time.UTC)
	err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageTwelfth, Contour: "home"},
		Events: []UsageEventRow{
			{Bucket: hour, Messages: 10, Idle: IdlePauses{
				Under30s: 1, Under2m: 5, Under10m: 1, MaxMS: 9 * 60 * 1000,
			}},
			{Bucket: hour.Add(time.Hour), Messages: 4, Idle: IdlePauses{
				Under2m: 1, Under1h: 1, Over4h: 1, MaxMS: 18 * 60 * 60 * 1000,
			}},
		},
		Scan: UsageScanPoint{Path: "/t/idle.jsonl", Contour: "home", Inode: 121, Size: 10, Offset: 10},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	var maxMS int64
	if err := pool.QueryRow(ctx,
		`SELECT max(idle_ms_max) FROM usage_events_1h WHERE session_id = $1`, usageTwelfth).Scan(&maxMS); err != nil {
		t.Fatal(err)
	}
	if maxMS != 18*60*60*1000 {
		t.Errorf("the pause peak is %d ms, wanted 18 hours", maxMS)
	}

	var median string
	err = pool.QueryRow(ctx, `
		WITH t AS (
			SELECT sum(idle_lt_30s) a, sum(idle_lt_2m) b, sum(idle_lt_10m) c,
			       sum(idle_lt_1h) d, sum(idle_lt_4h) e, sum(idle_ge_4h) f
			  FROM usage_events_1h WHERE session_id = $1
		), x AS (
			SELECT * FROM t, LATERAL (VALUES
				(1, '30s', a), (2, '2m', b), (3, '10m', c),
				(4, '1h', d), (5, '4h', e), (6, 'more', f)) v(i, name, n)
		), acc AS (
			SELECT i, name, sum(n) OVER (ORDER BY i) AS upto, sum(n) OVER () AS total FROM x
		)
		SELECT name FROM acc WHERE upto >= total / 2.0 ORDER BY i LIMIT 1`,
		usageTwelfth).Scan(&median)
	if err != nil {
		t.Fatalf("the median over the buckets: %v", err)
	}
	if median != "2m" {
		t.Errorf("the pause median landed in bucket %q, wanted 2m", median)
	}
}

func TestUsageSubagentFileFillsSessionPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{
			SessionID: usageFourth, Contour: "vendor", CWD: "/srv/proj/lab",
			GitBranch: "main", Version: "2.0.31",
			StartedAt: hour.Add(30 * time.Minute), EndedAt: hour.Add(50 * time.Minute),
		},
		Rows: []UsageRow{{Bucket: hour, Model: "claude-opus-5", Agent: "agent-B", Answers: 6}},
		Scan: UsageScanPoint{Path: "/t/orphan.jsonl", Contour: "vendor", Inode: 41, Size: 300, Offset: 300},
	}); err != nil {
		t.Fatalf("the orphan: %v", err)
	}

	var contour, cwd, branch string
	err := pool.QueryRow(ctx, `
		SELECT contour, cwd, git_branch FROM usage_sessions WHERE session_id = $1`,
		usageFourth).Scan(&contour, &cwd, &branch)
	if err != nil {
		t.Fatalf("the session was not created from the subagent file: %v", err)
	}
	if contour != "vendor" || cwd != "/srv/proj/lab" || branch != "main" {
		t.Errorf("the session directory: %q %q %q", contour, cwd, branch)
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{
			SessionID: usageFourth, Contour: "vendor", CWD: "/srv/proj/lab",
			StartedAt: hour.Add(5 * time.Minute), EndedAt: hour.Add(20 * time.Minute),
		},
		Rows:   []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 11}},
		Events: []UsageEventRow{{Bucket: hour, Messages: 120, Compacts: 2}},
		Scan:   UsageScanPoint{Path: "/t/orphan-parent.jsonl", Contour: "vendor", Inode: 42, Size: 700, Offset: 700},
	}); err != nil {
		t.Fatalf("the parent: %v", err)
	}

	var started, ended time.Time
	if err := pool.QueryRow(ctx, `
		SELECT started_at, ended_at FROM usage_sessions WHERE session_id = $1`, usageFourth).
		Scan(&started, &ended); err != nil {
		t.Fatal(err)
	}
	if !started.Equal(hour.Add(5 * time.Minute)) {
		t.Errorf("the session starts at %s, wanted the earliest of the files", started)
	}
	if !ended.Equal(hour.Add(50 * time.Minute)) {
		t.Errorf("the session ends at %s, wanted the latest of the files", ended)
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageFourth, Contour: "vendor"},
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Agent: "agent-B", Answers: 6}},
		Scan:    UsageScanPoint{Path: "/t/orphan.jsonl", Contour: "vendor", Inode: 41, Size: 400, Offset: 400},
	}); err != nil {
		t.Fatalf("the subagent repeat: %v", err)
	}
	if got := eventSum(t, ctx, pool, usageFourth, "messages"); got != 120 {
		t.Errorf("after the subagent repeat there are %d messages, wanted 120", got)
	}
	if err := pool.QueryRow(ctx,
		`SELECT cwd FROM usage_sessions WHERE session_id = $1`, usageFourth).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	if cwd != "/srv/proj/lab" {
		t.Errorf("the session's directory was overwritten: %q", cwd)
	}
}

func TestUsageSessionCascadeSparesScanPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageFifth, Contour: "home"},
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 2}},
		Events:  []UsageEventRow{{Bucket: hour, Messages: 8}},
		Tools:   []UsageToolRow{{Bucket: hour, Tool: "Edit", Calls: 1}},
		Scan:    UsageScanPoint{Path: "/t/cascade.jsonl", Contour: "home", Inode: 51, Size: 60, Offset: 60},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM usage_sessions WHERE session_id = $1`, usageFifth); err != nil {
		t.Fatalf("removing the session: %v", err)
	}
	for _, table := range []string{"usage_1h", "usage_events_1h", "usage_tools_1h"} {
		if n := rowCount(t, ctx, pool, table, usageFifth); n != 0 {
			t.Errorf("after the session was removed, %s still holds %d rows", table, n)
		}
	}

	points, err := s.UsageScanPoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := points["/t/cascade.jsonl"]; !ok {
		t.Error("removing the session took the scan point — the file will be re-read in full")
	}
}

func TestUsageScanMarksMissingPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 16, 0, 0, 0, time.UTC)
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageSixth, Contour: "home"},
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 3, OutputTokens: 30}},
		Scan:    UsageScanPoint{Path: "/t/gone.jsonl", Contour: "home", Inode: 61, Size: 90, Offset: 90},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	n, err := s.MarkUsageScanMissing(ctx, []string{"/t/gone.jsonl", "/t/never-seen.jsonl"})
	if err != nil {
		t.Fatalf("marking the missing files: %v", err)
	}
	if n != 1 {
		t.Errorf("%d files marked, wanted one — the second is not in the database at all", n)
	}
	again, err := s.MarkUsageScanMissing(ctx, []string{"/t/gone.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("marking again touched %d rows", again)
	}

	if got := answersOf(t, ctx, pool, usageSixth, hour, "claude-opus-5", ""); got != 3 {
		t.Errorf("the file going missing took the usage away: %d answers, wanted 3", got)
	}
	points, err := s.UsageScanPoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if points["/t/gone.jsonl"].MissingAt.IsZero() {
		t.Fatal("the missing file is not marked")
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageSixth, Contour: "home"},
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 4, OutputTokens: 40}},
		Scan:    UsageScanPoint{Path: "/t/gone.jsonl", Contour: "home", Inode: 61, Size: 120, Offset: 120},
	}); err != nil {
		t.Fatalf("the file coming back: %v", err)
	}
	points, err = s.UsageScanPoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !points["/t/gone.jsonl"].MissingAt.IsZero() {
		t.Error("the file that came back is still marked as missing")
	}
}

func TestUsageToolNameKeptWholePG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	long := "mcp__plugin_browser-toolkit_playwright__browser_console_messages"
	longer := long + "__with__a__server__that__names__things__at__length__and__then__some"

	hour := time.Date(2026, 9, 10, 17, 0, 0, 0, time.UTC)
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageSeven, Contour: "home"},
		Tools: []UsageToolRow{
			{Bucket: hour, Tool: long, Calls: 2},
			{Bucket: hour, Tool: longer, Calls: 1},
		},
		Scan: UsageScanPoint{Path: "/t/tools.jsonl", Contour: "home", Inode: 71, Size: 10, Offset: 10},
	}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	for _, name := range []string{long, longer} {
		var got string
		err := pool.QueryRow(ctx, `
			SELECT tool FROM usage_tools_1h WHERE session_id = $1 AND tool = $2`,
			usageSeven, name).Scan(&got)
		if err != nil {
			t.Fatalf("a name of length %d was not found: %v", len(name), err)
		}
		if got != name {
			t.Errorf("the name was truncated: %q", got)
		}
	}
}

func TestUsageWriteIsAllOrNothingPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 18, 0, 0, 0, time.UTC)
	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageEighth, Contour: "home"},
		Rows:    []UsageRow{{Bucket: hour, Model: "claude-opus-5", Answers: 1}},
		Scan:    UsageScanPoint{Path: "/t/torn.jsonl", Contour: "home", Inode: 81, Size: 100, Offset: 100},
	}); err != nil {
		t.Fatalf("the first write: %v", err)
	}

	err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageEighth, Contour: "home"},
		Rows: []UsageRow{
			{Bucket: hour.Add(time.Hour), Model: "claude-opus-5", Answers: 2},
			{Bucket: hour.Add(2 * time.Hour), Model: "claude-opus-5", Answers: 3, LatencyMaxMS: math.MaxInt32 + 1},
		},
		Scan: UsageScanPoint{Path: "/t/torn.jsonl", Contour: "home", Inode: 81, Size: 900, Offset: 900},
	})
	if err == nil {
		t.Fatal("a write with a value that does not fit went through silently")
	}

	if n := rowCount(t, ctx, pool, "usage_1h", usageEighth); n != 1 {
		t.Errorf("after the failed batch there are %d aggregate rows, wanted one — from the first write", n)
	}
	points, perr := s.UsageScanPoints(ctx)
	if perr != nil {
		t.Fatal(perr)
	}
	if got := points["/t/torn.jsonl"].Offset; got != 100 {
		t.Errorf("the failed write moved the scan point to %d, wanted the previous 100", got)
	}
}

func TestUsageSpeedSplitsRowsAndKindDoesNotPG(t *testing.T) {
	ctx, s, pool := usageStore(t)

	hour := time.Date(2026, 9, 10, 19, 0, 0, 0, time.UTC)
	err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageNinth, Contour: "home"},
		Rows: []UsageRow{
			{Bucket: hour, Model: "claude-opus-5", Speed: "standard", ServiceTier: "standard", Answers: 5},
			{Bucket: hour, Model: "claude-opus-5", Speed: "fast", ServiceTier: "standard", Answers: 2},
			{Bucket: hour, Model: "claude-opus-5", Answers: 9},
			{Bucket: hour, Model: "claude-opus-5", Agent: "agent-C", AgentKind: "Explore", Answers: 4},
		},
		Scan: UsageScanPoint{Path: "/t/speed.jsonl", Contour: "home", Inode: 91, Size: 10, Offset: 10},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	if n := rowCount(t, ctx, pool, "usage_1h", usageNinth); n != 4 {
		t.Fatalf("%d aggregate rows, wanted four: the modes do not merge into one", n)
	}
	var fast, plain int
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(sum(answers) FILTER (WHERE speed = 'fast'), 0)::int,
		       coalesce(sum(answers) FILTER (WHERE speed = '' AND agent = ''), 0)::int
		  FROM usage_1h WHERE session_id = $1`, usageNinth).Scan(&fast, &plain); err != nil {
		t.Fatal(err)
	}
	if fast != 2 || plain != 9 {
		t.Errorf("%d answers in fast (wanted 2), %d without a mode (wanted 9)", fast, plain)
	}

	var kinds int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT agent_kind) FILTER (WHERE agent_kind <> '')::int
		  FROM usage_1h WHERE session_id = $1`, usageNinth).Scan(&kinds); err != nil {
		t.Fatal(err)
	}
	if kinds != 1 {
		t.Errorf("%d agent types counted, wanted one", kinds)
	}

	if err := s.WriteUsageFile(ctx, UsageFile{
		Session: UsageSession{SessionID: usageNinth, Contour: "home"},
		Rows: []UsageRow{
			{Bucket: hour, Model: "claude-opus-5", Agent: "agent-C", AgentKind: "php-developer", Answers: 4},
		},
		Scan: UsageScanPoint{Path: "/t/speed.jsonl", Contour: "home", Inode: 91, Size: 20, Offset: 20},
	}); err != nil {
		t.Fatalf("the repeat with a different type: %v", err)
	}
	var kind string
	if err := pool.QueryRow(ctx, `
		SELECT agent_kind FROM usage_1h WHERE session_id = $1 AND agent = 'agent-C'`,
		usageNinth).Scan(&kind); err != nil {
		t.Fatalf("the agent's type: %v", err)
	}
	if kind != "php-developer" {
		t.Errorf("the agent's type is %q, wanted php-developer", kind)
	}
	if n := rowCount(t, ctx, pool, "usage_1h", usageNinth); n != 4 {
		t.Errorf("there are now %d aggregate rows: the agent's type must not multiply rows", n)
	}
}

func usageStore(t *testing.T) (context.Context, *Store, *pgxpool.Pool) {
	t.Helper()
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool := mustPool(t, s)

	t.Cleanup(func() {
		clean := context.Background()
		pool.Exec(clean, `DELETE FROM usage_sessions WHERE session_id::text LIKE '6a0f1e2c-%'`)
		pool.Exec(clean, `DELETE FROM usage_scan WHERE path LIKE '/t/%'`)
	})
	return ctx, s, pool
}

func answersOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, session string, bucket time.Time, model, agent string) int {
	t.Helper()
	var answers int
	err := pool.QueryRow(ctx, `
		SELECT coalesce(sum(answers), 0)::int FROM usage_1h
		 WHERE session_id = $1 AND bucket = $2 AND model = $3 AND agent = $4`,
		session, bucket, model, agent).Scan(&answers)
	if err != nil {
		t.Fatalf("the answers for the hour: %v", err)
	}
	return answers
}

func toolCalls(t *testing.T, ctx context.Context, pool *pgxpool.Pool, session string, bucket time.Time, agent, tool string) int {
	t.Helper()
	var calls int
	err := pool.QueryRow(ctx, `
		SELECT coalesce(sum(calls), 0)::int FROM usage_tools_1h
		 WHERE session_id = $1 AND bucket = $2 AND agent = $3 AND tool = $4`,
		session, bucket, agent, tool).Scan(&calls)
	if err != nil {
		t.Fatalf("the tool's calls: %v", err)
	}
	return calls
}

func eventSum(t *testing.T, ctx context.Context, pool *pgxpool.Pool, session, column string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx,
		`SELECT coalesce(sum(`+column+`), 0)::int FROM usage_events_1h WHERE session_id = $1`,
		session).Scan(&n)
	if err != nil {
		t.Fatalf("the sum of %s: %v", column, err)
	}
	return n
}

func rowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, session string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*)::int FROM `+table+` WHERE session_id = $1`, session).Scan(&n); err != nil {
		t.Fatalf("counting the rows of %s: %v", table, err)
	}
	return n
}
