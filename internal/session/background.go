package session

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A restart ends the session and everything it runs: its agents, its
// workflows and the commands and watchers it sent to the background. The
// restart waits while any of that is at work; a wake-up the session set itself
// runs nothing and is not waited for. The collector counts the work of every
// session into its snapshot, and the delivery's background.py reads the same
// counts the same way — the two share their cases in
// deploy/claude/testdata/background.json.

// The collector reads the sessions every six seconds and writes them out on
// its next five-second tick, so a snapshot on disk can be eleven seconds old.
// A turn that began with the news that the last agent is done can end before
// the snapshot has read that news: work the snapshot shows is looked at again
// in one read after the look began, or in the last one when none comes in time.
const (
	freshWait = 15 * time.Second
	freshStep = 500 * time.Millisecond
)

// work is what of a session is at work in the background.
type work struct {
	Agents, Tasks, Workflows int
}

func (w work) any() bool { return w.Agents+w.Tasks+w.Workflows > 0 }

// workOf is the work of a session in the snapshot, wake-ups left out.
func workOf(s snapshot, session string) work {
	for _, r := range s.Sessions {
		if r.SessionID != session {
			continue
		}
		var counts map[string]json.RawMessage
		_ = json.Unmarshal(r.Work, &counts)
		tasks := count(counts["tasks"]) - count(counts["wakes"])
		return work{Agents: count(counts["agents"]), Tasks: max(tasks, 0), Workflows: count(counts["workflows"])}
	}
	return work{}
}

// count is a count the collector wrote: a whole number above nought, or none.
func count(raw json.RawMessage) int {
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// atWork is the work of a session still at work. A snapshot that is not there
// shows no work: without the collector nothing counts it.
func (h Host) atWork(session string, wait time.Duration) work {
	since := h.Now()
	s, _ := h.read()
	w := workOf(s, session)
	if !w.any() {
		return w
	}
	begun := float64(since.UnixNano()) / 1e9
	end := since.Add(wait)
	for h.Now().Before(end) {
		h.Sleep(freshStep)
		s, _ = h.read()
		if s.Dated && s.At > begun {
			return workOf(s, session)
		}
	}
	return w
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// words is the work in words: 2 agents and 1 background task.
func (w work) words() string {
	var parts []string
	if w.Agents > 0 {
		parts = append(parts, plural(w.Agents, "agent", "agents"))
	}
	if w.Workflows > 0 {
		parts = append(parts, plural(w.Workflows, "workflow", "workflows"))
	}
	if w.Tasks > 0 {
		parts = append(parts, plural(w.Tasks, "background task", "background tasks"))
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
