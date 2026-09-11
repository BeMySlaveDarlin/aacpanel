package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stand struct {
	socket string
	client *Client
	calls  *atomic.Int32
}

func newStand(t *testing.T, exec Executor) *stand {
	t.Helper()
	calls := &atomic.Int32{}
	counted := ExecutorFunc(func(ctx context.Context, req Request) (string, error) {
		calls.Add(1)
		return exec.Execute(ctx, req)
	})

	socket := filepath.Join(t.TempDir(), "exec.sock")
	srv := NewServer(socket, counted, 5*time.Second)
	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("socket: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Run(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return &stand{socket: socket, client: NewClient(socket, 5*time.Second), calls: calls}
}

func serve(t *testing.T, exec Executor) *Client {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "exec.sock")
	srv := NewServer(socket, exec, 5*time.Second)
	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Run(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return NewClient(socket, 5*time.Second)
}

type limitedExec struct{ kinds []Kind }

func (e limitedExec) Execute(context.Context, Request) (string, error) { return "", nil }
func (e limitedExec) Kinds() []Kind                                    { return e.kinds }

func TestServerAnswersWithWhatTheHostCanDo(t *testing.T) {
	client := serve(t, limitedExec{kinds: []Kind{ContainerStart, ContainerStop}})

	got, err := client.Kinds(context.Background())
	if err != nil {
		t.Fatalf("the list did not arrive: %v", err)
	}
	if len(got) != 2 || got[0] != ContainerStart || got[1] != ContainerStop {
		t.Errorf("the executor named %v, while it can do two actions", got)
	}
}

func TestServerKeepsFullListForPlainExecutor(t *testing.T) {
	client := serve(t, okExecutor("done"))

	got, err := client.Kinds(context.Background())
	if err != nil {
		t.Fatalf("the list did not arrive: %v", err)
	}
	if len(got) != len(Kinds) {
		t.Errorf("a plain executor named %d actions out of %d", len(got), len(Kinds))
	}
}

func okExecutor(detail string) Executor {
	return ExecutorFunc(func(context.Context, Request) (string, error) { return detail, nil })
}

func request(id string, kind Kind, target string) Request {
	return Request{ID: id, Kind: kind, Target: target, Device: "phone"}
}

func TestServerRunsAllowedAction(t *testing.T) {
	s := newStand(t, okExecutor("the container is stopped"))

	resp, err := s.client.Do(context.Background(), request("req-1", ContainerStop, "shop"))
	if err != nil {
		t.Fatalf("the request: %v", err)
	}
	if !resp.OK || resp.Detail != "the container is stopped" {
		t.Fatalf("the answer: %+v", resp)
	}
	if resp.ID != "req-1" {
		t.Fatalf("the answer belongs to another request: %q", resp.ID)
	}
	if resp.DurationMs < 0 {
		t.Fatalf("the duration: %d", resp.DurationMs)
	}
}

func TestServerRejectsUnknownKind(t *testing.T) {
	s := newStand(t, okExecutor(""))

	resp := rawRequest(t, s.socket, `{"id":"req-2","kind":"container.remove","target":"shop"}`)
	if resp.OK {
		t.Fatalf("an unknown action was executed: %+v", resp)
	}
	if resp.Error == "" {
		t.Fatal("a refusal with no explanation")
	}
	if got := s.calls.Load(); got != 0 {
		t.Fatalf("the executor was called %d times for an unknown action", got)
	}
}

func TestServerRejectsShellInTarget(t *testing.T) {
	s := newStand(t, okExecutor(""))

	targets := []string{
		"shop; rm -rf /",
		"shop && docker rm -f shop",
		"shop\nrm -rf /",
		"$(whoami)",
		"`id`",
		"shop|tee /etc/passwd",
		"../../etc/passwd",
		"'shop'",
		"shop shop",
		"shop\u0000",
	}
	for i, target := range targets {
		payload, err := json.Marshal(Request{ID: fmt.Sprintf("shell-%d", i), Kind: ContainerStop, Target: target})
		if err != nil {
			t.Fatalf("building the request: %v", err)
		}
		resp := rawRequest(t, s.socket, string(payload))
		if resp.OK {
			t.Fatalf("the executor accepted target %q", target)
		}
		if resp.Error == "" {
			t.Fatalf("target %q is rejected with no explanation", target)
		}
	}
	if got := s.calls.Load(); got != 0 {
		t.Fatalf("the executor was called %d times on planted targets", got)
	}
}

func TestClientRejectsShellInTargetBeforeSending(t *testing.T) {
	s := newStand(t, okExecutor(""))

	_, err := s.client.Do(context.Background(), request("req-shell", ContainerStop, "shop; rm -rf /"))
	if err == nil {
		t.Fatal("the client sent a target with a command inside")
	}
	var bad ErrBadRequest
	if !errors.As(err, &bad) {
		t.Fatalf("the error is %v, ErrBadRequest expected", err)
	}
	if got := s.calls.Load(); got != 0 {
		t.Fatalf("the executor was called %d times", got)
	}
}

func TestServerIsIdempotent(t *testing.T) {
	s := newStand(t, okExecutor("done"))
	req := request("req-3", ContainerRestart, "shop")

	first, err := s.client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("the first request: %v", err)
	}
	second, err := s.client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("the repeat: %v", err)
	}

	if got := s.calls.Load(); got != 1 {
		t.Fatalf("the action ran %d times, one expected", got)
	}
	if second.Detail != first.Detail || second.OK != first.OK {
		t.Fatalf("the repeat returned a different result: %+v against %+v", second, first)
	}
}

