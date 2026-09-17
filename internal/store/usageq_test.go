package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aacpanel/internal/testdb"
)

const (
	vitrinaHome = "7c1f0e2d-0000-4000-8000-00000000000%d"
	vitrinaHour = "2026-09-10T10:00:00Z"
)

func vitrinaID(n int) string { return fmt.Sprintf(vitrinaHome, n) }

func TestUsageProjectsSumUpToTheContourPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(1), "probe", "/opt/probe/panel", 100, 10)
	seedSession(t, ctx, pool, vitrinaID(2), "probe", "/opt/probe/panel/web", 200, 20)
	seedSession(t, ctx, pool, vitrinaID(3), "probe", "/tmp/scratchpad", 40, 4)

	f := vitrinaFilter()
	rows, err := s.UsageBreakdownFor(ctx, f, UsageByProject, 0)
	if err != nil {
		t.Fatalf("the breakdown by project: %v", err)
	}
	var total int64
	for _, r := range rows {
		total += r.Input
	}
	byContour, err := s.UsageBreakdownFor(ctx, f, UsageByContour, 0)
	if err != nil {
		t.Fatalf("the breakdown by contour: %v", err)
	}
	if len(byContour) != 1 || byContour[0].Input != total {
		t.Fatalf("%d by project, %+v by contour — the sums must add up", total, byContour)
	}
	last := rows[len(rows)-1]
	if !last.Outside {
		t.Fatalf("the last row is %+v, and it must be \"outside the map\"", last)
	}
	if last.Input != 40 {
		t.Errorf("%d tokens outside the map, wanted 40 — the scratchpad's usage", last.Input)
	}
}

func TestUsageOutsideRowStandsEvenAtZeroPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(4), "probe", "/opt/probe/panel", 100, 10)

	rows, err := s.UsageBreakdownFor(ctx, vitrinaFilter(), UsageByProject, 0)
	if err != nil {
		t.Fatalf("the breakdown: %v", err)
	}
	last := rows[len(rows)-1]
	if !last.Outside {
		t.Fatalf("the last row is %+v, wanted \"outside the map\" as a zero row", last)
	}
	if last.Label != "" {
		t.Errorf("label %q — the server does not draw it, it has a flag for that", last.Label)
	}
	if last.Input != 0 {
		t.Errorf("%d tokens outside the map, yet there is nothing outside the map", last.Input)
	}
}

func TestUsageProjectTakesTheLongestPrefixPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(5), "probe", "/opt/probe/panel/web/src", 100, 10)

	rows, err := s.UsageBreakdownFor(ctx, vitrinaFilter(), UsageByProject, 0)
	if err != nil {
		t.Fatalf("the breakdown: %v", err)
	}
	for _, r := range rows {
		if r.Outside {
			continue
		}
		if r.Key != "/opt/probe/panel/web" {
			t.Fatalf("project %q, wanted the longest matching prefix", r.Key)
		}
	}
}

func TestUsageFiltersAreCountedOnTheServerPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(6), "probe", "/opt/probe/panel", 100, 10)
	seedSession(t, ctx, pool, vitrinaID(7), "probe", "/tmp/scratchpad", 40, 4)

	all, err := s.UsageSummaryFor(ctx, vitrinaFilter())
	if err != nil {
		t.Fatalf("the summary: %v", err)
	}
	if all.Input != 140 {
		t.Fatalf("input without a filter is %d, wanted 140", all.Input)
	}

	f := vitrinaFilter()
	f.Project = "/opt/probe/panel"
	one, err := s.UsageSummaryFor(ctx, f)
	if err != nil {
		t.Fatalf("the summary by project: %v", err)
	}
	if one.Input != 100 {
		t.Errorf("input by project is %d, wanted 100: the filter must be applied here", one.Input)
	}

	f = vitrinaFilter()
	f.Outside = true
	outside, err := s.UsageSummaryFor(ctx, f)
	if err != nil {
		t.Fatalf("the summary outside the map: %v", err)
	}
	if outside.Input != 40 {
		t.Errorf("input outside the map is %d, wanted 40", outside.Input)
	}
}

