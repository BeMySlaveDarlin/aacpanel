package action

import (
	"context"
	"testing"
)

func TestChildActionsKeepTheCallerAndNeverPermitTools(t *testing.T) {
	const caller = "11111111-1111-4111-8111-111111111111"
	question := &Answer{AskID: "q", Picks: [][]int{{1}}}
	for _, kind := range []Kind{SessionClose, SessionAnswer} {
		request := Request{ID: "parent", Kind: kind, Target: "child", Caller: caller}
		if kind == SessionAnswer {
			request.Answer = question
		}
		if err := request.Validate(); err != nil {
			t.Fatalf("a scoped %s was refused: %v", kind, err)
		}
	}
	if err := (Request{ID: "parent", Kind: SessionPermit, Target: "child", Caller: caller,
		Permit: &Permit{Option: 1, Fingerprint: "p"}}).Validate(); err == nil {
		t.Fatal("a parent can grant a child's permission")
	}
	for _, request := range []Request{
		{Ask: AskOpening, Target: "child"},
		{ID: "parent", Kind: SessionLetter, Target: "parent", ParentQuestion: caller, Conversation: "not-a-conversation", Text: "Question?"},
		{ID: "parent", Kind: SessionLetter, Target: "parent", ParentQuestion: caller, Conversation: caller, From: caller, Text: "Question?"},
	} {
		if err := request.Validate(); err == nil {
			t.Fatalf("an unbound opening or panel letter was accepted: %+v", request)
		}
	}
	if err := (Request{ID: "parent", Kind: SessionLetter, Target: "parent", ParentQuestion: caller,
		Conversation: caller, Text: "Question?"}).Validate(); err != nil {
		t.Fatalf("a panel question letter was refused: %v", err)
	}
}

type openingExecutor struct{}

func (openingExecutor) Execute(context.Context, Request) (string, error) { return "", nil }
func (openingExecutor) Opening(_ context.Context, target, conversation string) (*Opening, error) {
	return &Opening{Name: target, Conversation: conversation, Letters: true}, nil
}

func TestTheOpeningQueryUsesTheExecutorsOwnershipSource(t *testing.T) {
	server := NewServer("", openingExecutor{}, 0)
	const id = "11111111-1111-4111-8111-111111111111"
	response := server.handle(t.Context(), Request{Ask: AskOpening, Target: "child", Conversation: id})
	if !response.OK || response.Opening == nil || response.Opening.Conversation != id || !response.Opening.Letters {
		t.Fatalf("the opening was not passed on: %+v", response)
	}
}
