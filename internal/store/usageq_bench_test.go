package store

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUsageQueryTimingsPG(t *testing.T) {
	if os.Getenv("AACP_USAGE_BENCH") == "" {
		t.Skip("the dashboard measurement runs on demand: AACP_USAGE_BENCH=1")
	}
	ctx, s, pool := vitrina(t)
	seedBench(t, ctx, pool)

	now := time.Now().UTC()
	month := UsageFilter{From: now.AddDate(0, 0, -30), To: now, Contours: []string{"probe"}}
	all := UsageFilter{From: time.Unix(0, 0), To: now, Contours: []string{"probe"}}
	byProject := month
	byProject.Project = "/opt/probe/project-7"

	cases := []struct {
		name string
		want string
		run  func() error
	}{
		{"the hourly series over 30 days", "10.5 ms", func() error {
			_, err := s.UsageSeriesFor(ctx, month, "hour")
			return err
		}},
		{"the cache by day, 90 days, in the machine's zone", "6.0 ms", func() error {
			f := all
			f.From = now.AddDate(0, 0, -90)
			f.Zone = "Asia/Tokyo"
			_, err := s.UsageSeriesFor(ctx, f, "day")
			return err
		}},
		{"the same in UTC — the price of the zone", "6.0 ms", func() error {
			f := all
			f.From = now.AddDate(0, 0, -90)
			_, err := s.UsageSeriesFor(ctx, f, "day")
			return err
		}},
		{"the summary over 30 days", "—", func() error {
			_, err := s.UsageSummaryFor(ctx, month)
			return err
		}},
		{"the top projects over 30 days", "10.6 ms", func() error {
			_, err := s.UsageBreakdownFor(ctx, month, UsageByProject, 0)
			return err
		}},
		{"the top sessions over 30 days", "7.0 ms", func() error {
			_, err := s.UsageBreakdownFor(ctx, month, UsageBySession, 20)
			return err
		}},
		{"everything by contour, the whole history", "13.2 ms", func() error {
			_, err := s.UsageBreakdownFor(ctx, all, UsageByContour, 0)
			return err
		}},
		{"the models over a month", "4.6 ms", func() error {
			_, err := s.UsageModelsFor(ctx, month)
			return err
		}},
		{"the top tools", "18.0 ms", func() error {
			_, err := s.UsageToolsFor(ctx, month, 5)
			return err
		}},
		{"the same without a filter — the price of the join", "18.0 ms", func() error {
			f := month
			f.Contours = nil
			_, err := s.UsageToolsFor(ctx, f, 5)
			return err
		}},
		{"the cache by day once more, in the zone", "6.0 ms", func() error {
			f := all
			f.From = now.AddDate(0, 0, -90)
			f.Zone = "Asia/Tokyo"
			_, err := s.UsageSeriesFor(ctx, f, "day")
			return err
		}},
		{"the breakdown of one project", "16 ms", func() error {
			_, err := s.UsageBreakdownFor(ctx, byProject, UsageBySession, 20)
			return err
		}},
	}

	for _, c := range cases {
		if err := c.run(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		times := make([]time.Duration, 0, 7)
		for i := 0; i < 7; i++ {
			start := time.Now()
			if err := c.run(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			times = append(times, time.Since(start))
		}
		slices.Sort(times)
		t.Logf("%-38s median %6.1f ms (promised %s)", c.name,
			float64(times[len(times)/2].Microseconds())/1000, c.want)
	}
}

func seedBench(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	const (
		sessions = 1200
		hours    = 60
		tools    = 5
		span     = 60 * 24
	)
	seedMapBench(t, ctx, pool)

	start := time.Now().UTC().Truncate(time.Hour).Add(-span * time.Hour)
	step := float64(span-hours) / float64(sessions)
	ids := make([]string, sessions)
	starts := make([]time.Time, sessions)
	sessionRows := make([][]any, 0, sessions)
	for i := range ids {
		ids[i] = fmt.Sprintf("7c1f0e2d-1000-4000-8000-%012d", i)
		cwd := fmt.Sprintf("/opt/probe/project-%d/subdirectory", i%30)
		if i%6 == 0 {
			cwd = fmt.Sprintf("/tmp/scratchpad-%d", i%50)
		}
		starts[i] = start.Add(time.Duration(float64(i)*step) * time.Hour)
		sessionRows = append(sessionRows, []any{ids[i], "probe", cwd, "main", "2.1.0",
			starts[i], starts[i].Add(time.Hour * hours)})
	}
	copyInto(t, ctx, pool, "usage_sessions",
		[]string{"session_id", "contour", "cwd", "git_branch", "version", "started_at", "ended_at"},
		sessionRows)

	models := []string{"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5"}
	names := []string{"Bash", "Read", "Edit", "Grep", "mcp__plugin__browser_console_messages"}

	hourRows := make([][]any, 0, sessions*hours)
	toolRows := make([][]any, 0, sessions*hours*tools)
	eventRows := make([][]any, 0, sessions*hours)
	for h := 0; h < hours; h++ {
		for i := range ids {
			bucket := starts[i].Add(time.Duration(h) * time.Hour)
			agent := ""
			if i%3 == 0 {
				agent = fmt.Sprintf("agent-%02d", i%7)
			}
			hourRows = append(hourRows, []any{ids[i], bucket, models[i%len(models)], agent,
				"standard", "standard", "general-purpose", 12,
				int64(1200 + i), int64(3400 + h), int64(900000 + i*7), int64(40000 + h),
				0, 0, 12, 3, int64(7500 * 12), 23100})
			eventRows = append(eventRows, []any{ids[i], bucket, agent, 24, 1, 0, 0, 0})
			for k := 0; k < tools; k++ {
				toolRows = append(toolRows, []any{ids[i], bucket, agent, names[k], 3 + k, k % 2})
			}
		}
	}
	byBucket := func(a, b []any) int { return a[1].(time.Time).Compare(b[1].(time.Time)) }
	slices.SortStableFunc(hourRows, byBucket)
	slices.SortStableFunc(eventRows, byBucket)
	slices.SortStableFunc(toolRows, byBucket)

	copyInto(t, ctx, pool, "usage_1h",
		[]string{"session_id", "bucket", "model", "agent", "speed", "service_tier",
			"agent_kind", "answers", "input_tokens", "output_tokens", "cache_read",
			"cache_creation", "cache_1h", "cache_5m", "iterations", "thinking",
			"latency_ms_sum", "latency_ms_max"}, hourRows)
	copyInto(t, ctx, pool, "usage_events_1h",
		[]string{"session_id", "bucket", "agent", "messages", "compacts",
			"interrupts", "interrupts_tool", "api_errors"}, eventRows)
	copyInto(t, ctx, pool, "usage_tools_1h",
		[]string{"session_id", "bucket", "agent", "tool", "calls", "errors"}, toolRows)

	mustExec(t, ctx, pool, `ANALYZE usage_1h`)
	mustExec(t, ctx, pool, `ANALYZE usage_tools_1h`)
	mustExec(t, ctx, pool, `ANALYZE usage_events_1h`)
	mustExec(t, ctx, pool, `ANALYZE usage_sessions`)
	t.Logf("seeded: %d hours, %d tools, %d sessions", len(hourRows), len(toolRows), len(ids))
}

func seedMapBench(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var profile int
	if err := pool.QueryRow(ctx, `
		INSERT INTO profiles (name, config_dir) VALUES ('probe', '/home/probe/.claude')
		RETURNING id`).Scan(&profile); err != nil {
		t.Fatalf("the profile: %v", err)
	}
	for g := 0; g < 3; g++ {
		var group int
		if err := pool.QueryRow(ctx, `
			INSERT INTO profile_groups (profile_id, name) VALUES ($1, $2) RETURNING id`,
			profile, fmt.Sprintf("group-%d", g)).Scan(&group); err != nil {
			t.Fatalf("the group: %v", err)
		}
		for p := g; p < 30; p += 3 {
			mustExec(t, ctx, pool, `
				INSERT INTO profile_projects (group_id, name, path) VALUES ($1, $2, $3)`,
				group, fmt.Sprintf("project-%d", p), fmt.Sprintf("/opt/probe/project-%d", p))
		}
	}
}

func copyInto(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, cols []string, rows [][]any) {
	t.Helper()
	n, err := pool.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(rows))
	if err != nil {
		t.Fatalf("seeding %s: %v", table, err)
	}
	if int(n) != len(rows) {
		t.Fatalf("seeding %s: %d of %d landed", table, n, len(rows))
	}
}
