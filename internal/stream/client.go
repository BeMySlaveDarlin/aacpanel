package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Ask puts one request to the holder of a conversation.
func Ask(ctx context.Context, sessionID string, req Request) (Reply, error) {
	if !uuidLike(sessionID) {
		return Reply{}, fmt.Errorf("the session id %q is not a uuid", sessionID)
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", SocketPath(sessionID))
	if err != nil {
		return Reply{}, fmt.Errorf("the session is not held on the stream: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(controlWait + 10*time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Reply{}, err
	}
	var reply Reply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return Reply{}, fmt.Errorf("the holder did not answer: %w", err)
	}
	return reply, nil
}

// Held reads the state file of a conversation and says whether a live holder
// keeps it with claude at pid. A `claude -p` with no such file is someone
// else's run — an SDK reviewer, a one-shot question — and not a session of the
// panel, whatever its flags.
func Held(sessionID string, pid int) (Summary, bool) {
	if !uuidLike(sessionID) {
		return Summary{}, false
	}
	raw, err := os.ReadFile(StatePath(sessionID))
	if err != nil {
		return Summary{}, false
	}
	var sum Summary
	if json.Unmarshal(raw, &sum) != nil || sum.SessionID != sessionID {
		return Summary{}, false
	}
	if pid != 0 && sum.PID != pid {
		return Summary{}, false
	}
	if sum.Holder <= 0 || !alive(sum.Holder) {
		return Summary{}, false
	}
	return sum, true
}

func alive(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}
