package notify

import (
	"fmt"
	"strings"
	"testing"
)

func TestRebootRecoveryReportsCountsAndCanBeQuieted(t *testing.T) {
	m := RevivalMessage("host-boot", 7, 5, []string{"thread: codex", "heavy: headroom did not recover"})
	if !strings.Contains(m.Body, "revived 5 of 7") || !strings.Contains(m.Body, "thread: codex") || !strings.Contains(m.Body, "heavy: headroom") || m.Severity != Warning || m.Kind != kindOf(m.Tag) {
		t.Fatalf("recovery report: %+v", m)
	}
	if !(Prefs{}).Allows(m) || (Prefs{Off: []string{KindReboot}}).Clean().Allows(m) {
		t.Fatal("the recovery kind cannot be turned off")
	}
	if quiet := quietOf(m.Kind, m.Source); quiet == nil || quiet.Key != KindReboot {
		t.Fatalf("the recovery push cannot quiet its kind: %+v", quiet)
	}
	if all := RevivalMessage("another", 2, 2, nil); all.Severity != Info || strings.Contains(all.Body, "not revived") {
		t.Fatalf("complete recovery: %+v", all)
	}
}

func TestRecoveryReportKeepsNamesWhenManySessionsWait(t *testing.T) {
	var failures []string
	for i := 0; i < 27; i++ {
		failures = append(failures, fmt.Sprintf("project-session-%d: %s", i, strings.Repeat("memory pressure ", 30)))
	}
	m := RevivalMessage("boot", len(failures), 0, failures)
	for i := range failures {
		if !strings.Contains(m.Body, fmt.Sprintf("project-session-%d", i)) {
			t.Fatalf("a conversation disappeared from the push: %s", m.Body)
		}
	}
	if raw, err := payloadOf(m); err != nil || len(raw) > maxPayload {
		t.Fatalf("the recovery report cannot fit a push: %d bytes, %v", len(raw), err)
	}
}
