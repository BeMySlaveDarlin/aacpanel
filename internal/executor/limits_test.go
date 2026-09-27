package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A contour whose snapshot is fresh — a terminal's status line keeps it so —
// is not probed; one past its interval is, and a probe that cannot start says
// which contour it failed.
func TestLimitsAreProbedOnlyWhereTheSnapshotHasAged(t *testing.T) {
	home := t.TempDir()
	fresh, stale := filepath.Join(home, "fresh"), filepath.Join(home, "stale")
	for _, d := range []string{fresh, stale} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fresh, "rate-limits.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(stale, "rate-limits.json")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(home, "registry.conf")
	if err := os.WriteFile(reg, []byte("fresh | "+home+" | "+fresh+"\nstale | "+home+" | "+stale+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AACP_CLAUDE_REGISTRY", reg)
	t.Setenv(probeClaudeEnv, filepath.Join(home, "no-claude-here"))

	failed := RenewLimits(context.Background(), time.Now(), 10*time.Minute)
	if len(failed) != 1 || !strings.HasPrefix(failed[0], "stale:") {
		t.Errorf("the renewal reports %q: only the aged contour is probed", failed)
	}
}

func TestTheIntervalOfTheProbeIsTheHosts(t *testing.T) {
	t.Setenv(limitsEveryEnv, "")
	if LimitsInterval() != LimitsEvery {
		t.Errorf("no setting gives %s, expected %s", LimitsInterval(), LimitsEvery)
	}
	t.Setenv(limitsEveryEnv, "2m")
	if LimitsInterval() != 2*time.Minute {
		t.Errorf("a setting of 2m gives %s", LimitsInterval())
	}
	t.Setenv(limitsEveryEnv, "5s")
	if LimitsInterval() != LimitsEvery {
		t.Errorf("an interval under a minute is taken: %s", LimitsInterval())
	}
}
