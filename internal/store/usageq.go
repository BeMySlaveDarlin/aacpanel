package store

import (
	"context"

	"fmt"
	"github.com/jackc/pgx/v5"
	"regexp"
	"time"
)

// UsageFilter is the dashboard's shared filter.
type UsageFilter struct {
	From     time.Time
	To       time.Time
	Contours []string
	Group    string
	Project  string
	Outside  bool
	Session  string
	Zone     string
}

var zoneRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+/-]{0,63}$`)

func (f UsageFilter) zone() (string, error) {
	if f.Zone == "" {
		return "UTC", nil
	}
	if !zoneRe.MatchString(f.Zone) {
		return "", badRequest("usage: %q does not look like an IANA time zone", f.Zone)
	}
	return f.Zone, nil
}

// A session is placed on the map by its directory. The contour it carries is
// the name the machine gives the account — the directory of its configuration,
// as the wrapper registry spells it — while a profile of the map carries the
// name a person gave it, and the two are the same word only by chance: the
// account in the directory named "algo" is called "Schoolwork" on the screen.
// Matching them by name put everything but the personal contour outside the map.
//
// A session run in a git worktree is placed by the main checkout the agent
// found for it: the worktree lies beside the repository, not inside it.
//
// A group is keyed by its id, not by its name: two profiles may both have a
// group called Common, and they are two groups.
const sessionCTE = `
	sess AS (
		SELECT s.session_id, s.contour, s.cwd, s.started_at, s.ended_at,
		       m.project, m.grp, m.grp_id, m.path
		  FROM usage_sessions s
		  CROSS JOIN LATERAL (SELECT coalesce(nullif(s.checkout, ''), s.cwd) AS dir) d
		  LEFT JOIN LATERAL (
		       SELECT p.name AS project, g.name AS grp, g.id AS grp_id, p.path AS path
		         FROM profile_projects p
		         JOIN profile_groups g ON g.id = p.group_id
		         JOIN profiles pr ON pr.id = g.profile_id
		        WHERE d.dir = p.path OR d.dir LIKE p.path || '/%'
		        ORDER BY length(p.path) DESC, length(pr.prefix) DESC, pr.sort, p.id
		        LIMIT 1
		  ) m ON true
		 WHERE (cardinality($3::text[]) = 0 OR s.contour = ANY($3))
		   AND ($4 = '' OR m.grp_id::text = $4)
		   AND ($5 = '' OR m.path = $5)
		   AND (NOT $6::boolean OR m.project IS NULL)
		   AND ($7 = '' OR s.session_id::text = $7)
	)`

func (f UsageFilter) args() []any {
	if f.empty() {
		return []any{f.From.UTC(), f.To.UTC()}
	}
	return f.mapArgs()
}

func (f UsageFilter) mapArgs() []any {
	contours := f.Contours
	if contours == nil {
		contours = []string{}
	}
	return []any{f.From.UTC(), f.To.UTC(), contours, f.Group, f.Project, f.Outside, f.Session}
}

func (f UsageFilter) empty() bool {
	return len(f.Contours) == 0 && f.Group == "" && f.Project == "" && !f.Outside && f.Session == ""
}

func planEachTime(args []any) []any {
	return append([]any{pgx.QueryExecModeExec}, args...)
}

// UsageTotals are one slice's numbers.
type UsageTotals struct {
	Answers       int64   `json:"answers"`
	Input         int64   `json:"input"`
	Output        int64   `json:"output"`
	CacheRead     int64   `json:"cacheRead"`
	CacheCreation int64   `json:"cacheCreation"`
	Cache1h       int64   `json:"cache1h"`
	Cache5m       int64   `json:"cache5m"`
	Thinking      int64   `json:"thinking"`
	Inbound       int64   `json:"inbound"`
	Hit           float64 `json:"hit"`
}

// SubShareIn gives the subagents' share of the input.
func (s UsageSummary) SubShareIn() float64  { return share(s.Sub.Inbound, s.Inbound) }
func (s UsageSummary) SubShareOut() float64 { return share(s.Sub.Output, s.Output) }

func share(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

func (t *UsageTotals) fillHit() { t.Hit = share(t.CacheRead, t.Inbound) }

// UsageSummary is the period's summary.
type UsageSummary struct {
	UsageTotals
	Sessions     int64       `json:"sessions"`
	Sub          UsageTotals `json:"sub"`
	Agents       int64       `json:"agents"`
	Kinds        int64       `json:"kinds"`
	LatencyAvgMS int64       `json:"latencyAvgMs"`
	LatencyMaxMS int64       `json:"latencyMaxMs"`
	Messages     int64       `json:"messages"`
	Compacts     int64       `json:"compacts"`
	Interrupts   int64       `json:"interrupts"`
	APIErrors    int64       `json:"apiErrors"`
}

const usageSums = `
	sum(u.answers)::bigint, sum(u.input_tokens)::bigint, sum(u.output_tokens)::bigint,
	sum(u.cache_read)::bigint, sum(u.cache_creation)::bigint,
	sum(u.cache_1h)::bigint, sum(u.cache_5m)::bigint, sum(u.thinking)::bigint,
	sum(u.input_tokens + u.cache_read + u.cache_creation)::bigint`

const usageSumsAs = `
	sum(u.answers)::bigint AS answers, sum(u.input_tokens)::bigint AS input,
	sum(u.output_tokens)::bigint AS output, sum(u.cache_read)::bigint AS cache_read,
	sum(u.cache_creation)::bigint AS cache_creation, sum(u.cache_1h)::bigint AS cache_1h,
	sum(u.cache_5m)::bigint AS cache_5m, sum(u.thinking)::bigint AS thinking,
	sum(u.input_tokens + u.cache_read + u.cache_creation)::bigint AS inbound`

// UsageSummaryFor returns the period's numbers in one query.
func (s *Store) UsageSummaryFor(ctx context.Context, f UsageFilter) (UsageSummary, error) {
	var out UsageSummary
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	head, from := "WITH span AS (", "usage_1h u"
	if !f.empty() {
		head, from = "WITH"+sessionCTE+", span AS (", "usage_1h u JOIN sess ON sess.session_id = u.session_id"
	}
	row := pool.QueryRow(ctx, head+`
			SELECT u.session_id, u.agent, u.agent_kind, u.answers, u.input_tokens,
			       u.output_tokens, u.cache_read, u.cache_creation, u.cache_1h,
			       u.cache_5m, u.thinking, u.latency_ms_sum, u.latency_ms_max
			  FROM `+from+`
			 WHERE u.bucket >= $1 AND u.bucket < $2
		)
		SELECT coalesce(sum(u.answers), 0)::bigint,
		       coalesce(sum(u.input_tokens), 0)::bigint,
		       coalesce(sum(u.output_tokens), 0)::bigint,
		       coalesce(sum(u.cache_read), 0)::bigint,
		       coalesce(sum(u.cache_creation), 0)::bigint,
		       coalesce(sum(u.cache_1h), 0)::bigint,
		       coalesce(sum(u.cache_5m), 0)::bigint,
		       coalesce(sum(u.thinking), 0)::bigint,
		       coalesce(sum(u.input_tokens + u.cache_read + u.cache_creation), 0)::bigint,
		       coalesce(sum(u.answers) FILTER (WHERE u.agent <> ''), 0)::bigint,
		       coalesce(sum(u.input_tokens) FILTER (WHERE u.agent <> ''), 0)::bigint,
		       coalesce(sum(u.output_tokens) FILTER (WHERE u.agent <> ''), 0)::bigint,
		       coalesce(sum(u.cache_read) FILTER (WHERE u.agent <> ''), 0)::bigint,
		       coalesce(sum(u.cache_creation) FILTER (WHERE u.agent <> ''), 0)::bigint,
		       coalesce(sum(u.input_tokens + u.cache_read + u.cache_creation)
		                FILTER (WHERE u.agent <> ''), 0)::bigint,
		       coalesce(sum(u.latency_ms_sum), 0)::bigint,
		       coalesce(max(u.latency_ms_max), 0)::bigint
		  FROM span u`, planEachTime(f.args())...)
	t := &out.UsageTotals
	if err := row.Scan(&t.Answers, &t.Input, &t.Output, &t.CacheRead, &t.CacheCreation,
		&t.Cache1h, &t.Cache5m, &t.Thinking, &t.Inbound,
		&out.Sub.Answers, &out.Sub.Input, &out.Sub.Output, &out.Sub.CacheRead, &out.Sub.CacheCreation,
		&out.Sub.Inbound, &out.LatencyAvgMS, &out.LatencyMaxMS); err != nil {
		return out, fmt.Errorf("usage totals: %w", Unavailable(err))
	}

	row = pool.QueryRow(ctx, head+`
			SELECT u.session_id, u.agent, u.agent_kind
			  FROM `+from+`
			 WHERE u.bucket >= $1 AND u.bucket < $2
			 GROUP BY 1, 2, 3
		)
		SELECT count(DISTINCT session_id)::bigint,
		       count(DISTINCT agent) FILTER (WHERE agent <> '')::bigint,
		       count(DISTINCT agent_kind) FILTER (WHERE agent_kind <> '')::bigint
		  FROM span`, planEachTime(f.args())...)
	if err := row.Scan(&out.Sessions, &out.Agents, &out.Kinds); err != nil {
		return out, fmt.Errorf("subagent fan-out: %w", Unavailable(err))
	}

	t.fillHit()
	out.Sub.fillHit()
	if t.Answers > 0 {
		out.LatencyAvgMS /= t.Answers
	}

	events := "usage_events_1h e"
	head2 := ""
	if !f.empty() {
		head2 = "WITH" + sessionCTE + " "
		events = "usage_events_1h e JOIN sess ON sess.session_id = e.session_id"
	}
	row = pool.QueryRow(ctx, head2+`
		SELECT coalesce(sum(e.compacts), 0)::bigint,
		       coalesce(sum(e.interrupts + e.interrupts_tool), 0)::bigint,
		       coalesce(sum(e.api_errors), 0)::bigint,
		       coalesce(sum(e.messages), 0)::bigint
		  FROM `+events+`
		 WHERE e.bucket >= $1 AND e.bucket < $2`, planEachTime(f.args())...)
	if err := row.Scan(&out.Compacts, &out.Interrupts, &out.APIErrors, &out.Messages); err != nil {
		return out, fmt.Errorf("session events: %w", Unavailable(err))
	}
	return out, nil
}

// UsagePoint is a point of the time series.
type UsagePoint struct {
	At time.Time `json:"at"`
	UsageTotals
}

// UsageSeriesFor returns the series for the chart.
func (s *Store) UsageSeriesFor(ctx context.Context, f UsageFilter, step string) ([]UsagePoint, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	zone, err := f.zone()
	if err != nil {
		return nil, err
	}

	bucket := "u.bucket"
	switch step {
	case "", "hour":
	case "day":
		bucket = fmt.Sprintf("date_trunc('day', u.bucket, $%d)", len(f.args())+1)
	default:
		return nil, badRequest("usage: step %q is not understood, it is hour or day", step)
	}

	args := f.args()
	if step == "day" {
		args = append(args, zone)
	}
	from := "usage_1h u"
	if !f.empty() {
		from = "usage_1h u JOIN sess ON sess.session_id = u.session_id"
	}
	query := "SELECT " + bucket + ` AS at, ` + usageSums + `
		  FROM ` + from + `
		 WHERE u.bucket >= $1 AND u.bucket < $2
		 GROUP BY 1 ORDER BY 1`
	if !f.empty() {
		query = "WITH" + sessionCTE + " " + query
	}

	rows, err := pool.Query(ctx, query, planEachTime(args)...)
	if err != nil {
		return nil, fmt.Errorf("usage series: %w", Unavailable(err))
	}
	defer rows.Close()

	out := []UsagePoint{}
	for rows.Next() {
		var p UsagePoint
		if err := rows.Scan(&p.At, &p.Answers, &p.Input, &p.Output, &p.CacheRead,
			&p.CacheCreation, &p.Cache1h, &p.Cache5m, &p.Thinking, &p.Inbound); err != nil {
			return nil, fmt.Errorf("usage series: %w", Unavailable(err))
		}
		p.fillHit()
		p.At = p.At.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage series: %w", Unavailable(err))
	}
	return out, nil
}

// UsageSlice is a breakdown's row.
type UsageSlice struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Contour  string `json:"contour,omitempty"`
	Outside  bool   `json:"outside,omitempty"`
	Sessions int64  `json:"sessions"`
	UsageTotals
}

const (
	UsageByContour = "contour"
	UsageByGroup   = "group"
	UsageByProject = "project"
	UsageBySession = "session"
	UsageByCWD     = "cwd"
)

// UsageBreakdownFor returns the breakdown for a level.
func (s *Store) UsageBreakdownFor(ctx context.Context, f UsageFilter, by string, limit int) ([]UsageSlice, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	var key, label string
	switch by {
	case UsageByContour:
		key, label = "sess.contour", "sess.contour"
	case UsageByGroup:
		key, label = "coalesce(sess.grp_id::text, '')", "coalesce(sess.grp, '')"
	case UsageByProject:
		key, label = "coalesce(sess.path, '')", "coalesce(sess.project, '')"
	case UsageBySession:
		key, label = "sess.session_id::text", "coalesce(sess.project, sess.cwd)"
	case UsageByCWD:
		key, label = "sess.cwd", "sess.cwd"
	default:
		return nil, badRequest("usage: breakdown %q is not understood", by)
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}

	rows, err := pool.Query(ctx, `
		WITH`+sessionCTE+`,
		per AS (
			SELECT u.session_id,`+usageSumsAs+`
			  FROM usage_1h u
			 WHERE u.bucket >= $1 AND u.bucket < $2
			 GROUP BY 1
		)
		SELECT `+key+` AS k, min(`+label+`) AS name, min(sess.contour) AS contour,
		       bool_and(sess.project IS NULL) AS outside,
		       sum(per.answers)::bigint, sum(per.input)::bigint, sum(per.output)::bigint,
		       sum(per.cache_read)::bigint, sum(per.cache_creation)::bigint,
		       sum(per.cache_1h)::bigint, sum(per.cache_5m)::bigint, sum(per.thinking)::bigint,
		       sum(per.inbound)::bigint,
		       count(*)::bigint
		  FROM per JOIN sess USING (session_id)
		 GROUP BY 1
		 ORDER BY sum(per.inbound) DESC, 1
		 LIMIT $8`, planEachTime(append(f.mapArgs(), limit))...)
	if err != nil {
		return nil, fmt.Errorf("usage breakdown: %w", Unavailable(err))
	}
	defer rows.Close()

	out := []UsageSlice{}
	for rows.Next() {
		var sl UsageSlice
		if err := rows.Scan(&sl.Key, &sl.Label, &sl.Contour, &sl.Outside,
			&sl.Answers, &sl.Input, &sl.Output, &sl.CacheRead, &sl.CacheCreation,
			&sl.Cache1h, &sl.Cache5m, &sl.Thinking, &sl.Inbound, &sl.Sessions); err != nil {
			return nil, fmt.Errorf("usage breakdown: %w", Unavailable(err))
		}
		sl.fillHit()
		out = append(out, sl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage breakdown: %w", Unavailable(err))
	}
	if by == UsageByProject || by == UsageByGroup {
		out = withOutside(out)
	}
	return out, nil
}

func withOutside(rows []UsageSlice) []UsageSlice {
	out := make([]UsageSlice, 0, len(rows)+1)
	outside := UsageSlice{Outside: true}
	for _, r := range rows {
		if r.Outside {
			outside = r
			continue
		}
		out = append(out, r)
	}
	return append(out, outside)
}

// UsageModelSlice is one model's share.
type UsageModelSlice struct {
	Model string `json:"model"`
	UsageTotals
}

// UsageModelsFor returns the models and the "main session / subagents" split.
func (s *Store) UsageModelsFor(ctx context.Context, f UsageFilter) ([]UsageModelSlice, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	from := "usage_1h u"
	if !f.empty() {
		from = "usage_1h u JOIN sess ON sess.session_id = u.session_id"
	}
	query := "SELECT u.model," + usageSums + `
		  FROM ` + from + `
		 WHERE u.bucket >= $1 AND u.bucket < $2
		 GROUP BY 1 ORDER BY sum(u.input_tokens + u.cache_read + u.cache_creation) DESC`
	if !f.empty() {
		query = "WITH" + sessionCTE + " " + query
	}
	rows, err := pool.Query(ctx, query, planEachTime(f.args())...)
	if err != nil {
		return nil, fmt.Errorf("usage models: %w", Unavailable(err))
	}
	defer rows.Close()

	out := []UsageModelSlice{}
	for rows.Next() {
		var m UsageModelSlice
		if err := rows.Scan(&m.Model, &m.Answers, &m.Input, &m.Output, &m.CacheRead,
			&m.CacheCreation, &m.Cache1h, &m.Cache5m, &m.Thinking, &m.Inbound); err != nil {
			return nil, fmt.Errorf("usage models: %w", Unavailable(err))
		}
		m.fillHit()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage models: %w", Unavailable(err))
	}
	return out, nil
}

// UsageToolRowTop is a row of the tools top.
type UsageToolRowTop struct {
	Tool   string `json:"tool"`
	Calls  int64  `json:"calls"`
	Errors int64  `json:"errors"`
}

// UsageTools is the tools top plus what did not fit into it.
type UsageTools struct {
	Top       []UsageToolRowTop `json:"top"`
	RestNames int               `json:"restNames"`
	RestCalls int64             `json:"restCalls"`
	Calls     int64             `json:"calls"`
	Errors    int64             `json:"errors"`
}

// UsageToolsFor returns the tools top for the period.
func (s *Store) UsageToolsFor(ctx context.Context, f UsageFilter, limit int) (UsageTools, error) {
	var out UsageTools
	pool, err := s.Pool()
	if err != nil {
		return out, err
	}
	if limit <= 0 {
		limit = 5
	}
	from := "usage_tools_1h t"
	if !f.empty() {
		from = "usage_tools_1h t JOIN sess ON sess.session_id = t.session_id"
	}
	query := `SELECT t.tool, sum(t.calls)::bigint, sum(t.errors)::bigint
		  FROM ` + from + `
		 WHERE t.bucket >= $1 AND t.bucket < $2
		 GROUP BY 1 ORDER BY 2 DESC, 1`
	if !f.empty() {
		query = "WITH" + sessionCTE + " " + query
	}
	rows, err := pool.Query(ctx, query, planEachTime(f.args())...)
	if err != nil {
		return out, fmt.Errorf("usage tools: %w", Unavailable(err))
	}
	defer rows.Close()

	out.Top = []UsageToolRowTop{}
	for rows.Next() {
		var t UsageToolRowTop
		if err := rows.Scan(&t.Tool, &t.Calls, &t.Errors); err != nil {
			return out, fmt.Errorf("usage tools: %w", Unavailable(err))
		}
		out.Calls += t.Calls
		out.Errors += t.Errors
		if len(out.Top) < limit {
			out.Top = append(out.Top, t)
			continue
		}
		out.RestNames++
		out.RestCalls += t.Calls
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("usage tools: %w", Unavailable(err))
	}
	return out, nil
}

// UsageContours reports which contours exist in what was collected.
func (s *Store) UsageContours(ctx context.Context) ([]string, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT contour FROM usage_sessions ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("usage contours: %w", Unavailable(err))
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("usage contours: %w", Unavailable(err))
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UsageScanAt reports when the last collection ran and how many files it covers.
func (s *Store) UsageScanAt(ctx context.Context) (time.Time, int, error) {
	pool, err := s.Pool()
	if err != nil {
		return time.Time{}, 0, err
	}
	var at *time.Time
	var files int
	err = pool.QueryRow(ctx, `
		SELECT max(scanned_at), count(*)::int FROM usage_scan WHERE missing_at IS NULL`).
		Scan(&at, &files)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("last usage scan: %w", Unavailable(err))
	}
	if at == nil {
		return time.Time{}, files, nil
	}
	return at.UTC(), files, nil
}
