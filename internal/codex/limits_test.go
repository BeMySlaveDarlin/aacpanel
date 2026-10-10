package codex

import (
	"encoding/json"
	"os"
	"testing"

	"aacpanel/internal/codex/codextest"
)

func limitsOf(t *testing.T) (Limits, bool) {
	t.Helper()
	raw, err := os.ReadFile(LimitsPath("acme"))
	if err != nil {
		return Limits{}, false
	}
	var out Limits
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the limits do not read: %v\n%s", err, raw)
	}
	return out, true
}

func window(used float64, minutes, resets int64) map[string]any {
	return map[string]any{"usedPercent": used, "windowDurationMins": minutes, "resetsAt": resets}
}

// The limits of the account are read when the link connects and kept by the
// contour; an update after a turn is sparse, and what it does not say stands.
func TestTheLimitsOfTheAccountAreReadAndUpdated(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	srv.Limits(map[string]any{"limitId": "codex", "limitName": nil, "primary": window(50, 10080, 1791948832),
		"secondary": nil, "credits": nil, "planType": "pro", "rateLimitReachedType": nil})
	l := NewLink(srv.Home, "acme")
	run(t, l)
	until(t, "the limits to be read", func() bool { _, ok := limitsOf(t); return ok })
	lim, _ := limitsOf(t)
	if lim.Contour != "acme" || lim.CodexHome != srv.Home || lim.LimitID != "codex" || lim.Plan != "pro" ||
		lim.Primary == nil || lim.Primary.UsedPercent != 50 || *lim.Primary.Minutes != 10080 ||
		*lim.Primary.ResetsAt != 1791948832 || lim.Secondary != nil || lim.At == 0 {
		t.Errorf("the limits read as %+v", lim)
	}
	if got := srv.Calls("account/rateLimits/read"); len(got) != 1 || params(t, got[0])["excludeResetCreditDetails"] != true {
		t.Errorf("account/rateLimits/read went as %s", got)
	}

	srv.LimitsUpdate(map[string]any{"limitId": "codex", "primary": window(55, 10080, 1791948832), "secondary": nil,
		"planType": nil, "rateLimitReachedType": "rate_limit_reached"})
	until(t, "the update to be merged", func() bool {
		lim, _ := limitsOf(t)
		return lim.Primary != nil && lim.Primary.UsedPercent == 55
	})
	if lim, _ := limitsOf(t); lim.Plan != "pro" || lim.Reached != "rate_limit_reached" {
		t.Errorf("an update that says no plan took it away: %+v", lim)
	}
}

// An account with no limits to tell leaves no file, and the link works on.
func TestAnAccountWithoutLimitsLeavesNoFile(t *testing.T) {
	_, _ = linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	if _, ok := limitsOf(t); ok {
		t.Error("limits the daemon did not tell were written")
	}
}
