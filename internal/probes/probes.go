// Package probes runs availability checks against tunnels, services and external APIs.
package probes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/store"
)

const (
	tickEvery   = 5 * time.Second
	reloadEvery = time.Minute
	jitterShare = 0.1
)

// Outcome says why a check ended the way it did.
type Outcome string

const (
	OK      Outcome = "ok"
	Network Outcome = "network"
	Timeout Outcome = "timeout"
	Status  Outcome = "status"
	// Degraded means working, but worse than usual.
	Degraded Outcome = "degraded"
	Config   Outcome = "config"
)

type Probe struct {
	ID           int
	Name         string
	Kind         string
	Target       string
	Interval     time.Duration
	Timeout      time.Duration
	Runner       string
	Enabled      bool
	ExpectStatus int
	Components   []string
}

// Result is what came out of a check.
type Result struct {
	OK        bool
	Outcome   Outcome
	LatencyMS int
	Err       string
}

// Scheduler runs the checks that are performed from the container.
type Scheduler struct {
	store  *store.Store
	client *http.Client
}

func New(s *store.Store) *Scheduler {
	return &Scheduler{
		store: s,
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{DisableKeepAlives: true},
		},
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	var list []Probe
	var loadedAt time.Time
	next := map[int]time.Time{}
	started := time.Now()

	tick := time.NewTicker(tickEvery)
	defer tick.Stop()

	for {
		now := time.Now()
		if now.Sub(loadedAt) >= reloadEvery {
			fresh, err := s.load(ctx)
			switch {
			case err == nil:
				list, loadedAt = fresh, now
			case ctx.Err() != nil, errors.Is(err, store.ErrClosed):
			case errors.Is(err, store.ErrUnavailable) && time.Since(started) < time.Minute:
			default:
				log.Printf("probes: the list of probes: %v", err)
			}
		}

		for _, p := range list {
			due, seen := next[p.ID]
			if !seen {
				next[p.ID] = now.Add(time.Duration(rand.Float64() * float64(p.Interval)))
				continue
			}
			if now.Before(due) {
				continue
			}
			next[p.ID] = now.Add(jitter(p.Interval))
			go s.runOne(ctx, p)
		}

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func splitComponents(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func jitter(d time.Duration) time.Duration {
	spread := float64(d) * jitterShare
	return d + time.Duration((rand.Float64()*2-1)*spread)
}

func (s *Scheduler) runOne(ctx context.Context, p Probe) {
	res := Check(ctx, s.client, p)
	if err := s.save(ctx, p, res); err != nil && ctx.Err() == nil {
		if !errors.Is(err, store.ErrUnavailable) && !errors.Is(err, store.ErrClosed) {
			log.Printf("probes: writing the result of %q: %v", p.Name, err)
		}
	}
}

// Check performs a single check.
func Check(ctx context.Context, client *http.Client, p Probe) Result {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	started := time.Now()
	var err error
	var status int
	var page statusPage
	var pageErr error

	switch p.Kind {
	case "tcp":
		var conn net.Conn
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", p.Target)
		if conn != nil {
			conn.Close()
		}
	case "http", "status":
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, p.Target, nil)
		if err != nil {
			return Result{Outcome: Config, Err: err.Error(), LatencyMS: 0}
		}
		req.Header.Set("User-Agent", "aacpanel-probe")
		var resp *http.Response
		resp, err = client.Do(req)
		if resp != nil {
			status = resp.StatusCode
			if p.Kind == "status" && err == nil {
				page, pageErr = readStatusPage(resp.Body)
			}
			resp.Body.Close()
		}
	default:
		return Result{Outcome: Config, Err: fmt.Sprintf("unknown probe kind %q", p.Kind)}
	}

	latency := int(time.Since(started).Milliseconds())

	if err != nil {
		out := Network
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			out = Timeout
		}
		return Result{Outcome: out, LatencyMS: latency, Err: shorten(err.Error())}
	}
	if p.Kind == "status" {
		return statusPageResult(status, page, pageErr, latency, p.Components)
	}
	if p.Kind == "http" && !statusOK(p.ExpectStatus, status) {
		if p.ExpectStatus != 0 {
			return Result{Outcome: Status, LatencyMS: latency,
				Err: fmt.Sprintf("answered %d, expected %d", status, p.ExpectStatus)}
		}
		return Result{Outcome: Status, LatencyMS: latency, Err: fmt.Sprintf("answered %d", status)}
	}
	return Result{OK: true, Outcome: OK, LatencyMS: latency}
}

func statusOK(expect, status int) bool {
	if expect != 0 {
		return status == expect
	}
	return status >= 200 && status < 400
}

type statusPage struct {
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Components []statusComponent `json:"components"`
}

type statusComponent struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

const statusBodyMax = 64 << 10

func readStatusPage(body io.Reader) (statusPage, error) {
	var page statusPage
	err := json.NewDecoder(io.LimitReader(body, statusBodyMax)).Decode(&page)
	return page, err
}

func statusPageResult(status int, page statusPage, pageErr error, latency int, watch []string) Result {
	if status != http.StatusOK {
		return Result{Outcome: Status, LatencyMS: latency,
			Err: fmt.Sprintf("the status page answered %d", status)}
	}
	if pageErr != nil {
		return Result{Outcome: Config, LatencyMS: latency,
			Err: shorten("the answer cannot be parsed: " + pageErr.Error())}
	}
	if len(watch) > 0 {
		return watchedResult(page, watch, latency)
	}
	if page.Status.Indicator == "" {
		return Result{Outcome: Config, LatencyMS: latency,
			Err: "no status.indicator in the answer — the address is not a statuspage"}
	}
	if page.Status.Indicator == "none" {
		return Result{OK: true, Outcome: OK, LatencyMS: latency}
	}
	text := severity(page.Status.Indicator)
	if d := strings.TrimSpace(page.Status.Description); d != "" {
		text += ": " + d
	}
	return Result{Outcome: indicatorOutcome(page.Status.Indicator), LatencyMS: latency, Err: shorten(text)}
}

func indicatorOutcome(indicator string) Outcome {
	if indicator == "minor" {
		return Degraded
	}
	return Status
}

func watchedResult(page statusPage, watch []string, latency int) Result {
	if len(page.Components) == 0 {
		return Result{Outcome: Config, LatencyMS: latency,
			Err: "no components in the answer — the address is not a statuspage summary.json"}
	}

	worst := OK
	var bad []string
	for _, want := range watch {
		found := false
		for _, c := range page.Components {
			if !strings.EqualFold(strings.TrimSpace(c.Name), want) {
				continue
			}
			found = true
			out := componentOutcome(c.Status)
			if out == OK {
				continue
			}
			bad = append(bad, c.Name+": "+componentSeverity(c.Status))
			if worse(out, worst) {
				worst = out
			}
		}
		if !found {
			return Result{Outcome: Config, LatencyMS: latency, Err: notOnPage(want, page.Components)}
		}
	}
	if worst == OK {
		return Result{OK: true, Outcome: OK, LatencyMS: latency}
	}
	return Result{Outcome: worst, LatencyMS: latency, Err: shorten(strings.Join(bad, "; "))}
}

func notOnPage(want string, comps []statusComponent) string {
	const lead = "; the page has: "
	head := fmt.Sprintf("component %q is not on the page — renamed or set wrong", want)
	names := pageNames(comps, want, errMax-len(head)-len(lead))
	if names == "" {
		return shorten(head)
	}
	return head + lead + names
}

func pageNames(comps []statusComponent, want string, budget int) string {
	lower := strings.ToLower(want)
	ordered := make([]string, 0, len(comps))
	var rest []string
	for _, c := range comps {
		if strings.Contains(strings.ToLower(c.Name), lower) {
			ordered = append(ordered, c.Name)
			continue
		}
		rest = append(rest, c.Name)
	}
	ordered = append(ordered, rest...)

	var out strings.Builder
	shown := 0
	for _, name := range ordered {
		piece := fmt.Sprintf("%q", name)
		if shown > 0 {
			piece = ", " + piece
		}
		tail := 0
		if left := len(ordered) - shown - 1; left > 0 {
			tail = len(andMore(left))
		}
		if out.Len()+len(piece)+tail > budget {
			break
		}
		out.WriteString(piece)
		shown++
	}
	if shown == 0 {
		return ""
	}
	if left := len(ordered) - shown; left > 0 {
		out.WriteString(andMore(left))
	}
	return out.String()
}

func andMore(n int) string {
	return fmt.Sprintf(" and %d more", n)
}

func componentOutcome(status string) Outcome {
	switch status {
	case "operational":
		return OK
	case "degraded_performance", "under_maintenance":
		return Degraded
	default:
		return Status
	}
}

func worse(a, b Outcome) bool {
	return rank(a) > rank(b)
}

func rank(o Outcome) int {
	switch o {
	case OK:
		return 0
	case Degraded:
		return 1
	default:
		return 2
	}
}

func componentSeverity(status string) string {
	switch status {
	case "degraded_performance":
		return "degraded performance"
	case "partial_outage":
		return "partial outage"
	case "major_outage":
		return "outage"
	case "under_maintenance":
		return "maintenance"
	default:
		return status
	}
}

func severity(indicator string) string {
	switch indicator {
	case "minor":
		return "minor incident"
	case "major":
		return "major incident"
	case "critical":
		return "critical incident"
	default:
		return indicator
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

const errMax = 200

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= errMax {
		return s
	}
	cut := errMax
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut]
}

func (s *Scheduler) load(ctx context.Context) ([]Probe, error) {
	pool, err := s.store.Pool()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, `
		SELECT id, name, kind, target, interval_sec, timeout_sec, runner, enabled, expect_status, components
		FROM probes WHERE enabled AND runner = 'service' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Probe, error) {
		var p Probe
		var interval, timeout int
		var expect *int
		var components *string
		err := r.Scan(&p.ID, &p.Name, &p.Kind, &p.Target, &interval, &timeout, &p.Runner, &p.Enabled, &expect, &components)
		if expect != nil {
			p.ExpectStatus = *expect
		}
		if components != nil {
			p.Components = splitComponents(*components)
		}
		p.Interval = time.Duration(interval) * time.Second
		p.Timeout = time.Duration(timeout) * time.Second
		return p, err
	})
}

func (s *Scheduler) save(ctx context.Context, p Probe, res Result) error {
	pool, err := s.store.Pool()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var errText *string
	if res.Err != "" {
		errText = &res.Err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO probe_results (ts, probe_id, ok, latency_ms, outcome, error)
		VALUES (now(), $1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
		p.ID, res.OK, res.LatencyMS, string(res.Outcome), errText)
	return err
}
