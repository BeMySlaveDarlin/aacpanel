package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// UsageSession is a directory row: whose session it is, where it ran and when.
type UsageSession struct {
	SessionID string
	Contour   string
	CWD       string
	GitBranch string
	Version   string
	StartedAt time.Time
	EndedAt   time.Time
}

// UsageRow is one hour of one session's usage: model × subagent × mode.
type UsageRow struct {
	Bucket        time.Time
	Model         string
	Agent         string
	Speed         string
	ServiceTier   string
	AgentKind     string
	Answers       int
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheCreation int64
	Cache1h       int64
	Cache5m       int64
	Iterations    int
	Thinking      int
	LatencySumMS  int64
	LatencyMaxMS  int
}

// UsageEventRow is one hour of a conversation's events.
type UsageEventRow struct {
	Bucket         time.Time
	Agent          string
	Messages       int
	Compacts       int
	Interrupts     int
	InterruptsTool int
	APIErrors      int
	Idle           IdlePauses
}

// IdlePauses are the human's pauses laid out into buckets.
type IdlePauses struct {
	Under30s int
	Under2m  int
	Under10m int
	Under1h  int
	Under4h  int
	Over4h   int
	MaxMS    int64
}

// UsageToolRow holds one tool's calls within an hour.
type UsageToolRow struct {
	Bucket time.Time
	Agent  string
	Tool   string
	Calls  int
	Errors int
}

// UsageScanPoint is what is known about a file on disk.
type UsageScanPoint struct {
	Path      string
	Contour   string
	SessionID string
	Inode     int64
	Size      int64
	Offset    int64
	HeadSum   []byte
	ScannedAt time.Time
	MissingAt time.Time
}

// UsageFile is everything that parsed out of one transcript.
type UsageFile struct {
	Session UsageSession
	Rows    []UsageRow
	Events  []UsageEventRow
	Tools   []UsageToolRow
	Scan    UsageScanPoint

	Since   time.Time
	Rewrite bool
	Agents  []string
}