func TestServerCollapsesConcurrentRepeats(t *testing.T) {
	release := make(chan struct{})
	s := newStand(t, ExecutorFunc(func(ctx context.Context, req Request) (string, error) {
		<-release
		return "done", nil
	}))
	req := request("req-4", ContainerStop, "shop")

	var wg sync.WaitGroup
	results := make([]Response, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := s.client.Do(context.Background(), req)
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			results[i] = resp
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := s.calls.Load(); got != 1 {
		t.Fatalf("the action ran %d times on concurrent repeats", got)
	}
	for i, r := range results {
		if !r.OK || r.Detail != "done" {
			t.Fatalf("answer %d: %+v", i, r)
		}
	}
}

func TestServerDistinguishesRequests(t *testing.T) {
	s := newStand(t, okExecutor("done"))

	for _, id := range []string{"req-5", "req-6"} {
		if _, err := s.client.Do(context.Background(), request(id, ContainerStop, "shop")); err != nil {
			t.Fatalf("request %s: %v", id, err)
		}
	}
	if got := s.calls.Load(); got != 2 {
		t.Fatalf("%d actions ran, two expected", got)
	}
}

func TestServerReportsExecutorError(t *testing.T) {
	s := newStand(t, ExecutorFunc(func(context.Context, Request) (string, error) {
		return "", errors.New("the container does not answer SIGTERM")
	}))

	resp, err := s.client.Do(context.Background(), request("req-7", ContainerStop, "shop"))
	if err != nil {
		t.Fatalf("the request: %v", err)
	}
	if resp.OK || resp.Error != "the container does not answer SIGTERM" {
		t.Fatalf("the answer: %+v", resp)
	}
}

func TestServerStopsHangingAction(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "exec.sock")
	srv := NewServer(socket, ExecutorFunc(func(ctx context.Context, req Request) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}), 200*time.Millisecond)

	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx, ln)

	resp, err := NewClient(socket, 5*time.Second).Do(context.Background(), request("req-8", SessionClose, "shop"))
	if err != nil {
		t.Fatalf("the request: %v", err)
	}
	if resp.OK {
		t.Fatal("a hanging action returned success")
	}
}

func TestSocketIsPrivate(t *testing.T) {
	s := newStand(t, okExecutor(""))

	info, err := os.Stat(s.socket)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the socket permissions are %o, 600 expected", mode)
	}
	dir, err := os.Stat(filepath.Dir(s.socket))
	if err != nil {
		t.Fatalf("the directory: %v", err)
	}
	if mode := dir.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("the directory permissions are %o: outsiders can reach it", mode)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "exec.sock")

	first := NewServer(socket, okExecutor(""), time.Second)
	ln, err := first.Listen()
	if err != nil {
		t.Fatalf("the first start: %v", err)
	}
	ln.Close()

	second := NewServer(socket, okExecutor(""), time.Second)
	ln2, err := second.Listen()
	if err != nil {
		t.Fatalf("the second start: %v", err)
	}
	ln2.Close()
}

func TestListenRefusesNonSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("the file: %v", err)
	}

	if _, err := NewServer(path, okExecutor(""), time.Second).Listen(); err == nil {
		t.Fatal("the executor took over the path of a foreign file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the foreign file is gone: %v", err)
	}
}

func TestServerAnswersGarbage(t *testing.T) {
	s := newStand(t, okExecutor(""))

	resp := rawRequest(t, s.socket, "this is not json")
	if resp.OK || resp.Error == "" {
		t.Fatalf("garbage got an ok answer or a refusal with no explanation: %+v", resp)
	}

	if _, err := s.client.Do(context.Background(), request("req-9", ContainerStop, "shop")); err != nil {
		t.Fatalf("after garbage: %v", err)
	}
}

func TestQuietConnectionGetsNoAnswer(t *testing.T) {
	s := newStand(t, okExecutor(""))

	conn, err := net.DialTimeout("unix", s.socket, time.Second)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	if half, ok := conn.(*net.UnixConn); ok {
		if err := half.CloseWrite(); err != nil {
			t.Fatalf("half-closing the connection: %v", err)
		}
	}

	var buf [1]byte
	switch n, err := conn.Read(buf[:]); {
	case err == nil || !errors.Is(err, io.EOF):
		t.Fatalf("an empty connection got an answer: %d bytes read, error %v", n, err)
	}

	half := rawRequest(t, s.socket, `{"kind":`)
	if half.OK || half.Error == "" {
		t.Fatalf("a truncated request got no refusal with an explanation: %+v", half)
	}

	if _, err := s.client.Do(context.Background(), request("req-11", ContainerStop, "shop")); err != nil {
		t.Fatalf("after a quiet connection: %v", err)
	}
}

func TestClientReportsUnavailable(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "missing.sock"), time.Second)

	if client.Available(context.Background()) {
		t.Fatal("a non-existent socket is reported available")
	}
	_, err := client.Do(context.Background(), request("req-10", ContainerStop, "shop"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("the error is %v, ErrUnavailable expected", err)
	}
}

func rawRequest(t *testing.T, socket, payload string) Response {
	t.Helper()
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(payload + "\n")); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if half, ok := conn.(*net.UnixConn); ok {
		half.CloseWrite()
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("the answer: %v", err)
	}
	return resp
}

func TestListenRejectsLongPath(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("a-long-path/", 12), "exec.sock")

	_, err := NewServer(long, okExecutor(""), time.Second).Listen()
	if err == nil {
		t.Fatal("a path that is too long is accepted")
	}
	if !strings.Contains(err.Error(), "characters") {
		t.Fatalf("the error does not explain the reason: %v", err)
	}
}
