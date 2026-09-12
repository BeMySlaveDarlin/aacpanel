package rules

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MinForSec is the smallest permitted hold time.
const MinForSec = 10

type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

type Op string

const (
	GT Op = ">"
	GE Op = ">="
	LT Op = "<"
	LE Op = "<="
	EQ Op = "="
)

// Source is a measured quantity.
type Source struct {
	Key      string
	Title    string
	Unit     string
	Targeted bool
}

// Sources is what the rules engine can measure.
var Sources = map[string]Source{
	"container.cpu_pct":   {Key: "container.cpu_pct", Title: "Container CPU", Unit: "%", Targeted: true},
	"container.mem_pct":   {Key: "container.mem_pct", Title: "Container memory against its limit", Unit: "%", Targeted: true},
	"container.mem_bytes": {Key: "container.mem_bytes", Title: "Container memory", Unit: "bytes", Targeted: true},
	"container.unhealthy": {Key: "container.unhealthy", Title: "Container unhealthy", Unit: "flag", Targeted: true},
	"stack.running_pct":   {Key: "stack.running_pct", Title: "Running share of the stack", Unit: "%", Targeted: true},
	"probe.fail_streak":   {Key: "probe.fail_streak", Title: "Probe failures in a row", Targeted: true},
	"host.cpu_pct":        {Key: "host.cpu_pct", Title: "Host CPU", Unit: "%"},
	"host.mem_pct":        {Key: "host.mem_pct", Title: "Host memory", Unit: "%"},
	"host.load1":          {Key: "host.load1", Title: "Load average over a minute"},
	"host.swap_bytes":     {Key: "host.swap_bytes", Title: "Host swap", Unit: "bytes"},
	"host.cpu_temp":       {Key: "host.cpu_temp", Title: "Processor temperature", Unit: "°C"},
	"disk.used_pct":       {Key: "disk.used_pct", Title: "Disk used", Unit: "%", Targeted: true},
	"session.pct":         {Key: "session.pct", Title: "Claude limit spent", Unit: "%", Targeted: true},
}

// Rule is a rule as data: what is measured, against what, for how long and how serious it is.
type Rule struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Subject   string   `json:"subject"`
	Target    *string  `json:"target"`
	Op        Op       `json:"op"`
	Threshold float64  `json:"threshold"`
	ForSec    int      `json:"forSec"`
	Severity  Severity `json:"severity"`
	Enabled   bool     `json:"enabled"`
}

// Source returns the description of the measured quantity.
func (r Rule) Source() (Source, bool) {
	s, ok := Sources[r.Subject]
	return s, ok
}

// Match reports whether the condition holds for this value.
func (r Rule) Match(v float64) bool {
	switch r.Op {
	case GT:
		return v > r.Threshold
	case GE:
		return v >= r.Threshold
	case LT:
		return v < r.Threshold
	case LE:
		return v <= r.Threshold
	case EQ:
		return math.Abs(v-r.Threshold) < 1e-9
	default:
		return false
	}
}

// Validate catches a rule that the schema would let through but the engine would not understand.
func (r Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("rule without a name")
	}
	src, ok := Sources[r.Subject]
	if !ok {
		return fmt.Errorf("rule %q: unknown source %q", r.Name, r.Subject)
	}
	switch r.Op {
	case GT, GE, LT, LE, EQ:
	default:
		return fmt.Errorf("rule %q: unknown operator %q", r.Name, r.Op)
	}
	switch r.Severity {
	case Info, Warning, Critical:
	default:
		return fmt.Errorf("rule %q: unknown severity %q", r.Name, r.Severity)
	}
	if r.ForSec < MinForSec {
		return fmt.Errorf("rule %q: the hold of %d s is shorter than %d s — a momentary spike is no reason to wake the phone",
			r.Name, r.ForSec, MinForSec)
	}
	if r.Target != nil && *r.Target != "" && !src.Targeted {
		return fmt.Errorf("rule %q: the source %q has a single object, target %q is redundant", r.Name, r.Subject, *r.Target)
	}
	if math.IsNaN(r.Threshold) || math.IsInf(r.Threshold, 0) {
		return fmt.Errorf("rule %q: the threshold is not a number", r.Name)
	}
	return nil
}

// Load reads the rules from the database.
func Load(ctx context.Context, pool *pgxpool.Pool) (list []Rule, bad []error, err error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, subject, target, op, threshold, for_sec, severity, enabled
		FROM rules ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Rule, error) {
		var rule Rule
		err := r.Scan(&rule.ID, &rule.Name, &rule.Subject, &rule.Target,
			&rule.Op, &rule.Threshold, &rule.ForSec, &rule.Severity, &rule.Enabled)
		return rule, err
	})
	if err != nil {
		return nil, nil, err
	}

	for _, rule := range all {
		if err := rule.Validate(); err != nil {
			bad = append(bad, err)
			continue
		}
		list = append(list, rule)
	}
	return list, bad, nil
}

// Enabled keeps only the enabled rules.
func Enabled(list []Rule) []Rule {
	out := make([]Rule, 0, len(list))
	for _, r := range list {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out
}
