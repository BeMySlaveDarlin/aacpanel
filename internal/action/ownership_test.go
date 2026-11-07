package action

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

const ownerFrom = "11111111-1111-4111-8111-111111111111"
const ownerTo = "22222222-2222-4222-8222-222222222222"

type passingRestarter struct {
	calls atomic.Int32
	fail  bool
}

func (e *passingRestarter) Execute(context.Context, Request) (string, error) {
	return "", fmt.Errorf("restart lost its result method")
}
func (e *passingRestarter) Restart(context.Context, Request) (string, *OwnerTransfer, error) {
	e.calls.Add(1)
	pass := &OwnerTransfer{From: ownerFrom, Name: "parent", Conversation: ownerTo}
	if e.fail {
		return "", pass, fmt.Errorf("the replacement started but closing the old turn failed")
	}
	return "restarted", pass, nil
}

func TestRestartReturnsOwnerTransfer(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			exec := &passingRestarter{fail: fail}
			client := serve(t, exec)
			req := Request{ID: "restart", Kind: SessionRestart, Target: "parent", Conversation: ownerFrom}
			for range 2 {
				reply, err := client.Do(t.Context(), req)
				if err != nil || reply.OK == fail || reply.Ownership == nil || *reply.Ownership != (OwnerTransfer{From: ownerFrom, Name: "parent", Conversation: ownerTo}) {
					t.Fatalf("the transfer did not survive the reply: %+v %v", reply, err)
				}
			}
			if exec.calls.Load() != 1 {
				t.Fatalf("a repeated request passed ownership %d times", exec.calls.Load())
			}
		})
	}
}

func TestOwnerTransferNamesDistinctConversations(t *testing.T) {
	good := OwnerTransfer{From: ownerFrom, Name: "parent", Conversation: ownerTo}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []OwnerTransfer{{From: "invalid", Name: "parent", Conversation: ownerTo}, {From: ownerFrom, Name: "parent", Conversation: ownerFrom}, {From: ownerFrom, Name: "../parent", Conversation: ownerTo}} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid transfer: %+v", bad)
		}
	}
}
