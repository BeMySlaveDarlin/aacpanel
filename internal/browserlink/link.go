package browserlink

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Link carries the closed browser protocol over one local socket. Reads have
// one owner; writes from input, resize and image acknowledgements are serialized.
type Link struct {
	conn net.Conn
	scan *bufio.Scanner
	mu   sync.Mutex
}

// New bounds each incoming frame before decoding it.
func New(conn net.Conn) *Link {
	s := bufio.NewScanner(conn)
	s.Buffer(make([]byte, 64<<10), MaxFrame)
	return &Link{conn: conn, scan: s}
}

// Read receives one frame; an oversized or incomplete message ends the link.
func (l *Link) Read() (Frame, error) {
	if !l.scan.Scan() {
		err := l.scan.Err()
		if err == nil {
			err = net.ErrClosed
		}
		return Frame{}, err
	}
	return Decode(l.scan.Bytes())
}

// Write serializes a frame with a deadline so a stalled peer cannot hold shutdown.
func (l *Link) Write(f Frame) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return json.NewEncoder(l.conn).Encode(f)
}

// Close releases the socket and wakes its reader and writer.
func (l *Link) Close() error { return l.conn.Close() }

// ReadDeadline bounds an attachment handshake or the wait for a viewer heartbeat.
func (l *Link) ReadDeadline(t time.Time) { l.conn.SetReadDeadline(t) }

// Dial connects only to the caller-selected Unix socket, never a network address.
func Dial(ctx context.Context, path string) (*Link, error) {
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return New(conn), nil
}

// Listen replaces a stale endpoint and permits only its host owner to connect.
func Listen(path string) (net.Listener, error) {
	if len(path) > 100 {
		return nil, errors.New("the browser socket path is too long")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Attach validates the profile and viewport before negotiating the protocol.
// No input can be sent until the holder acknowledges the attachment.
func Attach(ctx context.Context, path, id string, size Size) (*Link, error) {
	if !ID(id) {
		return nil, errors.New("invalid browser profile")
	}
	if err := size.Check(); err != nil {
		return nil, err
	}
	l, err := Dial(ctx, path)
	if err != nil {
		return nil, err
	}
	l.ReadDeadline(time.Now().Add(15 * time.Second))
	if err = l.Write(Frame{Type: "attach", Version: Protocol, Profile: id, Size: &size}); err != nil {
		l.Close()
		return nil, err
	}
	f, err := l.Read()
	if err != nil {
		l.Close()
		return nil, err
	}
	if f.Type != "ready" || f.Version != Protocol {
		l.Close()
		return nil, fmt.Errorf("the browser did not attach: %s", f.Error)
	}
	l.ReadDeadline(time.Time{})
	return l, nil
}
