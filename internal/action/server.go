package action

import (
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
	"time"
)

const (
	maxRequest    = FilesBytesMax/3*4 + 64<<10
	readTimeout   = 5 * time.Second
	writeTimeout  = 5 * time.Second
	resultTTL     = 10 * time.Minute
	socketMode    = 0o600
	dirMode       = 0o700
	maxSocketPath = 100
)

// Executor performs one permitted action.
type Executor interface {
	Execute(ctx context.Context, req Request) (detail string, err error)
}

// Asker is an executor that can answer questions about a session.
type Asker interface {
	Permission(ctx context.Context, target string) (*Permission, error)
}

// WindowAsker is an executor that can tell whether a session has a window open.
type WindowAsker interface {
	Window(ctx context.Context, target string) (*Window, error)
}

// Capable is an executor that does not do everything listed in Kinds.
type Capable interface {
	Kinds() []Kind
}

// ExecutorFunc adapts a plain function into an Executor.
type ExecutorFunc func(ctx context.Context, req Request) (string, error)

func (f ExecutorFunc) Execute(ctx context.Context, req Request) (string, error) { return f(ctx, req) }

// Server accepts requests from the service.
type Server struct {
	path    string
	exec    Executor
	timeout time.Duration

	mu      sync.Mutex
	results map[string]*result
}

func (s *Server) kinds() []Kind {
	if able, ok := s.exec.(Capable); ok {
		return able.Kinds()
	}
	return Kinds
}

type result struct {
	done chan struct{}
	resp Response
	at   time.Time
}

// NewServer prepares an executor with a ceiling on one action.
func NewServer(path string, exec Executor, timeout time.Duration) *Server {
	if timeout <= 0 {
		timeout = time.Minute
	}
	return &Server{path: path, exec: exec, timeout: timeout, results: map[string]*result{}}
}

// Listen opens the socket with owner-only permissions.
func (s *Server) Listen() (net.Listener, error) {
	if len(s.path) > maxSocketPath {
		return nil, fmt.Errorf("the socket path is %d characters, the kernel takes no more than %d: %s",
			len(s.path), maxSocketPath, s.path)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("socket directory: %w", err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return nil, fmt.Errorf("socket directory permissions: %w", err)
	}
	if info, err := os.Stat(s.path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s is taken by something that is not a socket", s.path)
		}
		if err := os.Remove(s.path); err != nil {
			return nil, fmt.Errorf("stale socket: %w", err)
		}
	}

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return nil, fmt.Errorf("socket %s: %w", s.path, err)
	}
	if err := os.Chmod(s.path, socketMode); err != nil {
		ln.Close()
		return nil, fmt.Errorf("socket permissions: %w", err)
	}
	return ln, nil
}

// Run serves the socket until the context is cancelled.
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accepting a connection: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.serve(ctx, conn)
		}()
	}
}

func (s *Server) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(readTimeout))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return
		}
		s.reply(conn, Response{OK: false, Error: "the request was not parsed: " + err.Error()})
		return
	}

	resp := s.handle(ctx, req)
	s.reply(conn, resp)
}

func (s *Server) handle(ctx context.Context, req Request) Response {
	if err := req.Validate(); err != nil {
		log.Printf("aacpanel-exec: rejected: %v", err)
		return Failed(req.ID, err, 0)
	}

	if req.Ask == AskKinds {
		return Response{ID: req.ID, OK: true, Kinds: s.kinds()}
	}

	if req.Ask == AskPermission {
		asker, ok := s.exec.(Asker)
		if !ok {
			return Failed(req.ID, errors.New("this executor cannot read session dialogs"), 0)
		}
		perm, err := asker.Permission(ctx, req.Target)
		if err != nil {
			return Failed(req.ID, err, 0)
		}
		return Response{ID: req.ID, OK: true, Permission: perm}
	}

	if req.Ask == AskWindow {
		asker, ok := s.exec.(WindowAsker)
		if !ok {
			return Failed(req.ID, errors.New("this executor does not know about session windows"), 0)
		}
		win, err := asker.Window(ctx, req.Target)
		if err != nil {
			return Failed(req.ID, err, 0)
		}
		return Response{ID: req.ID, OK: true, Window: win}
	}

	s.mu.Lock()
	s.sweepLocked()
	if prev, ok := s.results[req.ID]; ok {
		s.mu.Unlock()
		select {
		case <-prev.done:
			log.Printf("aacpanel-exec: repeat of %s, returning the earlier result", req.ID)
			return prev.resp
		case <-ctx.Done():
			return Failed(req.ID, errors.New("the executor is shutting down"), 0)
		}
	}
	cur := &result{done: make(chan struct{}), at: time.Now()}
	s.results[req.ID] = cur
	s.mu.Unlock()

	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	detail, err := s.exec.Execute(runCtx, req)
	cancel()
	took := time.Since(started)

	if err != nil {
		cur.resp = Failed(req.ID, err, took)
		log.Printf("aacpanel-exec: %s %s (%s): %v", req.Kind, req.Target, took.Round(time.Millisecond), err)
	} else {
		cur.resp = Done(req.ID, detail, took)
		log.Printf("aacpanel-exec: %s %s done in %s", req.Kind, req.Target, took.Round(time.Millisecond))
	}
	close(cur.done)
	return cur.resp
}

func (s *Server) reply(conn net.Conn, resp Response) {
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		log.Printf("aacpanel-exec: the reply was not sent: %v", err)
	}
}

func (s *Server) sweepLocked() {
	for id, r := range s.results {
		select {
		case <-r.done:
			if time.Since(r.at) > resultTTL {
				delete(s.results, id)
			}
		default:
		}
	}
}
