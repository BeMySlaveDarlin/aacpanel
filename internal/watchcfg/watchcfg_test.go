package watchcfg

import (
	"strings"
	"testing"
	"time"
)

func TestTheBuiltInConfigIsValid(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) == 0 || len(cfg.Probes) == 0 {
		t.Fatalf("the config describes %d rules and %d probes", len(cfg.Rules), len(cfg.Probes))
	}
}

func TestDurationsReadAsWritten(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cfg.Rules {
		if r.Key == "disk.full" && r.For.std() != 5*time.Minute {
			t.Fatalf("disk.full holds for %s, not 5m", r.For.std())
		}
	}
	for _, p := range cfg.Probes {
		if p.Key == "status.openai" && p.Interval.std() != 5*time.Minute {
			t.Fatalf("status.openai runs every %s, not 5m", p.Interval.std())
		}
	}
}

const oneRule = `
rules:
  - key: host.cpu
    name: Host CPU is loaded
    subject: host.cpu_pct
    op: ">"
    threshold: 90
    for: 10m
    severity: warning
`

func TestConfigRefuses(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		says string
	}{
		{
			name: "a misspelled field",
			yaml: strings.Replace(oneRule, "threshold: 90", "treshold: 90", 1),
			says: "treshold",
		},
		{
			name: "an unknown source",
			yaml: strings.Replace(oneRule, "host.cpu_pct", "host.temperature", 1),
			says: "unknown source",
		},
		{
			name: "a hold shorter than the floor",
			yaml: strings.Replace(oneRule, "for: 10m", "for: 5s", 1),
			says: "shorter",
		},
		{
			name: "a hold that is not a duration",
			yaml: strings.Replace(oneRule, "for: 10m", "for: 600", 1),
			says: "not a duration",
		},
		{
			name: "an unknown severity",
			yaml: strings.Replace(oneRule, "severity: warning", "severity: loud", 1),
			says: "unknown severity",
		},
		{
			name: "an unknown operator",
			yaml: strings.Replace(oneRule, `op: ">"`, `op: "~"`, 1),
			says: "unknown operator",
		},
		{
			name: "a key in two shapes",
			yaml: strings.Replace(oneRule, "key: host.cpu", "key: Host_CPU", 1),
			says: "lowercase",
		},
		{
			name: "no rules at all",
			yaml: "probes: []\n",
			says: "no rules",
		},
		{
			name: "a target on a source that has one object",
			yaml: strings.Replace(oneRule, "subject: host.cpu_pct", "subject: host.cpu_pct\n    target: some-container", 1),
			says: "redundant",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parse([]byte(c.yaml))
			if err == nil {
				t.Fatalf("the config passed with %s", c.name)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the complaint is %q, expected it to mention %q", err, c.says)
			}
		})
	}
}

func TestConfigRefusesADuplicateKey(t *testing.T) {
	twice := oneRule + strings.TrimPrefix(oneRule, "\nrules:\n")
	_, err := parse([]byte(twice))
	if err == nil || !strings.Contains(err.Error(), "taken") {
		t.Fatalf("two rules under one key passed: %v", err)
	}
}

func TestConfigRefusesAKeyHeldByBothLists(t *testing.T) {
	both := oneRule + `
probes:
  - key: host.cpu
    name: Host CPU
    kind: http
    target: https://example.invalid/
    interval: 1m
    timeout: 5s
`
	_, err := parse([]byte(both))
	if err == nil || !strings.Contains(err.Error(), "taken") {
		t.Fatalf("a rule and a probe shared a key: %v", err)
	}
}

const oneProbe = `
rules:
  - key: host.cpu
    name: Host CPU is loaded
    subject: host.cpu_pct
    op: ">"
    threshold: 90
    for: 10m
    severity: warning

probes:
  - key: api.example
    name: API Example
    kind: http
    target: https://example.invalid/
    interval: 1m
    timeout: 5s
`

func TestProbeConfigRefuses(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		says string
	}{
		{
			name: "an unknown kind",
			yaml: strings.Replace(oneProbe, "kind: http", "kind: ping", 1),
			says: "unknown kind",
		},
		{
			name: "components on an http check",
			yaml: strings.Replace(oneProbe, "timeout: 5s", "timeout: 5s\n    components: Something", 1),
			says: "status page",
		},
		{
			name: "an expected code on a status page",
			yaml: strings.Replace(strings.Replace(oneProbe, "kind: http", "kind: status", 1),
				"timeout: 5s", "timeout: 5s\n    expectStatus: 200", 1),
			says: "read as json",
		},
		{
			name: "an interval below the floor",
			yaml: strings.Replace(oneProbe, "interval: 1m", "interval: 1s", 1),
			says: "interval",
		},
		{
			name: "a timeout that outlasts the interval",
			yaml: strings.Replace(strings.Replace(oneProbe, "interval: 1m", "interval: 30s", 1),
				"timeout: 5s", "timeout: 45s", 1),
			says: "outlasts",
		},
		{
			name: "a code that is not an http status",
			yaml: strings.Replace(oneProbe, "timeout: 5s", "timeout: 5s\n    expectStatus: 42", 1),
			says: "not an http status",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parse([]byte(c.yaml))
			if err == nil {
				t.Fatalf("the config passed with %s", c.name)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the complaint is %q, expected it to mention %q", err, c.says)
			}
		})
	}
}

func TestAnOmittedEnabledMeansWatched(t *testing.T) {
	cfg, err := parse([]byte(oneRule))
	if err != nil {
		t.Fatal(err)
	}
	if !on(cfg.Rules[0].Enabled) {
		t.Fatal("a rule that says nothing about enabled came out disabled")
	}
}
