package executor

import (
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

func TestPanelClaimMailUsesPeerInputAndRefusesAReusedName(t *testing.T) {
	e, holder := parentAndChild(t, question(), "")
	r := req(action.SessionLetter, "demo")
	r.Claim, r.Conversation, r.MessageID, r.Text = strings.Repeat("a", 32), streamSID, senderSID, "The panel: claim is due."
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	got := only(t, holder)
	_, name, body := claudeReads(t, got.Text)
	if got.Op != stream.OpLetter || got.UUID != senderSID || name != "panel claim" || body != r.Text {
		t.Fatalf("panel claim delivery: %+v", got)
	}
	r.Target = "parent"
	if _, err := e.Execute(t.Context(), r); err == nil {
		t.Fatal("a claim letter reached a reused recipient name")
	}
}

func TestPanelClaimMailReachesCodexInItsEnvelope(t *testing.T) {
	daemon, e := onCodex(t, nil)
	r := req(action.SessionLetter, codexName)
	r.Claim, r.Conversation, r.MessageID, r.Text = strings.Repeat("b", 32), codexThread, senderSID, "The panel: claim is due."
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	calls := daemon.Calls("turn/start")
	if len(calls) != 1 {
		t.Fatalf("claim turn: %d", len(calls))
	}
	_, name, body, before, _ := envelopeIn(t, turnText(t, calls[0]))
	if name != "panel claim" || body != r.Text || !strings.Contains(before, "did not type it") {
		t.Fatalf("panel letter: %q %q %q", name, body, before)
	}
}
