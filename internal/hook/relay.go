package hook

import (
	"errors"
	"io"
	"net"
	"os"
	"time"
)

// The question relay: a session's question, the call of AskUserQuestion, goes
// to the panel before claude puts it on the screen. The collector keeps it in
// its book and the panel offers the person the dialog to answer on the phone.
// The hook hands the collector the payload of the call over ask.sock, in the
// collector's runtime directory, and waits for its word that the question is
// in the book.
//
// The relay never holds the question: it says nothing to claude whatever
// befalls it, and its exit is 0, as every hook of the executor's. A collector
// that is not there, or is down, or does not answer leaves the question on the
// screen as it was, only not on the phone. Each step of the exchange — the
// connection, the message, the answer — is given relayStep, as the
// delivery's ask-hook.py gives its socket, so the hook ends well inside its
// kit timeout. codex runs the same hook from its hooks.json.
//
// The two send the same bytes on the same payload; they share their cases in
// deploy/claude/testdata/ask-hook.json.

const relayTool = "AskUserQuestion"

// relayStep is how long each step of the exchange with the collector may take.
var relayStep = 1250 * time.Millisecond

// relayWait is the longest the relay takes: the connection, the message and
// the answer, a step each.
const relayWait = 3750 * time.Millisecond

// askSocket is the collector's socket for questions.
func askSocket() string {
	if path := os.Getenv("AACP_ASK_SOCKET"); path != "" {
		return path
	}
	return "/run/aacpanel-agent/ask.sock"
}

func relayQuestion(_ string, payload []byte, _ time.Time) Reply {
	v, ok := pyLoad(payload)
	if !ok {
		return Reply{}
	}
	call, ok := v.(*pyObject)
	if !ok || call.get("tool_name") != relayTool {
		return Reply{}
	}
	_ = handOver(askSocket(), pyDumps(call), relayStep, relayWait, false)
	return Reply{}
}

// handOver hands a message to a socket of the collector and waits for its
// answer: the first read of it, or all of it up to the end. Each step — the
// connection, the message, every read of the answer — is given step, as a
// python socket's timeout gives it, and the whole of it no more than whole:
// past its timeout claude would kill the hook mid-way. What the collector
// answers is no business of the hook.
func handOver(path string, msg []byte, step, whole time.Duration, all bool) error {
	end := time.Now().Add(whole)
	by := func() time.Time {
		at := time.Now().Add(step)
		if at.After(end) {
			return end
		}
		return at
	}
	conn, err := net.DialTimeout("unix", path, time.Until(by()))
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(by()); err != nil {
		return err
	}
	if _, err := conn.Write(msg); err != nil {
		return err
	}
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		return err
	}
	buf := make([]byte, 64<<10)
	for {
		if err := conn.SetReadDeadline(by()); err != nil {
			return err
		}
		if _, err := conn.Read(buf); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !all {
			return nil
		}
	}
}
