package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// ReviewShelf writes a reading to the shelf the host agent keeps.
//
// A socket of its own rather than the chat one: a reading is up to two hundred
// notes with the lines they stand on, and the chat socket answers a request
// that fits in one read. What comes back is where the file landed — the path
// the session is handed and the panel writes down.
type ReviewShelf struct {
	Socket string
}

// ReviewPut is a reading on its way to the shelf. The shelf names the file
// from the id and refuses anything that is not a plain name: what travels here
// is a reading, never a path.
type ReviewPut struct {
	ID      string       `json:"id"`
	Session string       `json:"session"`
	Cwd     string       `json:"cwd,omitempty"`
	Base    string       `json:"base,omitempty"`
	At      string       `json:"at,omitempty"`
	Notes   []ReviewNote `json:"notes"`
}

// ReviewNote is one remark on one line, as the shelf keeps it.
type ReviewNote struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Quote string `json:"quote"`
	Text  string `json:"text"`
	At    string `json:"at,omitempty"`
}

// ErrNoShelf is the answer when the shelf socket is not there at all — the
// agent is not running, or its state directory is not mounted.
var ErrNoShelf = errors.New("the shelf of readings is unreachable: the agent socket is not there")

// Available says whether there is a shelf to write to.
func (s *ReviewShelf) Available() bool { return s != nil && s.Socket != "" }

// Put writes the reading and answers with the path of the file. The write half
// is closed before reading: the agent takes the request to the end of the
// stream, which is what lets a reading be larger than one read.
func (s *ReviewShelf) Put(ctx context.Context, put ReviewPut) (string, error) {
	if !s.Available() {
		return "", ErrNoShelf
	}
	if put.Notes == nil {
		put.Notes = []ReviewNote{}
	}
	body, err := json.Marshal(put)
	if err != nil {
		return "", err
	}

	var dial net.Dialer
	conn, err := dial.DialContext(ctx, "unix", s.Socket)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoShelf, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	if _, err := conn.Write(body); err != nil {
		return "", fmt.Errorf("the reading was not handed over: %w", err)
	}
	if half, ok := conn.(*net.UnixConn); ok {
		if err := half.CloseWrite(); err != nil {
			return "", fmt.Errorf("the reading was not handed over: %w", err)
		}
	}

	var reply struct {
		OK    bool   `json:"ok"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return "", fmt.Errorf("the shelf gave no answer: %w", err)
	}
	if !reply.OK {
		if reply.Error == "" {
			reply.Error = "the shelf refused the reading and said nothing"
		}
		return "", errors.New(reply.Error)
	}
	if reply.Path == "" {
		return "", errors.New("the shelf took the reading and did not say where it landed")
	}
	return reply.Path, nil
}