func TestUsageCacheHitUsesTheFullDenominatorPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedSession(t, ctx, pool, vitrinaID(8), "probe", "/tmp/cache", 10, 1)
	mustExec(t, ctx, pool, `UPDATE usage_1h SET cache_read = 700, cache_creation = 290
	                         WHERE session_id = $1`, vitrinaID(8))

	sum, err := s.UsageSummaryFor(ctx, vitrinaFilter())
	if err != nil {
		t.Fatalf("the summary: %v", err)
	}
	if got := sum.Hit; got < 0.69 || got > 0.71 {
		t.Errorf("cache efficiency %.3f, wanted about 0.70 — the denominator covers the whole input", got)
	}
}

func TestUsageDaySeriesCutsDaysInTheZonePG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedSession(t, ctx, pool, vitrinaID(9), "probe", "/tmp/zone", 10, 1)
	mustExec(t, ctx, pool, `UPDATE usage_1h SET bucket = $2 WHERE session_id = $1`,
		vitrinaID(9), time.Date(2026, 9, 10, 22, 0, 0, 0, time.UTC))

	f := vitrinaFilter()
	f.Zone = "Asia/Tokyo"
	points, err := s.UsageSeriesFor(ctx, f, "day")
	if err != nil {
		t.Fatalf("the series by day: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("%d points, wanted one", len(points))
	}
	if got := points[0].At.UTC(); got.Day() != 10 || got.Hour() != 15 {
		t.Errorf("the day started at %s, wanted the 10th at 15:00 UTC — midnight of the 11th at +09", got)
	}
}

func TestUsageToolsKeepTheTailCountedPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedSession(t, ctx, pool, vitrinaID(1), "probe", "/tmp/tools", 10, 1)
	for i, name := range []string{"Bash", "Read", "Edit", "Grep", "mcp__long__name"} {
		mustExec(t, ctx, pool, `
			INSERT INTO usage_tools_1h (session_id, bucket, agent, tool, calls, errors)
			VALUES ($1, $2, '', $3, $4, 1)`,
			vitrinaID(1), vitrinaHour, name, 100-i*10)
	}

	tools, err := s.UsageToolsFor(ctx, vitrinaFilter(), 2)
	if err != nil {
		t.Fatalf("the tools: %v", err)
	}
	if len(tools.Top) != 2 || tools.Top[0].Tool != "Bash" {
		t.Fatalf("top %+v, wanted two names with Bash in front", tools.Top)
	}
	if tools.RestNames != 3 {
		t.Errorf("%d names in the tail, wanted three", tools.RestNames)
	}
	if tools.Calls != 400 {
		t.Errorf("%d calls in all, wanted 400 — over every name, not over the shown ones", tools.Calls)
	}
}

func TestUsageOutsideExpandsToDirectoriesPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(2), "probe", "/tmp/scratchpad", 40, 4)
	seedSession(t, ctx, pool, vitrinaID(3), "probe", "/root", 10, 1)

	f := vitrinaFilter()
	f.Outside = true
	rows, err := s.UsageBreakdownFor(ctx, f, UsageByCWD, 0)
	if err != nil {
		t.Fatalf("the expansion: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d directories, wanted two: %+v", len(rows), rows)
	}
	if rows[0].Key != "/tmp/scratchpad" {
		t.Errorf("%q came first, wanted the most expensive directory", rows[0].Key)
	}
}

func TestUsageWithoutFilterAnswersPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(1), "probe", "/opt/probe/panel", 100, 10)

	f := vitrinaFilter()
	f.Contours = nil

	if _, err := s.UsageSummaryFor(ctx, f); err != nil {
		t.Errorf("the summary without a filter: %v", err)
	}
	if _, err := s.UsageSeriesFor(ctx, f, "hour"); err != nil {
		t.Errorf("the series without a filter: %v", err)
	}
	if _, err := s.UsageSeriesFor(ctx, f, "day"); err != nil {
		t.Errorf("the daily series without a filter: %v", err)
	}
	if _, err := s.UsageModelsFor(ctx, f); err != nil {
		t.Errorf("the models without a filter: %v", err)
	}
	if _, err := s.UsageToolsFor(ctx, f, 5); err != nil {
		t.Errorf("the tools without a filter: %v", err)
	}
	if _, err := s.UsageBreakdownFor(ctx, f, UsageByProject, 0); err != nil {
		t.Errorf("the breakdown without a filter: %v", err)
	}
}

