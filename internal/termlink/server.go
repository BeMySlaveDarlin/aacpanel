package termlink

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
)

// Terminal is an open terminal as the protocol sees it.
type Terminal interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	Kind() string
	Detail() string
}

// Opener opens a terminal to the named session.
type Opener interface {
	Open(ctx context.Context, target string, cols, rows uint16) (Terminal, error)
}

// Server accepts terminal streams.
type Server struct {
	path   string
	opener Opener

	live atomic.Int32
}

const (
	socketMode    = 0o600
	dirMode       = 0o700
	maxSocketPath = 100
)

// NewServer prepares a listener.
func NewServer(path string, opener Opener) *Server {
	return &Server{path: path, opener: opener}
}

// Listen opens the socket with owner-only permissions.
func (s *Server) Listen() (net.Listener, error) {
	if len(s.path) > maxSocketPath {
		return nil, fmt.Errorf("the socket path is longer than %d bytes, the kernel will not take it: %s", maxSocketPath, s.path)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), dirMode); err != nil {
		return nil, fmt.Errorf("socket directory: %w", err)
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the stale socket cannot be removed: %w", err)
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return nil, fmt.Errorf("terminal socket: %w", err)
	}
	if err := os.Chmod(s.path, socketMode); err != nil {
		ln.Close()
		return nil, fmt.Errorf("socket permissions: %w", err)
	}
	return ln, nil
}

// Serve handles connections while the context is alive.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			return fmt.Errorf("accepting a connection: %w", err)
		}
		wg.Go(func() { s.handle(ctx, conn) })
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	out := json.NewEncoder(conn)
	var mu sync.Mutex
	send := func(f Frame) error {
		mu.Lock()
		defer mu.Unlock()
		return out.Encode(f)
	}

	in := bufio.NewReaderSize(conn, MaxFrame)
	first, err := readFrame(in)
	if err != nil {
		send(Frame{Type: FrameEnd, Error: fmt.Sprintf("the first frame did not parse: %v", err)})
		return
	}
	if err := ValidateOpen(first); err != nil {
		send(Frame{Type: FrameEnd, Error: err.Error()})
		return
	}

	if n := s.live.Add(1); int(n) > MaxTerminals {
		s.live.Add(-1)
		send(Frame{Type: FrameEnd, Error: fmt.Sprintf("%d terminals are already open, no more are held", MaxTerminals)})
		return
	}
	defer s.live.Add(-1)

	term, err := s.opener.Open(ctx, first.Target, first.Cols, first.Rows)
	if err != nil {
		send(Frame{Type: FrameEnd, Error: err.Error()})
		return
	}
	defer term.Close()

	if err := send(Frame{Type: FrameReady, Kind: term.Kind(), Detail: term.Detail()}); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.pumpOut(term, send)
	}()

	s.pumpIn(in, term, send)
	term.Close()
	<-done
}

func (s *Server) pumpOut(term Terminal, send func(Frame) error) {
	buf := make([]byte, MaxChunk)
	for {
		n, err := term.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := send(Frame{Type: FrameOut, Data: chunk}); sendErr != nil {
				return
			}
		}
		if err != nil {
			msg := ""
			if !detached(err) {
				msg = err.Error()
			}
			send(Frame{Type: FrameEnd, Error: msg})
			return
		}
	}
}

func detached(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO)
}

func (s *Server) pumpIn(in *bufio.Reader, term Terminal, send func(Frame) error) {
	for {
		f, err := readFrame(in)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("terminal: the input did not parse: %v", err)
			}
			return
		}
		switch f.Type {
		case FrameIn:
			if _, err := term.Write(f.Data); err != nil {
				send(Frame{Type: FrameEnd, Error: fmt.Sprintf("the input was not accepted: %v", err)})
				return
			}
		case FrameSize:
			if err := ValidateSize(f.Cols, f.Rows); err != nil {
				log.Printf("terminal: the size was not accepted: %v", err)
				continue
			}
			if err := term.Resize(f.Cols, f.Rows); err != nil {
				log.Printf("terminal: %v", err)
			}
		case FrameOpen:
			send(Frame{Type: FrameEnd, Error: "the terminal is already open, a second one in the same stream is not allowed"})
			return
		default:
			log.Printf("terminal: unknown frame %q", f.Type)
		}
	}
}

func readFrame(in *bufio.Reader) (Frame, error) {
	line, err := in.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return Frame{}, fmt.Errorf("the frame is longer than %d bytes", MaxFrame)
	}
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Frame{}, fmt.Errorf("the frame did not parse: %w", err)
	}
	return f, nil
}
