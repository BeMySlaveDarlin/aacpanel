package check

import (
	"strings"
	"testing"
)

type turnCallsShot struct {
	Calls     []string `json:"calls"`
	Head      []string `json:"head"`
	EmptyHead string   `json:"emptyHead"`
	EmptyHint string   `json:"emptyHint"`
	EmptyLeft int      `json:"emptyLeft"`
}

// The badge of a turn opens the calls of the whole turn — every run between
// the end of the turn before and this one — with how long the turn took and
// how many agents it left at work.
func TestTheBadgeOfATurnOpensItsCalls(t *testing.T) {
	var got turnCallsShot
	runFixture(t, "turncalls.html", &got)

	all := strings.Join(got.Calls, " | ")
	if len(got.Calls) != 3 || !strings.Contains(all, "thinking") ||
		!strings.Contains(all, "make check") || !strings.Contains(all, "main.go") {
		t.Errorf("the turn lists %v: the thinking and the calls of both its runs", got.Calls)
	}
	if strings.Contains(all, "the turn before") {
		t.Errorf("the turn lists a call of the turn before it: %v", got.Calls)
	}
	if len(got.Head) != 2 || !strings.HasPrefix(got.Head[0], "2 calls · worked 4m 59s · ") {
		t.Fatalf("the head of the sheet reads %q: the calls, how long the turn took and when it ended", got.Head)
	}
	if got.Head[1] != "2 background agents were still at work" {
		t.Errorf("the head does not say what the turn left at work: %q", got.Head[1])
	}
	if !strings.HasPrefix(got.EmptyHead, "0 calls · worked 2s") || got.EmptyLeft != 1 {
		t.Errorf("a turn without calls reads %q with %d lines", got.EmptyHead, got.EmptyLeft)
	}
	if !strings.Contains(got.EmptyHint, "no calls") {
		t.Errorf("a turn without calls opens an empty sheet with no word: %q", got.EmptyHint)
	}
}