func TestUsageSubShareCountsTheWholeInboundPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedSession(t, ctx, pool, vitrinaID(1), "probe", "/tmp/fan-out", 10, 100)
	mustExec(t, ctx, pool, `
		INSERT INTO usage_1h (session_id, bucket, model, agent, answers,
		                      input_tokens, output_tokens, cache_read)
		VALUES ($1, $2, 'claude-opus-5', 'agent-A', 3, 90, 900, 9900)`,
		vitrinaID(1), vitrinaHour)

	sum, err := s.UsageSummaryFor(ctx, vitrinaFilter())
	if err != nil {
		t.Fatalf("the summary: %v", err)
	}
	if sum.Inbound != 10000 {
		t.Fatalf("total input %d, wanted 10000 — the input plus the cache", sum.Inbound)
	}
	if got := sum.SubShareIn(); got < 0.98 || got > 1.0 {
		t.Errorf("the subagents' share of the input is %.3f, wanted 0.99 — against the whole input, not against the column", got)
	}
	byColumn := float64(sum.Sub.Input) / float64(sum.Input)
	if byColumn > 0.95 {
		t.Fatalf("the probe is built wrong: by the column the share is %.2f, and it has to differ from the total one", byColumn)
	}
}

func TestUsageQueriesAskForAFreshPlan(t *testing.T) {
	src, err := os.ReadFile("usageq.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, "...)") || strings.Contains(line, "planEachTime") {
			continue
		}
		if strings.Contains(line, "pgx.QueryExecModeExec") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		t.Errorf("the query goes out without planEachTime; from the sixth execution it will cost twice as much:\n%s",
			strings.TrimSpace(line))
	}
}

func TestUsageBreakdownOrdersByTheNumberItShowsPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(1), "probe", "/opt/probe/panel", 1000, 10)
	seedSession(t, ctx, pool, vitrinaID(2), "probe", "/opt/probe/panel/web", 10, 10)
	mustExec(t, ctx, pool, `UPDATE usage_1h SET cache_read = 100000 WHERE session_id = $1`,
		vitrinaID(2))

	rows, err := s.UsageBreakdownFor(ctx, vitrinaFilter(), UsageByProject, 0)
	if err != nil {
		t.Fatalf("the breakdown: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("%d rows, wanted two projects and \"outside the map\"", len(rows))
	}
	if rows[0].Key != "/opt/probe/panel/web" {
		t.Errorf("%q came first, yet the largest by total input is the web: it has a hundred thousand of cache",
			rows[0].Key)
	}
	if got := rows[0].Inbound; got != 100010 {
		t.Errorf("the first row's total input is %d, wanted 100010 — the input plus the cache", got)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Inbound > rows[i-1].Inbound {
			t.Fatalf("row %d (%d) is larger than the previous one (%d) — the order is not the one in the numbers",
				i, rows[i].Inbound, rows[i-1].Inbound)
		}
	}
}

func TestUsageModelsOrderByTheWholeInboundPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedSession(t, ctx, pool, vitrinaID(3), "probe", "/tmp/models", 1000, 10)
	mustExec(t, ctx, pool, `
		INSERT INTO usage_1h (session_id, bucket, model, agent, answers,
		                      input_tokens, output_tokens, cache_read)
		VALUES ($1, $2, 'claude-sonnet-5', '', 1, 10, 10, 100000)`,
		vitrinaID(3), vitrinaHour)

	models, err := s.UsageModelsFor(ctx, vitrinaFilter())
	if err != nil {
		t.Fatalf("the models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("%d models, wanted two", len(models))
	}
	if models[0].Model != "claude-sonnet-5" {
		t.Errorf("%q came first, yet the largest by total input is sonnet: it has a hundred thousand of cache",
			models[0].Model)
	}
	if models[0].Inbound <= models[1].Inbound {
		t.Errorf("total input %d against %d — the order is not the one in the numbers",
			models[0].Inbound, models[1].Inbound)
	}
}

