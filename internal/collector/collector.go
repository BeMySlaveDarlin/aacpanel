// Package collector holds the panel's tools that hand what a session says to
// the collector on the host: a brief for the person to walk through, and a
// call to their phone. The collector takes both on sockets of its own in its
// runtime directory — the same sockets the scripts of the shipped skills write
// to — and answers every request with whether it took it and, if not, why.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"syscall"
	"time"

	"aacpanel/internal/mcp"
)

// The sockets of the collector. A variable moves one the way it moves it for
// the scripts of the skills, so a tool and a script reach the same collector.
const (
	briefSocket  = "/run/aacpanel-agent/brief.sock"
	notifySocket = "/run/aacpanel-agent/notify.sock"
)

// BriefSocket is where the collector takes briefs.
func BriefSocket() string {
	return socketOf("AACP_BRIEF_SOCKET", briefSocket)
}

// NotifySocket is where the collector takes the calls of the sessions.
func NotifySocket() string {
	return socketOf("AACP_NOTIFY_SOCKET", notifySocket)
}

func socketOf(env, standard string) string {
	if path := os.Getenv(env); path != "" {
		return path
	}
	return standard
}

// reply is what the collector answers on either socket: ok, or the reason it
// did not take the request, with the name a brief was published under.
type reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	ID    string `json:"id"`
}

// maxReply bounds what is read back: the collector answers with one short
// object.
const maxReply = 64 << 10

// ask hands one request to the collector on the socket at path and returns its
// reply: one JSON object a connection, the write half closed, since the
// collector reads a brief to the end of the stream, and the reply read to the
// end. An error is the socket's; a refusal of the collector is a reply.
func ask(ctx context.Context, path string, wait time.Duration, body []byte) (reply, error) {
	dialer := net.Dialer{Timeout: wait}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return reply{}, fmt.Errorf("the panel's collector is not listening on %s", path)
		}
		return reply{}, err
	}
	defer conn.Close()
	deadline := time.Now().Add(wait)
	if at, ok := ctx.Deadline(); ok && at.Before(deadline) {
		deadline = at
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(body); err != nil {
		return reply{}, fmt.Errorf("the collector did not take the request: %w", err)
	}
	if half, ok := conn.(*net.UnixConn); ok {
		if err := half.CloseWrite(); err != nil {
			return reply{}, fmt.Errorf("the collector did not take the request: %w", err)
		}
	}
	raw, err := io.ReadAll(io.LimitReader(conn, maxReply))
	if err != nil {
		return reply{}, fmt.Errorf("the collector did not answer: %w", err)
	}
	var got reply
	if err := json.Unmarshal(raw, &got); err != nil {
		return reply{}, fmt.Errorf("the collector answered with something that is not a reply: %q", clip(string(raw), 200))
	}
	return got, nil
}

// session finds the conversation a call comes from. The collector names a
// session by its conversation — the answers of a brief go back there, and a
// call opens it on the phone — and holds a brief to the directory it works
// in, so both come from the claude the server serves and never from the
// model's arguments. A conversation claude has not written down yet cannot be
// named, and the call says to come again rather than go out unnamed.
func session(bind mcp.Bind) (mcp.Binding, error) {
	b, err := bind()
	if err != nil {
		return b, err
	}
	if b.SessionID == "" || b.Place.Dir == "" {
		return b, errors.New("the panel does not know this conversation yet — claude has not written down " +
			"its session. Try again in a moment")
	}
	return b, nil
}

// refusal is the collector's reason for not taking a request, or what stands
// for it when the collector gave none.
func refusal(got reply, otherwise string) string {
	if got.Error != "" {
		return got.Error
	}
	return otherwise
}

func clip(s string, runes int) string {
	r := []rune(s)
	if len(r) <= runes {
		return s
	}
	return string(r[:runes-1]) + "…"
}
