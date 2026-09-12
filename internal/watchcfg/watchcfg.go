// Package watchcfg holds what the panel watches out of the box: the alert rules
// and the probes the container runs itself.
//
// The description lives in config.yaml next to this file and is built into the
// binary: the image is distroless, there is no deploy directory inside it, and
// the description has to travel with the code that validates it. Changing a
// threshold or a wording is a change of one line here plus a rebuild, not a
// migration.
//
// The database follows the file, keyed by Key. A record that has lost its entry
// here is disabled, never deleted: alerts hang off their rule with ON DELETE
// CASCADE, and dropping the rule would take the history with it.
package watchcfg

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"

	"aacpanel/internal/rules"
)

//go:embed config.yaml
var source []byte

// keyFormat keeps the keys readable and stable: lowercase words in dot-separated
// segments. They end up in the database and in nothing else, so there is no
// reason for them to look like anything more.
var keyFormat = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`)

const (
	minInterval = 10 * time.Second
	maxInterval = 24 * time.Hour
	minTimeout  = time.Second
	maxTimeout  = time.Minute
)

// Config is the whole file.
type Config struct {
	Rules  []Rule  `yaml:"rules"`
	Probes []Probe `yaml:"probes"`
}

// Rule is one alert rule as it is written in the file.
type Rule struct {
	Key       string   `yaml:"key"`
	Name      string   `yaml:"name"`
	Subject   string   `yaml:"subject"`
	Target    string   `yaml:"target"`
	Op        string   `yaml:"op"`
	Threshold float64  `yaml:"threshold"`
	For       duration `yaml:"for"`
	Severity  string   `yaml:"severity"`
	Enabled   *bool    `yaml:"enabled"`
}

// Probe is one check the container runs on a schedule.
type Probe struct {
	Key          string   `yaml:"key"`
	Name         string   `yaml:"name"`
	Kind         string   `yaml:"kind"`
	Target       string   `yaml:"target"`
	Interval     duration `yaml:"interval"`
	Timeout      duration `yaml:"timeout"`
	ExpectStatus int      `yaml:"expectStatus"`
	Components   string   `yaml:"components"`
	Enabled      *bool    `yaml:"enabled"`
}

// duration reads "5m" and "10s" rather than a bare count of seconds: a hold and
// an interval are read far more often than they are written.
type duration time.Duration

func (d *duration) UnmarshalYAML(n *yaml.Node) error {
	var text string
	if err := n.Decode(&text); err != nil {
		return fmt.Errorf("%q is not a duration such as 30s or 5m", n.Value)
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("%q is not a duration such as 30s or 5m", text)
	}
	if parsed <= 0 {
		return fmt.Errorf("the duration %q is not positive", text)
	}
	*d = duration(parsed)
	return nil
}

func (d duration) std() time.Duration { return time.Duration(d) }

// on reads an omitted enabled as true: the common case is a record that is
// watched, and writing it out on every entry would be noise.
func on(flag *bool) bool { return flag == nil || *flag }

// Load reads the built-in description.
func Load() (Config, error) { return parse(source) }

func parse(b []byte) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	// A typo in a field name would otherwise pass in silence and leave the value
	// at its zero: a misspelled threshold is a rule that fires at nothing.
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("the watch config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if len(c.Rules) == 0 {
		return fmt.Errorf("the watch config describes no rules")
	}
	seen := map[string]string{}
	for _, r := range c.Rules {
		if err := checkKey("rule", r.Key, seen); err != nil {
			return err
		}
		if err := r.check(); err != nil {
			return err
		}
	}
	for _, p := range c.Probes {
		if err := checkKey("probe", p.Key, seen); err != nil {
			return err
		}
		if err := p.check(); err != nil {
			return err
		}
	}
	return nil
}

// checkKey holds the rules and the probes to one namespace. They live in
// separate tables and would not collide there, but a key that means one thing in
// one list and another thing in the other is a trap for the reader.
func checkKey(kind, key string, seen map[string]string) error {
	if key == "" {
		return fmt.Errorf("a %s without a key", kind)
	}
	if !keyFormat.MatchString(key) {
		return fmt.Errorf("%s %q: the key is not lowercase dot-separated words", kind, key)
	}
	if was, dup := seen[key]; dup {
		return fmt.Errorf("the key %q is taken by a %s and a %s at once", key, was, kind)
	}
	seen[key] = kind
	return nil
}

func (r Rule) check() error {
	var target *string
	if r.Target != "" {
		target = &r.Target
	}
	engine := rules.Rule{
		Name:      r.Name,
		Subject:   r.Subject,
		Target:    target,
		Op:        rules.Op(r.Op),
		Threshold: r.Threshold,
		ForSec:    int(r.For.std().Seconds()),
		Severity:  rules.Severity(r.Severity),
	}
	if err := engine.Validate(); err != nil {
		return fmt.Errorf("rule %q: %w", r.Key, err)
	}
	return nil
}

func (p Probe) check() error {
	if p.Name == "" {
		return fmt.Errorf("probe %q: without a name", p.Key)
	}
	if p.Target == "" {
		return fmt.Errorf("probe %q: without a target", p.Key)
	}
	switch p.Kind {
	case "http":
		if p.Components != "" {
			return fmt.Errorf("probe %q: components belong to a status page, not to an http check", p.Key)
		}
		if p.ExpectStatus != 0 && (p.ExpectStatus < 100 || p.ExpectStatus > 599) {
			return fmt.Errorf("probe %q: %d is not an http status", p.Key, p.ExpectStatus)
		}
	case "status":
		if p.ExpectStatus != 0 {
			return fmt.Errorf("probe %q: a status page is read as json, an expected code means nothing here", p.Key)
		}
	default:
		return fmt.Errorf("probe %q: unknown kind %q", p.Key, p.Kind)
	}
	if d := p.Interval.std(); d < minInterval || d > maxInterval {
		return fmt.Errorf("probe %q: the interval of %s is outside %s..%s", p.Key, d, minInterval, maxInterval)
	}
	if d := p.Timeout.std(); d < minTimeout || d > maxTimeout {
		return fmt.Errorf("probe %q: the timeout of %s is outside %s..%s", p.Key, d, minTimeout, maxTimeout)
	}
	if p.Timeout.std() > p.Interval.std() {
		return fmt.Errorf("probe %q: the timeout of %s outlasts the interval of %s",
			p.Key, p.Timeout.std(), p.Interval.std())
	}
	return nil
}
