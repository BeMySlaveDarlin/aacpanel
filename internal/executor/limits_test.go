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

// The executor looks once a minute: a snapshot that would age past its
// interval before the next look is renewed at this one, so an interval of a
// minute renews at every look and not at every second.
func TestASnapshotAgingPastItsIntervalByTheNextLookIsRenewedNow(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "minute")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(config, "rate-limits.json")
	if err := os.WriteFile(snap, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	last := time.Now().Add(-50 * time.Second)
	if err := os.Chtimes(snap, last, last); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(home, "registry.conf")
	if err := os.WriteFile(reg, []byte("minute | "+home+" | "+config+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AACP_CLAUDE_REGISTRY", reg)
	t.Setenv(probeClaudeEnv, filepath.Join(home, "no-claude-here"))

	if failed := RenewLimits(context.Background(), time.Now(), time.Minute); len(failed) != 1 {
		t.Errorf("a snapshot 50 s old under an interval of a minute is not probed (%q): it renews at every second look", failed)
	}
}

func TestTheIntervalOfTheProbeIsTheHosts(t *testing.T) {
	t.Setenv(limitsEveryEnv, "")
	if LimitsInterval() != time.Minute {
		t.Errorf("no setting gives %s, expected a minute", LimitsInterval())
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

func TestQuestionClassifierRunsInTheSessionsContour(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "account")
	t.Setenv(sessionsEnv, "")
	t.Setenv("AACP_CLAUDE_HOME", "")
	reg := filepath.Join(root, "registry")
	if err := os.WriteFile(reg, []byte("work|"+root+"|"+config+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registryEnv, reg)
	procFS(t, fakeProc{pid: 7001, comm: "claude", args: []string{"claude"}, start: "71"})
	sessionFilesIn(t, filepath.Join(config, "sessions"), fakeSession{pid: 7001, name: "demo", start: "71", sid: senderSID})
	bin := filepath.Join(root, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat > candidate.json\nprintf '%s' '{\"result\":\"yes\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(probeClaudeEnv, bin)
	e, _ := newTest(t, "")
	yes, err := e.Question(t.Context(), "demo", senderSID, "Publish?")
	if err != nil || !yes {
		t.Fatalf("classification: %v %v", yes, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "candidate.json"))
	if err != nil || !strings.Contains(string(raw), "Publish?") {
		t.Fatalf("wrong contour: %s %v", raw, err)
	}
	if _, err := e.Question(t.Context(), "demo", streamSID, "Publish?"); err == nil {
		t.Fatal("a replacement conversation spent a call")
	}
}
