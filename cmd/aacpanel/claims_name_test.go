package main

import (
	"strings"
	"testing"

	"aacpanel/internal/host"
)

func TestClaimRecipientAcceptsCodexTitleAndRefusesDuplicates(t *testing.T) {
	rows := []claimSession{{LiveSession: host.LiveSession{
		Name: "codex-00000001", Title: "review", SessionID: "thread-one", Agent: "codex"}}}
	for _, name := range []string{"review", "codex-00000001"} {
		if got, err := claimRecipient(rows, name); err != nil || got.SessionID != "thread-one" {
			t.Errorf("recipient %s = %+v, %v", name, got, err)
		}
	}
	rows = append(rows, claimSession{LiveSession: host.LiveSession{Name: "review", SessionID: "claude-one"}})
	if _, err := claimRecipient(rows, "review"); err == nil || !strings.Contains(err.Error(), "ambiguous") ||
		!strings.Contains(err.Error(), "codex-00000001") || !strings.Contains(err.Error(), "claude-one") {
		t.Fatalf("an ambiguous recipient was accepted: %v", err)
	}
}
