package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	registry "aacpanel/internal/contours"
	"aacpanel/internal/stream"
)

// LimitsEvery is how old the snapshot of a contour's limits may grow before a
// probe renews it. A terminal of the contour keeps it fresh through its status
// line, and then no probe runs at all.
const LimitsEvery = 10 * time.Minute

// limitsEveryEnv overrides it, in Go duration form: a probe costs a claude
// start and a word on haiku, and how often it is worth that is the host's.
const limitsEveryEnv = "AACP_LIMITS_EVERY"

// probeClaudeEnv names the claude the probe starts: the wrapper of the
// contours, the same one the sessions are started with.
const probeClaudeEnv = "AACP_CLAUDE"

// LimitsInterval is the age past which a contour's snapshot is renewed.
func LimitsInterval() time.Duration {
	if d, err := time.ParseDuration(os.Getenv(limitsEveryEnv)); err == nil && d >= time.Minute {
		return d
	}
	return LimitsEvery
}

// RenewLimits probes every contour whose snapshot of the limits is older than
// every, and writes the snapshot where the collector reads it. It returns what
// went wrong, one line a contour.
func RenewLimits(ctx context.Context, now time.Time, every time.Duration) []string {
	bin := os.Getenv(probeClaudeEnv)
	if bin == "" {
		bin = "claude"
	}
	var failed []string
	for _, c := range registry.Load() {
		if c.Config == "" {
			continue
		}
		path := filepath.Join(c.Config, "rate-limits.json")
		if st, err := os.Stat(path); err == nil && now.Sub(st.ModTime()) < every {
			continue
		}
		dir, err := probeDir(c.Prefix)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", c.Profile, err))
			continue
		}
		snap, err := stream.ProbeLimits(ctx, bin, dir, now)
		if err == nil {
			err = stream.WriteSnapshot(path, snap)
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", c.Profile, err))
		}
	}
	return failed
}

// probeDir is where a probe of a contour starts: the root of the contour, where
// the wrapper of the contours picks its account; the contour of everything
// else starts in a directory of the executor's own.
func probeDir(prefix string) (string, error) {
	if prefix != "" && prefix != "*" {
		if st, err := os.Stat(prefix); err == nil && st.IsDir() {
			return prefix, nil
		}
		return "", fmt.Errorf("the root of the contour %s is not a directory", prefix)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "aacpanel", "limits-probe")
	return dir, os.MkdirAll(dir, 0o700)
}
