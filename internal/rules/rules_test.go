package rules

import (
	"context"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

func TestMatch(t *testing.T) {
	r := func(op Op, threshold float64) Rule {
		return Rule{Name: "test", Subject: "host.cpu_pct", Op: op, Threshold: threshold, ForSec: 60, Severity: Warning}
	}
	cases := []struct {
		rule  Rule
		value float64
		want  bool
	}{
		{r(GT, 90), 91, true},
		{r(GT, 90), 90, false},
		{r(GE, 90), 90, true},
		{r(LT, 50), 49, true},
		{r(LT, 50), 50, false},
		{r(LE, 50), 50, true},
		{r(EQ, 1), 1, true},
		{r(EQ, 1), 0, false},
		{r(EQ, 1), 1.0000000000001, true},
	}
	for _, c := range cases {
		if got := c.rule.Match(c.value); got != c.want {
			t.Errorf("%s %v on value %v returned %v", c.rule.Op, c.rule.Threshold, c.value, got)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Rule{Name: "sound", Subject: "host.cpu_pct", Op: GT, Threshold: 90, ForSec: 60, Severity: Warning}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a sound rule is rejected: %v", err)
	}

	target := "aacpanel"
	cases := []struct {
		name string
		rule Rule
		want string
	}{
		{"no name", Rule{Subject: "host.cpu_pct", Op: GT, ForSec: 60, Severity: Warning}, "without a name"},
		{"a typo in the source", Rule{Name: "x", Subject: "host.cpu", Op: GT, ForSec: 60, Severity: Warning}, "unknown source"},
		{"a broken operator", Rule{Name: "x", Subject: "host.cpu_pct", Op: "~", ForSec: 60, Severity: Warning}, "unknown operator"},
		{"a broken severity", Rule{Name: "x", Subject: "host.cpu_pct", Op: GT, ForSec: 60, Severity: "panic"}, "unknown severity"},
		{"no hold", Rule{Name: "x", Subject: "host.cpu_pct", Op: GT, ForSec: 0, Severity: Warning}, "hold"},
		{"a hold shorter than the metrics step", Rule{Name: "x", Subject: "host.cpu_pct", Op: GT, ForSec: 5, Severity: Warning}, "hold"},
		{"a target on a quantity that has no objects", Rule{Name: "x", Subject: "host.cpu_pct", Target: &target, Op: GT, ForSec: 60, Severity: Warning}, "redundant"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.rule.Validate()
			if err == nil {
				t.Fatal("the rule is accepted although it must not be")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the complaint is %q, expected the fragment %q", err, c.want)
			}
		})
	}
}

func TestDefaultRulesPG(t *testing.T) {
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}

	list, bad, err := Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range bad {
		t.Errorf("a default rule does not pass validation: %v", e)
	}
	if len(list) < 10 {
		t.Fatalf("%d rules loaded, expected at least ten of them from the migration", len(list))
	}

	seen := map[string]bool{}
	for _, r := range list {
		seen[r.Subject] = true
		if r.ForSec < MinForSec {
			t.Errorf("rule %q with a hold of %d s got into the database", r.Name, r.ForSec)
		}
	}
	for _, need := range []string{"host.cpu_pct", "disk.used_pct", "container.mem_pct", "session.pct"} {
		if !seen[need] {
			t.Errorf("there is no default rule for %s", need)
		}
	}

	t.Run("the schema does not accept a rule with no hold", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO rules (key, name, subject, op, threshold, for_sec, severity)
			VALUES ('test.nohold', 'no hold', 'host.cpu_pct', '>', 90, 0, 'warning')`)
		if err == nil {
			pool.Exec(ctx, "DELETE FROM rules WHERE name = 'no hold'")
			t.Fatal("a rule with a zero hold was written")
		}
	})

	t.Run("there cannot be two open alerts on one object", func(t *testing.T) {
		var ruleID int
		if err := pool.QueryRow(ctx, "SELECT id FROM rules ORDER BY id LIMIT 1").Scan(&ruleID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO alerts (rule_id, subject, severity) VALUES ($1, 'dup-test', 'warning')`, ruleID); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, "DELETE FROM alerts WHERE subject = 'dup-test'")

		if _, err := pool.Exec(ctx, `INSERT INTO alerts (rule_id, subject, severity) VALUES ($1, 'dup-test', 'critical')`, ruleID); err == nil {
			t.Error("a second open alert on the same object was written — the «opens and closes» mechanics then rest on the engine alone")
		}

		if _, err := pool.Exec(ctx, "UPDATE alerts SET closed_at = now() WHERE subject = 'dup-test'"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO alerts (rule_id, subject, severity) VALUES ($1, 'dup-test', 'warning')`, ruleID); err != nil {
			t.Errorf("after a close the alert does not open again: %v", err)
		}
	})
}

func TestEverySourceHasAQuery(t *testing.T) {
	for key := range Sources {
		if _, ok := queries[key]; !ok {
			t.Errorf("source %q is offered to a rule, and the engine has nothing to measure it with", key)
		}
	}
	for key := range queries {
		if _, ok := Sources[key]; !ok {
			t.Errorf("the engine measures %q, and no rule can name it", key)
		}
	}
}
