package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"aacpanel/internal/guards"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/session"
)

// The context guard: at its cap a session of a project that restarts its
// sessions is told, at the end of a turn, to wrap up and restart itself. At
// the cap or a little under it, by guards.RestartMargin: a session ending a
// turn there would not stop again until someone wrote to it.
//
// Not while its agents, workflows or the commands it sent to the background
// are at work: a restart would end them. The news that one of them is done
// starts a turn of its own, and the end of that turn asks again. That turn
// can end before the collector's snapshot has read the news, so work the
// snapshot shows is looked at again in a snapshot written after the look
// began (session.Host.Busy) — a look that takes up to session.LongestLook,
// longer than a snapshot can be old. The kit gives the hook a timeout past
// it: a hook claude kills mid-look says nothing, the restart is not asked
// for, and the session stays past its cap until someone writes to it.
//
// Not while a restart of the session is under way either, and once a
// conversation: the close of a restart lets the session stop once more, and an
// ask there would bring up a second session. The mark that the ask was sent
// lives under the executor's state, a file a conversation in context-guard/,
// the same files the delivery's context-guard.py keeps: a session moved from
// one to the other is not asked twice. The two share their cases in
// deploy/claude/testdata/context-guard.json.

const (
	// restartStale is how old a restart marker is when it was left by a
	// restart that never ended, an executor gone down mid-way, and no longer
	// holds the ask back.
	restartStale = 5 * time.Minute
	// sentKeep is how long a mark that the ask was sent is kept, past the
	// life of a conversation.
	sentKeep = 7 * 24 * time.Hour
)

// busy tells whether anything of the session is at work, from the snapshot
// at the path; a test puts a clock of its own in its place.
var busy = func(state, id string) bool {
	return session.Host{State: state, Now: time.Now, Sleep: time.Sleep}.Busy(id)
}

func guardContext(_ string, payload []byte, now time.Time) Reply {
	var p any
	if json.Unmarshal(payload, &p) != nil {
		return Reply{}
	}
	fields, ok := p.(map[string]any)
	if !ok {
		return Reply{}
	}
	if event, ok := fields["hook_event_name"]; ok && event != "Stop" {
		return Reply{}
	}
	// The turn that follows a block is the finalization itself: it is never
	// blocked again.
	if truthy(fields["stop_hook_active"]) {
		return Reply{}
	}
	g, ok := guards.Of(where(fields))
	if !ok || !g.Restart {
		return Reply{}
	}
	statePath := filepath.Join(hostcfg.Load().StateDir, "state.json")
	id, _ := fields["session_id"].(string)
	state := readState(statePath)
	pct, ok := fill(state, id)
	if !ok || pct < float64(g.Cap)-guards.RestartMargin {
		return Reply{}
	}
	if busy(statePath, id) {
		return Reply{}
	}
	name, _ := sessionOf(state, id)["session"].(string)
	if restarting(name, now) {
		return Reply{}
	}
	mark := filepath.Join(guards.Home(), "context-guard", quote(id))
	if _, err := os.Stat(mark); err == nil {
		return Reply{}
	}
	markSent(mark, now)
	return Reply{Block: guardReason(pct, g.Cap)}
}

// fill is the context fill of the session in percent, false when it is not
// known well: no row, no window, or a window that is not the model's exact
// one.
func fill(state map[string]any, id string) (float64, bool) {
	s := sessionOf(state, id)
	if s["pct"] == nil || !truthy(s["limit"]) {
		return 0, false
	}
	if known, ok := s["limitKnown"]; ok && !truthy(known) {
		return 0, false
	}
	return number(s["pct"])
}

// restarting tells whether a restart of the named session is under way: the
// executor keeps a marker while it restarts a session. A stale one is
// cleared.
func restarting(name string, now time.Time) bool {
	if name == "" {
		return false
	}
	marker := guards.RestartingPath(name)
	info, err := os.Stat(marker)
	if err != nil {
		return false
	}
	if now.Sub(info.ModTime()) < restartStale {
		return true
	}
	_ = os.Remove(marker)
	return false
}

// markSent marks the ask as sent in the conversation and prunes the marks
// past sentKeep. A mark that cannot be written leaves the ask to repeat, and
// the executor still refuses a second restart while one is under way.
func markSent(mark string, now time.Time) {
	dir := filepath.Dir(mark)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	old := now.Add(-sentKeep)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.ModTime().Before(old) {
			_ = os.Remove(path)
		}
	}
	if f, err := os.OpenFile(mark, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666); err == nil {
		f.Close()
	}
}

// guardReason is what the model is told instead of stopping.
func guardReason(pct float64, limit int) string {
	return "The context of this session is at " + fixed(pct) + "% of the model window and has reached the " +
		strconv.Itoa(limit) + "% this project set as its context cap, where the project restarts its sessions. " +
		"Take no new work. Put the state of the work on disk the way this project keeps it: " +
		"its finalize skill if it has one, otherwise a handoff note for the next session and " +
		"a commit of what is done. Then restart this session with session_restart, the tool of " +
		"the panel's server (mcp__aacpanel__session_restart), without continue: the panel starts " +
		"a fresh session in this place with the project's parameters. Do not go on with this " +
		"conversation. The restart is automatic: do it silently, and do not tell the person " +
		"about the context, the cap or the restart, nor that the next session will continue."
}

// quote is a conversation as one path element, the way the delivery's script
// names its mark: every byte but a letter, a digit and _.-~ as %XX.
func quote(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '_' || c == '.' || c == '-' || c == '~' {
			out = append(out, c)
			continue
		}
		out = fmt.Appendf(out, "%%%02X", c)
	}
	return string(out)
}
