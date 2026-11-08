package codex

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
)

const (
	methodReviewStarted   = "item/autoApprovalReview/started"
	methodReviewCompleted = "item/autoApprovalReview/completed"
	methodStrictReview    = "autoApprovalReview/strictReviewRequired"
)

func reviewPayload(turn, review, item, status string) map[string]any {
	return map[string]any{"turnId": turn, "reviewId": review, "targetItemId": item, "startedAtMs": 1000,
		"review": map[string]any{"status": status}, "action": map[string]any{"type": "command"}}
}

func reviewReached(t *testing.T, l *Link, id, status string) {
	t.Helper()
	until(t, "the review event", func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		s := l.reviews[id]
		return s != nil && s.items["review"].Review.Status == status
	})
}

func wireRequestReached(t *testing.T, l *Link, id string) {
	t.Helper()
	until(t, "the wire request", func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return len(l.pending[id]) == 1
	})
}

func noOwnerApproval(t *testing.T, l *Link, id string) {
	t.Helper()
	until(t, "no owner approval in the state", func() bool {
		st, _, ok := stateOf(t, id)
		return ok && st.Busy && len(st.Waiting) == 0 && st.Ask == nil
	})
	if len(l.Pending(id)) != 0 {
		t.Fatal("a guardian request is exposed as an owner request")
	}
}

func autoReviewed(t *testing.T) (*codextest.Server, *Link, string) {
	t.Helper()
	b := Begin{CWD: "/srv/proj", Sandbox: "workspace-write", Hold: true}
	b.ReviewAutomatically()
	srv, l, id := started(t, b)
	// Reviews belong to a running turn: an idle thread read clears them.
	srv.Running(id, "turn")
	stateSays(t, id, "the running turn before its review", func(st State) bool { return st.Busy })
	return srv, l, id
}

func TestGuardianReviewLastsUntilTheThreadIsIdle(t *testing.T) {
	srv, l, id := autoReviewed(t)
	srv.Notify(id, methodReviewStarted, reviewPayload("turn", "review", "item", "inProgress"))
	reviewReached(t, l, id, "inProgress")
	for _, active := range []bool{true, false} {
		if !active {
			srv.Set(id, "idle")
		}
		// Read after the event so seen must reconcile it with a newer snapshot,
		// even when the test reaches the event before the next scheduled poll.
		asked := time.Now()
		info, err := read(t.Context(), mustClient(t, l), id)
		if err != nil {
			t.Fatal(err)
		}
		l.seen(info, asked)
		l.mu.Lock()
		kept := l.reviews[id] != nil
		l.mu.Unlock()
		if kept != active {
			t.Fatalf("the review after the %s thread read: kept=%t, want %t", info.Status.Type, kept, active)
		}
	}
}

func TestGuardianReviewKeepsApprovalsAndFlagsAwayFromTheOwner(t *testing.T) {
	for _, requestFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "review before request", true: "request before review"}[requestFirst], func(t *testing.T) {
			srv, l, id := autoReviewed(t)
			var request int
			ask := func() {
				request = srv.Ask(id, methodCommand, map[string]any{"turnId": "turn", "itemId": "item", "startedAtMs": 1000, "command": "touch result"})
				wireRequestReached(t, l, id)
			}
			if requestFirst {
				ask()
			}
			srv.Notify(id, methodReviewStarted, reviewPayload("turn", "review", "item", "inProgress"))
			reviewReached(t, l, id, "inProgress")
			if !requestFirst {
				ask()
			}
			noOwnerApproval(t, l, id)
			if err := l.Respond(t.Context(), id, strconv.Itoa(request), json.RawMessage(`"accept"`)); err != ErrAnswered {
				t.Fatalf("the panel answered the guardian's request: %v", err)
			}
			complete := reviewPayload("turn", "review", "item", "approved")
			complete["decisionSource"] = "agent"
			srv.Notify(id, methodReviewCompleted, complete)
			reviewReached(t, l, id, "approved")
			noOwnerApproval(t, l, id)
			if len(srv.Answers()) != 0 {
				t.Fatal("the panel sent a decision while the guardian owned it")
			}
			srv.Resolve(request)
			until(t, "the resolved wire request", func() bool {
				l.mu.Lock()
				defer l.mu.Unlock()
				return len(l.pending[id]) == 0
			})
			noOwnerApproval(t, l, id)
		})
	}
}

func TestStrictReviewNeedsARealRequestAndTheOwnersAnswerReachesTheDaemon(t *testing.T) {
	for _, strictFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "strict after request", true: "strict before request"}[strictFirst], func(t *testing.T) {
			srv, l, id := autoReviewed(t)
			srv.Set(id, "active", flagApproval)
			if strictFirst {
				srv.Notify(id, methodStrictReview, map[string]any{"turnId": "turn", "startedAtMs": 1000})
			}
			srv.Notify(id, methodReviewStarted, reviewPayload("turn", "review", "item", "inProgress"))
			reviewReached(t, l, id, "inProgress")
			noOwnerApproval(t, l, id)
			request := srv.Ask(id, methodCommand, map[string]any{"turnId": "turn", "itemId": "item", "startedAtMs": 1000, "command": "touch result"})
			if !strictFirst {
				wireRequestReached(t, l, id)
				noOwnerApproval(t, l, id)
				srv.Notify(id, methodStrictReview, map[string]any{"turnId": "turn", "startedAtMs": 1000})
			}
			until(t, "the request handed to the owner", func() bool { return len(l.Pending(id)) == 1 })
			until(t, "the owner prompt in the state", func() bool {
				st, _, ok := stateOf(t, id)
				return ok && slices.Equal(st.Waiting, []string{"Bash"})
			})
			r := l.Pending(id)[0]
			if err := l.Respond(context.Background(), id, r.Key(), json.RawMessage(`"accept"`)); err != nil {
				t.Fatal(err)
			}
			until(t, "the owner's answer", func() bool { return len(srv.Answers()) == 1 })
			if got := srv.Answers()[0]; got.ID != request || string(got.Result) != `{"decision":"accept"}` {
				t.Fatalf("the owner answered another request: %+v", got)
			}
			noOwnerApproval(t, l, id)
		})
	}
}

func TestAnAutomaticReviewDoesNotHideOrdinaryHumanPrompts(t *testing.T) {
	for _, humanKind := range []string{"user reviewer", "humanCommand", "unmatched request", "question"} {
		t.Run(humanKind, func(t *testing.T) {
			srv, l, id := autoReviewed(t)
			if humanKind == "user reviewer" {
				if _, err := l.Configure(t.Context(), id, Setting{Mode: ModeAsk}); err != nil {
					t.Fatal(err)
				}
			}
			srv.Notify(id, methodReviewStarted, reviewPayload("turn", "review", "item", "inProgress"))
			reviewReached(t, l, id, "inProgress")
			method := methodCommand
			params := map[string]any{"turnId": "turn", "itemId": "item", "command": "touch result"}
			switch humanKind {
			case "humanCommand":
				params["kind"] = "humanCommand"
			case "unmatched request":
				params["itemId"] = "another-item"
			case "question":
				method = methodUserInput
				params["questions"] = []any{map[string]any{"id": "q", "header": "Choice", "question": "Continue?"}}
			}
			srv.Ask(id, method, params)
			until(t, "the human request", func() bool { return len(l.Pending(id)) == 1 })
		})
	}
}
