package executor

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	seenSocketName   = "seen.sock"
	seenDirEnv       = "AACP_SEEN_DIR"
	seenDirDefault   = "/run/aacpanel-agent"
	maxSeenReply     = 4 << 10
	transcriptLookup = 450 * time.Millisecond
)

// What else the collector is asked about a conversation, instead of whether a
// prompt landed: whether the session's own turn is over, and the permission
// mode it was last in.
const (
	seenAskTurn = "turn"
	seenAskMode = "mode"
)

type seenReq struct {
	Session string `json:"session"`
	Pos     *int64 `json:"pos,omitempty"`
	Mark    string `json:"mark,omitempty"`
	Ask     string `json:"ask,omitempty"`
	// Since is when the console started, in epoch milliseconds.
	Since int64 `json:"since,omitempty"`
}

type seenReply struct {
	OK    bool   `json:"ok"`
	Found bool   `json:"found"`
	Pos   int64  `json:"pos"`
	Seen  bool   `json:"seen"`
	Ended bool   `json:"ended"`
	Mode  string `json:"mode,omitempty"`
}

type transcriptTail struct {
	id     string
	sock   string
	found  bool
	pos    int64
	looked time.Time
}

func watchTranscript(s liveSession) *transcriptTail {
	if s.SessionID == "" {
		return nil
	}
	w := &transcriptTail{id: s.SessionID, sock: seenSocket(), pos: -1}
	w.ask("")
	return w
}

func seenSocket() string {
	dir := os.Getenv(seenDirEnv)
	if dir == "" {
		dir = seenDirDefault
	}
	return filepath.Join(dir, seenSocketName)
}

func (w *transcriptTail) watching() bool {
	return w != nil && w.found
}

func (w *transcriptTail) saw(mark string) bool {
	if w == nil {
		return false
	}
	if !w.found && time.Since(w.looked) < transcriptLookup {
		return false
	}
	return w.ask(mark)
}

func (w *transcriptTail) ask(mark string) bool {
	w.looked = time.Now()

	req := seenReq{Session: w.id}
	if w.pos >= 0 {
		pos := w.pos
		req.Pos = &pos
		req.Mark = mark
	}
	reply, err := askSeen(w.sock, req)
	if err != nil || !reply.OK {
		w.found = false
		return false
	}

	w.found = reply.Found
	if !reply.Found {
		w.pos = 0
		return false
	}
	w.pos = reply.Pos
	return reply.Seen
}

func askSeen(socket string, req seenReq) (seenReply, error) {
	var reply seenReply

	conn, err := net.DialTimeout("unix", socket, seenTimeout)
	if err != nil {
		return reply, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(seenTimeout)); err != nil {
		return reply, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return reply, err
	}
	if _, err := conn.Write(body); err != nil {
		return reply, err
	}
	if half, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = half.CloseWrite()
	}

	dec := json.NewDecoder(io.LimitReader(conn, maxSeenReply))
	if err := dec.Decode(&reply); err != nil {
		return seenReply{}, err
	}
	return reply, nil
}