func TestUsageHitArrivesReadyInEveryRowPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedMap(t, ctx, pool)
	seedSession(t, ctx, pool, vitrinaID(4), "probe", "/opt/probe/panel", 10, 10)
	mustExec(t, ctx, pool, `UPDATE usage_1h SET cache_read = 700, cache_creation = 290
	                         WHERE session_id = $1`, vitrinaID(4))

	want := 700.0 / 1000.0
	near := func(name string, got float64) {
		t.Helper()
		if got < want-0.01 || got > want+0.01 {
			t.Errorf("%s: cache efficiency %.3f, wanted %.2f — the denominator covers the whole input",
				name, got, want)
		}
	}

	sum, err := s.UsageSummaryFor(ctx, vitrinaFilter())
	if err != nil {
		t.Fatalf("the summary: %v", err)
	}
	near("the summary", sum.Hit)

	points, err := s.UsageSeriesFor(ctx, vitrinaFilter(), "hour")
	if err != nil || len(points) == 0 {
		t.Fatalf("the series: %v", err)
	}
	near("a point of the series", points[0].Hit)

	rows, err := s.UsageBreakdownFor(ctx, vitrinaFilter(), UsageByProject, 0)
	if err != nil || len(rows) == 0 {
		t.Fatalf("the breakdown: %v", err)
	}
	near("a breakdown row", rows[0].Hit)

	models, err := s.UsageModelsFor(ctx, vitrinaFilter())
	if err != nil || len(models) == 0 {
		t.Fatalf("the models: %v", err)
	}
	near("a model row", models[0].Hit)
}

func vitrinaFilter() UsageFilter {
	return UsageFilter{
		From:     time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Contours: []string{"probe"},
	}
}

func vitrina(t *testing.T) (context.Context, *Store, *pgxpool.Pool) {
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

	wipe := func() {
		clean := context.Background()
		mustExec(t, clean, pool, `DELETE FROM usage_sessions WHERE session_id::text LIKE '7c1f0e2d-%'`)
		mustExec(t, clean, pool, `
			DELETE FROM profile_projects WHERE group_id IN (
				SELECT g.id FROM profile_groups g JOIN profiles p ON p.id = g.profile_id
				 WHERE p.name = 'probe')`)
		mustExec(t, clean, pool, `DELETE FROM profiles WHERE name = 'probe'`)
	}
	wipe()
	t.Cleanup(wipe)
	return ctx, s, pool
}

func seedMap(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var profile, group int
	err := pool.QueryRow(ctx, `
		INSERT INTO profiles (name, config_dir) VALUES ('probe', '/home/probe/.claude')
		RETURNING id`).Scan(&profile)
	if err != nil {
		t.Fatalf("the profile: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO profile_groups (profile_id, name) VALUES ($1, 'Probes') RETURNING id`,
		profile).Scan(&group)
	if err != nil {
		t.Fatalf("the group: %v", err)
	}
	for name, path := range map[string]string{
		"panel": "/opt/probe/panel",
		"web":   "/opt/probe/panel/web",
	} {
		mustExec(t, ctx, pool, `
			INSERT INTO profile_projects (group_id, name, path) VALUES ($1, $2, $3)`,
			group, name, path)
	}
}

func seedSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, contour, cwd string, in, out int64) {
	t.Helper()
	mustExec(t, ctx, pool, `
		INSERT INTO usage_sessions (session_id, contour, cwd, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $4)`, id, contour, cwd, vitrinaHour)
	mustExec(t, ctx, pool, `
		INSERT INTO usage_events_1h (session_id, bucket, agent, messages, compacts, interrupts)
		VALUES ($1, $2, '', 4, 1, 1)`, id, vitrinaHour)
	mustExec(t, ctx, pool, `
		INSERT INTO usage_1h (session_id, bucket, model, agent, answers, input_tokens, output_tokens)
		VALUES ($1, $2, 'claude-opus-5', '', 2, $3, $4)`, id, vitrinaHour, in, out)
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seeding the data: %v", err)
	}
}

