package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shared are the cases the delivery's background.py runs too: one truth of
// what is at work, for the script and for the tool.
type shared struct {
	Work []struct {
		What    string          `json:"what"`
		Session string          `json:"session"`
		State   json.RawMessage `json:"state"`
		Work    [3]int          `json:"work"`
	} `json:"work"`
	Words []struct {
		Work  [3]int `json:"work"`
		Words string `json:"words"`
	} `json:"words"`
}

func sharedCases(t *testing.T) shared {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "background.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases shared
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Work) == 0 || len(cases.Words) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	return cases
}

// clock is a time that moves only when the tool sleeps.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time        { return c.now }
func (c *clock) Sleep(d time.Duration) { c.now = c.now.Add(d) }
func hostAt(t *testing.T, c *clock) Host {
	return Host{State: filepath.Join(t.TempDir(), "state.json"), Now: c.Now, Sleep: c.Sleep}
}

func writeState(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheSharedCasesOfWhatIsAtWork(t *testing.T) {
	for _, c := range sharedCases(t).Work {
		t.Run(c.What, func(t *testing.T) {
			h := hostAt(t, &clock{})
			writeState(t, h.State, string(c.State))
			s, err := h.read()
			if err != nil {
				t.Fatal(err)
			}
			if got := workOf(s, c.Session); got != (work{Agents: c.Work[0], Tasks: c.Work[1], Workflows: c.Work[2]}) {
				t.Errorf("the work is %+v, meant %v", got, c.Work)
			}
		})
	}
}

func TestTheSharedWordsOfWhatIsAtWork(t *testing.T) {
	for _, c := range sharedCases(t).Words {
		w := work{Agents: c.Work[0], Tasks: c.Work[1], Workflows: c.Work[2]}
		if got := w.words(); got != c.Words {
			t.Errorf("%v is said %q, meant %q", c.Work, got, c.Words)
		}
	}
}

// With nothing at work the answer comes at once, and with no snapshot there is
// nothing at work: without the collector nothing counts it.
func TestNothingAtWorkIsAnsweredAtOnce(t *testing.T) {
	c := &clock{now: time.Unix(1_000, 0)}
	h := hostAt(t, c)
	if got := h.atWork("mine", freshWait); got.any() || !c.now.Equal(time.Unix(1_000, 0)) {
		t.Errorf("no snapshot: %+v after waiting %v", got, c.now.Sub(time.Unix(1_000, 0)))
	}
	writeState(t, h.State, `{"sessionsAt":900,"sessions":[{"sessionId":"mine","work":{"agents":0}}]}`)
	if got := h.atWork("mine", freshWait); got.any() || !c.now.Equal(time.Unix(1_000, 0)) {
		t.Errorf("nothing at work: %+v after waiting %v", got, c.now.Sub(time.Unix(1_000, 0)))
	}
}

// Work the snapshot shows is looked at again in a snapshot written after the
// look began: the turn may have begun with the news that the agent is done,
// before the collector read it.
func TestWorkSeenIsLookedAtAgainInASnapshotWrittenAfterTheLook(t *testing.T) {
	c := &clock{now: time.Unix(1_000, 0)}
	h := hostAt(t, c)
	writeState(t, h.State, `{"sessionsAt":997,"sessions":[{"sessionId":"mine","work":{"agents":1}}]}`)
	sleeps := 0
	h.Sleep = func(d time.Duration) {
		sleeps++
		c.Sleep(d)
		if sleeps == 3 {
			writeState(t, h.State, `{"sessionsAt":1001,"sessions":[{"sessionId":"mine","work":{"agents":0}}]}`)
		}
	}
	if got := h.atWork("mine", freshWait); got.any() {
		t.Errorf("a snapshot older than the look kept an agent that is done at work: %+v", got)
	}
	if sleeps != 3 {
		t.Errorf("the look slept %d times, meant to stop at the first fresh snapshot", sleeps)
	}
}

// Work that goes on is work after the wait, and the wait keeps to its limit.
func TestWorkThatGoesOnIsWorkAfterTheWait(t *testing.T) {
	c := &clock{now: time.Unix(1_000, 0)}
	h := hostAt(t, c)
	writeState(t, h.State, `{"sessionsAt":997,"sessions":[{"sessionId":"mine","work":{"agents":1,"workflows":2}}]}`)
	if got := h.atWork("mine", freshWait); got != (work{Agents: 1, Workflows: 2}) {
		t.Errorf("the work is %+v", got)
	}
	if waited := c.now.Sub(time.Unix(1_000, 0)); waited < freshWait || waited > freshWait+freshStep {
		t.Errorf("the look waited %v, its limit is %v", waited, freshWait)
	}
}
