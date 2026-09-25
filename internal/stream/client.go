package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
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
	deadline := time.Now().Add(replyWait(req.Subtype) + 10*time.Second)
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

// OneShot says whether a claude process is a run of its own rather than a
// session. A `-p` is a one-off question, an SDK reviewer, a script — unless a
// holder keeps it: then it is a session of the panel on the stream, and the
// conversation id among its arguments says which one.
func OneShot(pid int, args []string) bool {
	print := false
	for _, a := range args {
		if a == "-p" || a == "--print" {
			print = true
			break
		}
	}
	if !print {
		return false
	}
	id := ""
	for i, a := range args {
		switch {
		case (a == "--session-id" || a == "--resume") && i+1 < len(args):
			id = args[i+1]
		case strings.HasPrefix(a, "--session-id="):
			id = strings.TrimPrefix(a, "--session-id=")
		case strings.HasPrefix(a, "--resume="):
			id = strings.TrimPrefix(a, "--resume=")
		}
	}
	if id == "" {
		return true
	}
	_, held := Held(id, pid)
	return !held
}