// seedProfile puts one profile with one group and its projects on the map, and
// returns the id of the group.
func seedProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	name, configDir, prefix, group string, projects map[string]string) int {
	t.Helper()
	var profile, groupID int
	err := pool.QueryRow(ctx, `
		INSERT INTO profiles (name, config_dir, prefix) VALUES ($1, $2, $3) RETURNING id`,
		name, configDir, prefix).Scan(&profile)
	if err != nil {
		t.Fatalf("the profile %s: %v", name, err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO profile_groups (profile_id, name) VALUES ($1, $2) RETURNING id`,
		profile, group).Scan(&groupID)
	if err != nil {
		t.Fatalf("the group %s: %v", group, err)
	}
	for project, path := range projects {
		mustExec(t, ctx, pool, `
			INSERT INTO profile_projects (group_id, name, path) VALUES ($1, $2, $3)`,
			groupID, project, path)
	}
	// The database outlives the test: what a test puts on the map it takes off
	// again, or the next one fails on a name that is already taken.
	t.Cleanup(func() {
		clean := context.Background()
		mustExec(t, clean, pool, `DELETE FROM profile_projects WHERE group_id = $1`, groupID)
		mustExec(t, clean, pool, `DELETE FROM profile_groups WHERE id = $1`, groupID)
		mustExec(t, clean, pool, `DELETE FROM profiles WHERE id = $1`, profile)
	})
	return groupID
}

// contourFilter is vitrinaFilter for the contours a test seeds itself.
func contourFilter(contours ...string) UsageFilter {
	f := vitrinaFilter()
	f.Contours = contours
	return f
}

// The contour of a session is the name the machine gives the account, and the
// profile of the map is the name a person gave it. They are different words,
// and a session placed by name landed outside the map — which is where two
// thirds of this host used to end up.
func TestUsagePlacesASessionWhoseContourIsNotTheProfileNamePG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	seedProfile(t, ctx, pool, "Schoolwork", "/home/probe/.claude-profiles/algo", "/opt/algo",
		"Backend", map[string]string{"lms": "/opt/algo/lms"})
	seedSession(t, ctx, pool, vitrinaID(1), "algo", "/opt/algo/lms", 100, 10)
	seedSession(t, ctx, pool, vitrinaID(2), "algo", "/tmp/worktree", 40, 4)

	rows, err := s.UsageBreakdownFor(ctx, contourFilter("algo"), UsageByProject, 0)
	if err != nil {
		t.Fatalf("the breakdown by project: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows, wanted the project and the row outside the map: %+v", len(rows), rows)
	}
	if rows[0].Label != "lms" || rows[0].Input != 100 {
		t.Errorf("the first row is %+v, wanted lms with its 100 tokens", rows[0])
	}
	if !rows[len(rows)-1].Outside || rows[len(rows)-1].Input != 40 {
		t.Errorf("outside the map: %+v, wanted the 40 tokens of the worktree and nothing else",
			rows[len(rows)-1])
	}
}

// Two profiles may each have a group called Common, and they are two groups:
// keyed by name they would be added up into one row belonging to neither.
func TestUsageKeepsTwoGroupsOfTheSameNameApartPG(t *testing.T) {
	ctx, s, pool := vitrina(t)
	algo := seedProfile(t, ctx, pool, "Schoolwork", "/home/probe/.claude-profiles/algo", "/opt/algo",
		"Common", map[string]string{"ai-platform": "/opt/algo/ai-platform"})
	evirma := seedProfile(t, ctx, pool, "The client", "/home/probe/.claude-profiles/evirma", "/opt/evirma",
		"Common", map[string]string{"ai-platform": "/opt/evirma/ai-platform"})
	seedSession(t, ctx, pool, vitrinaID(3), "algo", "/opt/algo/ai-platform", 100, 10)
	seedSession(t, ctx, pool, vitrinaID(4), "evirma", "/opt/evirma/ai-platform", 200, 20)

	rows, err := s.UsageBreakdownFor(ctx, contourFilter("algo", "evirma"), UsageByGroup, 0)
	if err != nil {
		t.Fatalf("the breakdown by group: %v", err)
	}
	named := map[string]int64{}
	for _, r := range rows {
		if r.Outside {
			continue
		}
		named[r.Key] = r.Input
	}
	if len(named) != 2 {
		t.Fatalf("%d groups, wanted two of the same name apart: %+v", len(named), rows)
	}
	if named[fmt.Sprint(algo)] != 100 || named[fmt.Sprint(evirma)] != 200 {
		t.Errorf("groups %+v — each keeps the usage of its own profile", named)
	}

	// And the filter of a group reaches one of them, not both.
	f := contourFilter("algo", "evirma")
	f.Group = fmt.Sprint(evirma)
	one, err := s.UsageBreakdownFor(ctx, f, UsageByProject, 0)
	if err != nil {
		t.Fatalf("the breakdown inside a group: %v", err)
	}
	if len(one) != 2 || one[0].Label != "ai-platform" || one[0].Input != 200 {
		t.Errorf("inside the group of the client: %+v, wanted its ai-platform alone", one)
	}
}
