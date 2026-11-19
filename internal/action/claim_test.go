package action

import (
	"strings"
	"testing"
)

func TestPanelClaimMailRequiresItsIdentityAndBoundRecipient(t *testing.T) {
	const conversation = "11111111-1111-4111-8111-111111111111"
	good := Request{ID: "claim", Kind: SessionLetter, Target: "peer", Claim: strings.Repeat("a", 32), Conversation: conversation, MessageID: conversation, Text: "The panel: a claim is due."}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Claim = "bad" }, func(r *Request) { r.Conversation = "" }, func(r *Request) { r.MessageID = "" },
		func(r *Request) { r.From = conversation }, func(r *Request) { r.Kind = SessionSend },
		func(r *Request) { r.ParentQuestion = conversation }, func(r *Request) { r.Report = &HelperReport{Thread: conversation, Parent: conversation} },
	} {
		bad := good
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted invalid panel claim mail: %+v", bad)
		}
	}
	if Valid(ActWithdraw) {
		t.Fatal("a service withdrawal reached the host action list")
	}
}
