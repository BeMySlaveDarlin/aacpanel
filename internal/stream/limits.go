package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The subscription limits of a contour are known to claude alone: a terminal
// hands them to its status line, which writes the contour's snapshot; claude
// -p runs no status line. So the executor asks a live session of each contour
// on the stream with get_usage and writes the same snapshot — any holder
// passes the request on, whatever version it runs.

// LimitsEvery is how often a contour's snapshot is renewed at most: a
// terminal of the contour may have just written it.
const LimitsEvery = 45 * time.Second

// limitsFile is the snapshot of a contour, in the contour's own directory.
const limitsFile = "rate-limits.json"

// RenewLimits renews the snapshot of the subscription limits of every contour
// that has a session on the stream, asking one of its sessions. It returns
// what went wrong, one line a contour.
func RenewLimits(ctx context.Context, now time.Time) []string {
	byDir := map[string]string{}
	var order []string
	for _, sid := range heldSessions() {
		sum, ok := Held(sid, 0)
		if !ok || sum.PID <= 0 {
			continue
		}
		dir := configDirOf(sum.PID)
		if dir == "" {
			continue
		}
		if _, seen := byDir[dir]; !seen {
			byDir[dir] = sid
			order = append(order, dir)
		}
	}
	var failed []string
	for _, dir := range order {
		path := filepath.Join(dir, limitsFile)
		if st, err := os.Stat(path); err == nil && now.Sub(st.ModTime()) < LimitsEvery {
			continue
		}
		reply, err := Ask(ctx, byDir[dir], Request{Op: OpControl, Subtype: "get_usage"})
		if err == nil && !reply.OK {
			err = fmt.Errorf("%s", reply.Error)
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		snap, ok := limitsOf(reply.Response, now)
		if !ok {
			continue
		}
		if err := writeAtomic(path, snap); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", dir, err))
		}
	}
	return failed
}

// heldSessions lists the conversations the directory of the stream holds a
// state file for.
func heldSessions() []string {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if id, ok := strings.CutSuffix(name, ".json"); ok && uuidLike(id) {
			out = append(out, id)
		}
	}
	return out
}

// configDirOf is the directory of the contour a claude runs in. The wrapper
// of the contours sets it for claude, not for the holder, so it is read from
// claude's own environment; a claude without one runs in the default.
func configDirOf(pid int) string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return ""
	}
	home := ""
	for kv := range bytes.SplitSeq(raw, []byte{0}) {
		if v, ok := bytes.CutPrefix(kv, []byte("CLAUDE_CONFIG_DIR=")); ok && len(v) > 0 {
			return string(v)
		}
		if v, ok := bytes.CutPrefix(kv, []byte("HOME=")); ok {
			home = string(v)
		}
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func writeAtomic(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

type usageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// limitsOf reads claude's answer to get_usage into the snapshot the status
// line writes: the share of each window spent and when it resets, in seconds.
// An account without limits — an API key — has nothing to write.
func limitsOf(resp json.RawMessage, now time.Time) ([]byte, bool) {
	var body struct {
		Response struct {
			Available bool `json:"rate_limits_available"`
			Limits    struct {
				Five  *usageWindow `json:"five_hour"`
				Seven *usageWindow `json:"seven_day"`
			} `json:"rate_limits"`
		} `json:"response"`
	}
	if json.Unmarshal(resp, &body) != nil {
		return nil, false
	}
	five, seven := body.Response.Limits.Five, body.Response.Limits.Seven
	if !body.Response.Available || five == nil || five.Utilization == nil {
		return nil, false
	}
	part := func(w *usageWindow) map[string]any {
		out := map[string]any{"pct": nil, "resetsAt": nil}
		if w == nil {
			return out
		}
		if w.Utilization != nil {
			out["pct"] = *w.Utilization
		}
		if at, err := time.Parse(time.RFC3339Nano, w.ResetsAt); err == nil {
			out["resetsAt"] = at.Unix()
		}
		return out
	}
	snap, err := json.Marshal(map[string]any{"at": now.Unix(), "fiveHour": part(five), "sevenDay": part(seven)})
	if err != nil {
		return nil, false
	}
	return append(snap, '\n'), true
}
