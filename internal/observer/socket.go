package observer

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
	"time"
)

// SocketPath is where the executor takes the calls of the panel's server
// under a session. It is not in the directory of the executor's own socket:
// that one is mounted into the service's container, and an observer runs a
// command — the service would run any command through it. Only a session
// starts an observer.
func SocketPath() string {
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		return filepath.Join(base, "aacpanel-observers", "sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("aacpanel-observers-%d", os.Getuid()), "sock")
}

// Ops of the socket.
const (
	OpStart = "start"
	OpList  = "list"
	OpStop  = "stop"
	OpTail  = "tail"
)

// Request is one call of the panel's server to the executor.
type Request struct {
	Op     string `json:"op"`
	Caller Caller `json:"caller"`
	Spec   *Spec  `json:"spec,omitempty"`
	Name   string `json:"name,omitempty"`
	Lines  int    `json:"lines,omitempty"`
}

// Reply is what the executor answers.
type Reply struct {
	OK        bool     `json:"ok"`
	Error     string   `json:"error,omitempty"`
	Observer  *View    `json:"observer,omitempty"`
	Observers []View   `json:"observers"`
	Lines     []string `json:"lines"`
}

const (
	// maxRequest bounds a request: a command and the words of a wake.
	maxRequest = 64 << 10
	// callWait bounds one call: a start waits for the unit to be started.
	callWait = 30 * time.Second
	// TailDefault and TailMax are how many lines of a log a tail reads.
	TailDefault = 40
	TailMax     = 400
)

// Listen opens the socket for the owner alone. A socket an executor left
// behind is taken over; one that answers is another executor's.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("the directory of the observers' socket: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("another executor answers on %s", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("the observers' socket %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// Serve answers the calls on ln until ctx ends.
func (k *Keeper) Serve(ctx context.Context, ln net.Listener) {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("observers: the socket stopped: %v", err)
			}
			return
		}
		go k.answer(ctx, conn)
	}
}

func (k *Keeper) answer(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(callWait + 5*time.Second))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Reply{Error: "the request was not parsed: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(ctx, callWait)
	defer cancel()
	_ = json.NewEncoder(conn).Encode(k.do(ctx, req))
}

func (k *Keeper) do(ctx context.Context, req Request) Reply {
	reply := Reply{Observers: []View{}, Lines: []string{}}
	fail := func(err error) Reply {
		reply.Error = err.Error()
		return reply
	}
	switch req.Op {
	case OpStart:
		if req.Spec == nil {
			return fail(errors.New("a start names no observer"))
		}
		v, err := k.Start(ctx, req.Caller, *req.Spec)
		if err != nil {
			return fail(err)
		}
		reply.Observer = &v
	case OpList:
		list, err := k.List(ctx, req.Caller)
		if err != nil {
			return fail(err)
		}
		reply.Observers = list
	case OpStop:
		v, err := k.Stop(ctx, req.Caller, req.Name)
		if err != nil {
			return fail(err)
		}
		reply.Observer = &v
	case OpTail:
		n := req.Lines
		if n <= 0 {
			n = TailDefault
		}
		v, lines, err := k.Tail(ctx, req.Caller, req.Name, min(n, TailMax))
		if err != nil {
			return fail(err)
		}
		reply.Observer, reply.Lines = &v, lines
	default:
		return fail(fmt.Errorf("there is no such operation: %q", req.Op))
	}
	reply.OK = true
	return reply
}

// Ask makes one call on the socket at path.
func Ask(ctx context.Context, path string, req Request) (Reply, error) {
	ctx, cancel := context.WithTimeout(ctx, callWait+10*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return Reply{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Reply{}, err
	}
	var reply Reply
	if err := json.NewDecoder(io.LimitReader(conn, 4<<20)).Decode(&reply); err != nil {
		return Reply{}, fmt.Errorf("the executor's answer was not read: %w", err)
	}
	return reply, nil
}