// WriteUsageFile writes a parsed file and moves its scan point.
func (s *Store) WriteUsageFile(ctx context.Context, f UsageFile) error {
	if f.Scan.Path == "" {
		return badRequest("usage: a file without a path — the scan point has nothing to hang on")
	}
	empty := len(f.Rows) == 0 && len(f.Events) == 0 && len(f.Tools) == 0
	if f.Session.SessionID == "" && !empty {
		return badRequest("usage: %s brought rows without a session", f.Scan.Path)
	}
	if f.Session.SessionID != "" && f.Session.Contour == "" {
		return badRequest("usage: session %s has no contour", f.Session.SessionID)
	}

	pool, err := s.Pool()
	if err != nil {
		return err
	}

	rows := slices.Clone(f.Rows)
	slices.SortFunc(rows, func(a, b UsageRow) int {
		if c := a.Bucket.Compare(b.Bucket); c != 0 {
			return c
		}
		if c := strings.Compare(a.Model, b.Model); c != 0 {
			return c
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	events := slices.Clone(f.Events)
	slices.SortFunc(events, func(a, b UsageEventRow) int {
		if c := a.Bucket.Compare(b.Bucket); c != 0 {
			return c
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	tools := slices.Clone(f.Tools)
	slices.SortFunc(tools, func(a, b UsageToolRow) int {
		if c := a.Bucket.Compare(b.Bucket); c != 0 {
			return c
		}
		if c := strings.Compare(a.Agent, b.Agent); c != 0 {
			return c
		}
		return strings.Compare(a.Tool, b.Tool)
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("usage %s: %w", f.Scan.Path, Unavailable(err))
	}
	defer tx.Rollback(context.Background())

	batch := &pgx.Batch{}
	queueUsageClear(batch, f, rows, events, tools)
	if f.Session.SessionID != "" {
		queueUsageSession(batch, f.Session)
	}
	for _, r := range rows {
		batch.Queue(`
			INSERT INTO usage_1h (session_id, bucket, model, agent, speed, service_tier,
				agent_kind, answers,
				input_tokens, output_tokens, cache_read, cache_creation, cache_1h, cache_5m,
				iterations, thinking, latency_ms_sum, latency_ms_max)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
			ON CONFLICT (session_id, bucket, model, agent, speed, service_tier) DO UPDATE SET
				agent_kind = excluded.agent_kind,
				answers = excluded.answers,
				input_tokens = excluded.input_tokens,
				output_tokens = excluded.output_tokens,
				cache_read = excluded.cache_read,
				cache_creation = excluded.cache_creation,
				cache_1h = excluded.cache_1h,
				cache_5m = excluded.cache_5m,
				iterations = excluded.iterations,
				thinking = excluded.thinking,
				latency_ms_sum = excluded.latency_ms_sum,
				latency_ms_max = excluded.latency_ms_max`,
			f.Session.SessionID, r.Bucket.UTC(), r.Model, r.Agent, r.Speed, r.ServiceTier,
			r.AgentKind, r.Answers,
			r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheCreation, r.Cache1h, r.Cache5m,
			r.Iterations, r.Thinking, r.LatencySumMS, r.LatencyMaxMS)
	}
	for _, e := range events {
		batch.Queue(`
			INSERT INTO usage_events_1h (session_id, bucket, agent,
				messages, compacts, interrupts, interrupts_tool, api_errors,
				idle_lt_30s, idle_lt_2m, idle_lt_10m, idle_lt_1h, idle_lt_4h, idle_ge_4h, idle_ms_max)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (session_id, bucket, agent) DO UPDATE SET
				messages = excluded.messages,
				compacts = excluded.compacts,
				interrupts = excluded.interrupts,
				interrupts_tool = excluded.interrupts_tool,
				api_errors = excluded.api_errors,
				idle_lt_30s = excluded.idle_lt_30s,
				idle_lt_2m = excluded.idle_lt_2m,
				idle_lt_10m = excluded.idle_lt_10m,
				idle_lt_1h = excluded.idle_lt_1h,
				idle_lt_4h = excluded.idle_lt_4h,
				idle_ge_4h = excluded.idle_ge_4h,
				idle_ms_max = excluded.idle_ms_max`,
			f.Session.SessionID, e.Bucket.UTC(), e.Agent,
			e.Messages, e.Compacts, e.Interrupts, e.InterruptsTool, e.APIErrors,
			e.Idle.Under30s, e.Idle.Under2m, e.Idle.Under10m, e.Idle.Under1h,
			e.Idle.Under4h, e.Idle.Over4h, e.Idle.MaxMS)
	}
	for _, t := range tools {
		batch.Queue(`
			INSERT INTO usage_tools_1h (session_id, bucket, agent, tool, calls, errors)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (session_id, bucket, agent, tool) DO UPDATE SET
				calls = excluded.calls,
				errors = excluded.errors`,
			f.Session.SessionID, t.Bucket.UTC(), t.Agent, t.Tool, t.Calls, t.Errors)
	}
	queueUsageScan(batch, f.Scan, f.Session.SessionID)

	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("usage %s: %w", f.Scan.Path, Unavailable(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("usage %s: %w", f.Scan.Path, Unavailable(err))
	}
	return nil
}

func queueUsageClear(batch *pgx.Batch, f UsageFile, rows []UsageRow, events []UsageEventRow, tools []UsageToolRow) {
	if f.Session.SessionID == "" {
		return
	}
	if !f.Rewrite && f.Since.IsZero() {
		return
	}
	agents := f.Agents
	if len(agents) == 0 {
		agents = agentsOf(rows, events, tools)
	}
	var since *time.Time
	if !f.Rewrite {
		since = nullTime(f.Since)
	}
	for _, table := range []string{"usage_1h", "usage_events_1h", "usage_tools_1h"} {
		batch.Queue(`DELETE FROM `+table+`
			 WHERE session_id = $1 AND agent = ANY($2)
			   AND ($3::timestamptz IS NULL OR bucket >= $3)`,
			f.Session.SessionID, agents, since)
	}
}

func queueUsageSession(batch *pgx.Batch, s UsageSession) {
	batch.Queue(`
		INSERT INTO usage_sessions (session_id, contour, cwd, git_branch, version, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (session_id) DO UPDATE SET
			contour = excluded.contour,
			cwd = coalesce(nullif(excluded.cwd, ''), usage_sessions.cwd),
			git_branch = coalesce(nullif(excluded.git_branch, ''), usage_sessions.git_branch),
			version = coalesce(nullif(excluded.version, ''), usage_sessions.version),
			started_at = least(usage_sessions.started_at, excluded.started_at),
			ended_at = greatest(usage_sessions.ended_at, excluded.ended_at)`,
		s.SessionID, s.Contour, s.CWD, s.GitBranch, s.Version,
		nullTime(s.StartedAt), nullTime(s.EndedAt))
}

func queueUsageScan(batch *pgx.Batch, p UsageScanPoint, sessionID string) {
	var sid *string
	if sessionID != "" {
		sid = &sessionID
	}
	batch.Queue(`
		INSERT INTO usage_scan (path, contour, session_id, inode, size, offset_bytes, head_sum, scanned_at, missing_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), NULL)
		ON CONFLICT (path) DO UPDATE SET
			contour = excluded.contour,
			session_id = coalesce(excluded.session_id, usage_scan.session_id),
			inode = excluded.inode,
			size = excluded.size,
			offset_bytes = excluded.offset_bytes,
			head_sum = excluded.head_sum,
			scanned_at = now(),
			missing_at = NULL`,
		p.Path, p.Contour, sid, p.Inode, p.Size, p.Offset, p.HeadSum)
}

func agentsOf(rows []UsageRow, events []UsageEventRow, tools []UsageToolRow) []string {
	seen := map[string]bool{}
	var out []string
	add := func(a string) {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, r := range rows {
		add(r.Agent)
	}
	for _, e := range events {
		add(e.Agent)
	}
	for _, t := range tools {
		add(t.Agent)
	}
	return out
}

// UsageScanPoints returns every scan point, keyed by the file's path.
func (s *Store) UsageScanPoints(ctx context.Context) (map[string]UsageScanPoint, error) {
	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT path, contour, session_id, inode, size, offset_bytes, head_sum, scanned_at, missing_at
		FROM usage_scan`)
	if err != nil {
		return nil, fmt.Errorf("usage scan points: %w", Unavailable(err))
	}
	defer rows.Close()

	out := map[string]UsageScanPoint{}
	for rows.Next() {
		var p UsageScanPoint
		var sid *string
		var missing *time.Time
		if err := rows.Scan(&p.Path, &p.Contour, &sid, &p.Inode, &p.Size,
			&p.Offset, &p.HeadSum, &p.ScannedAt, &missing); err != nil {
			return nil, fmt.Errorf("usage scan points: %w", Unavailable(err))
		}
		if sid != nil {
			p.SessionID = *sid
		}
		if missing != nil {
			p.MissingAt = *missing
		}
		out[p.Path] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage scan points: %w", Unavailable(err))
	}
	return out, nil
}

// MarkUsageScanMissing marks the files that were not found on disk.
func (s *Store) MarkUsageScanMissing(ctx context.Context, paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}
	pool, err := s.Pool()
	if err != nil {
		return 0, err
	}
	tag, err := pool.Exec(ctx, `
		UPDATE usage_scan SET missing_at = now()
		 WHERE path = ANY($1) AND missing_at IS NULL`, paths)
	if err != nil {
		return 0, fmt.Errorf("missing usage files: %w", Unavailable(err))
	}
	return int(tag.RowsAffected()), nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}
