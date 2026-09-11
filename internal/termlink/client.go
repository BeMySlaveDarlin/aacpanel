package termlink

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	dialTimeout = 3 * time.Second
	openTimeout = 10 * time.Second
)

// ErrUnavailable means the executor does not answer.
var ErrUnavailable = errors.New("the executor is unavailable")

// Client is the client of the terminal socket.
type Client struct {
	path string
}

// NewClient prepares a client, with an empty path meaning no terminal here.
func NewClient(path string) *Client { return &Client{path: path} }

// Enabled reports whether a terminal is configured at all.
func (c *Client) Enabled() bool { return c != nil && c.path != "" }

// Available reports whether the executor answers right now.
func (c *Client) Available(ctx context.Context) bool {
	if !c.Enabled() {
		return false
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Stream is an open terminal: output is read, input is written.
type Stream struct {
	conn net.Conn
	in   *bufio.Reader

	kind   string
	detail string

	mu  sync.Mutex
	enc *json.Encoder

	rest []byte
	end  string
}

// Open attaches to a session.
func (c *Client) Open(ctx context.Context, target string, cols, rows uint16) (*Stream, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("%w: the terminal socket is not configured", ErrUnavailable)
	}
	if err := ValidateSize(cols, rows); err != nil {
		return nil, err
	}

	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	s := &Stream{
		conn: conn,
		in:   bufio.NewReaderSize(conn, MaxFrame),
		enc:  json.NewEncoder(conn),
	}

	conn.SetDeadline(time.Now().Add(openTimeout))
	if err := s.frame(Frame{Type: FrameOpen, Target: target, Cols: cols, Rows: rows}); err != nil {
		conn.Close()
		return nil, err
	}
	ready, err := readFrame(s.in)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the executor did not answer the open: %w", err)
	}
	conn.SetDeadline(time.Time{})

	switch ready.Type {
	case FrameReady:
		s.kind, s.detail = ready.Kind, ready.Detail
		return s, nil
	case FrameEnd:
		conn.Close()
		return nil, fmt.Errorf("%s", ready.Error)
	default:
		conn.Close()
		return nil, fmt.Errorf("the open was answered with frame %q", ready.Type)
	}
}

// Kind reports what the terminal turned out to be.
func (s *Stream) Kind() string { return s.kind }

// Detail reports what was attached to.
func (s *Stream) Detail() string { return s.detail }

// End reports why the stream ended, empty when it was closed from this side.
func (s *Stream) End() string { return s.end }

// Read returns bytes of the screen.
func (s *Stream) Read(p []byte) (int, error) {
	for len(s.rest) == 0 {
		f, err := readFrame(s.in)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return 0, io.EOF
			}
			return 0, err
		}
		switch f.Type {
		case FrameOut:
			s.rest = f.Data
		case FrameEnd:
			s.end = f.Error
			return 0, io.EOF
		default:
		}
	}
	n := copy(p, s.rest)
	s.rest = s.rest[n:]
	return n, nil
}

// Write sends input.
func (s *Stream) Write(p []byte) (int, error) {
	if len(p) > MaxChunk {
		return 0, fmt.Errorf("the input is longer than %d bytes", MaxChunk)
	}
	if err := s.frame(Frame{Type: FrameIn, Data: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Resize reports a new window size.
func (s *Stream) Resize(cols, rows uint16) error {
	if err := ValidateSize(cols, rows); err != nil {
		return err
	}
	return s.frame(Frame{Type: FrameSize, Cols: cols, Rows: rows})
}

// Close detaches from the session.
func (s *Stream) Close() error { return s.conn.Close() }

func (s *Stream) frame(f Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enc.Encode(f); err != nil {
		return fmt.Errorf("the frame did not go out: %w", err)
	}
	return nil
}
