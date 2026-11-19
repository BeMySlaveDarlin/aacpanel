package act

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClaimInputAndOwnership(t *testing.T) {
	minute := 1
	invalid := []ClaimInput{
		{Expects: "reply", Re: strings.Repeat("a", 32)}, {Expects: "other"}, {Due: &minute},
		{Subject: "topic"}, {Re: "bad"}, {Outcome: "done"}, {Re: strings.Repeat("a", 32), Outcome: "other"},
		{Expects: "done", Subject: strings.Repeat("é", 121)},
	}
	for _, n := range []int{0, 1441} {
		value := n
		invalid = append(invalid, ClaimInput{Expects: "reply", Due: &value})
	}
	for _, input := range invalid {
		if err := input.Validate(); err == nil {
			t.Errorf("accepted invalid claim: %+v", input)
		}
	}
	row := Record{ID: strings.Repeat("a", 32), Kind: "claim", State: "open", Session: "sender", Conversation: "send-conv", Peer: "peer", PeerConversation: "peer-conv", Body: Body{Expects: "done"}}
	for _, c := range []struct {
		conversation, session, to, outcome, target string
		ok                                         bool
	}{
		{"peer-conv", "renamed", "", "done", "sender", true},
		{"new-peer-conv", "peer", "sender", "reply", "sender", true},
		{"foreign", "other", "", "done", "", false},
		{"peer-conv", "peer", "other", "done", "", false},
		{"peer-conv", "peer", "", "withdraw", "", false},
		{"new-send-conv", "sender", "", "withdraw", "peer", true},
	} {
		got, err := row.ClaimTarget(c.conversation, c.session, c.to, c.outcome)
		if (err == nil) != c.ok || got != c.target {
			t.Errorf("%+v: target=%q error=%v", c, got, err)
		}
	}
	row.State = "answered"
	if _, err := row.ClaimTarget("peer-conv", "peer", "", "done"); err == nil || !strings.Contains(err.Error(), "answered") {
		t.Fatalf("closed claim: %v", err)
	}
}

func TestClaimOutcomesAndLetterProtocol(t *testing.T) {
	row := Record{ID: strings.Repeat("b", 32), Kind: "claim", State: "open", Session: "sender", Body: Body{Expects: "done"}}
	for _, c := range []struct{ expect, outcome, state string }{
		{"reply", "reply", "answered"}, {"reply", "done", "answered"}, {"done", "reply", "open"},
		{"done", "done", "answered"}, {"done", "cant", "declined"}, {"done", "withdraw", "withdrawn"},
	} {
		row.Body.Expects = c.expect
		if state, err := row.ClaimState(c.outcome); err != nil || state != c.state {
			t.Errorf("%+v: %s %v", c, state, err)
		}
	}
	row.Body.Expects = "done"
	want := "Claim " + row.ID + " from sender: expects the work done, no due. Answer with send_to_session re=" + row.ID + " (outcome done when the work is done, cant if you cannot do it).\n\nKeep\nthis text."
	now := time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)
	if got := row.ClaimLetter("Keep\nthis text.", now); got != want {
		t.Fatalf("letter: %q", got)
	}
	if got := row.Reply("done", "Ready."); got != "Re claim "+row.ID+" (done):\n\nReady." {
		t.Fatalf("reply: %q", got)
	}
	due := now.Add(61 * time.Second)
	row.Due = &due
	if got := row.ClaimLetter("Do it.", now); !strings.Contains(got, "due in 2 min.") {
		t.Fatalf("due: %s", got)
	}
	if got := row.ClaimReceipt("accepted", ""); !strings.HasPrefix(got, "Sent as claim "+row.ID+": accepted.") || !strings.Contains(got, "if none comes by "+due.Local().Format("15:04")) {
		t.Fatalf("receipt: %s", got)
	}
	if got := row.ClaimReceipt("accepted", "reply"); !strings.Contains(got, "stays open") {
		t.Fatalf("intermediate receipt: %s", got)
	}
	row.State = "answered"
	if got := row.ClaimReceipt("accepted", "done"); !strings.HasSuffix(got, "is done.") {
		t.Fatalf("done receipt: %s", got)
	}
	body, _ := json.Marshal(Body{Expects: "reply"})
	if string(body) != `{"expects":"reply","due":null}` {
		t.Fatalf("claim body: %s", body)
	}
	if row.ClaimMessageID(2, "parent") == row.ClaimMessageID(2, "sender") || row.ClaimMessageID(1, "peer") == row.ClaimMessageID(2, "peer") {
		t.Fatal("rung messages share their identity")
	}
	if got := (ClaimInput{}).Title(strings.Repeat("é", 130) + "\nsecond"); got != strings.Repeat("é", 120) {
		t.Fatalf("subject: %q", got)
	}
}

func TestClaimMinutesRoundUpAndKeepAtLeastOne(t *testing.T) {
	for _, c := range []struct {
		span time.Duration
		want int
	}{
		{-time.Minute, 1}, {0, 1}, {time.Second, 1}, {time.Minute, 1}, {time.Minute + time.Nanosecond, 2}, {2 * time.Minute, 2},
	} {
		if got := ClaimMinutes(c.span); got != c.want {
			t.Errorf("%s: %d, want %d", c.span, got, c.want)
		}
	}
}

func TestClaimElapsedRoundsDownAndKeepsAtLeastOne(t *testing.T) {
	for _, c := range []struct {
		span time.Duration
		want int
	}{
		{0, 1}, {30 * time.Second, 1}, {5*time.Minute + time.Second, 5}, {5*time.Minute + 59*time.Second, 5}, {6 * time.Minute, 6},
	} {
		if got := ClaimElapsed(c.span); got != c.want {
			t.Errorf("%s: %d, want %d", c.span, got, c.want)
		}
	}
}
