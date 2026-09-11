package termlink

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeTerm struct {
	out    io.ReadCloser
	mu     sync.Mutex
	in     []byte
	sizes  [][2]uint16
	closed bool
}

func (f *fakeTerm) Read(p []byte) (int, error) { return f.out.Read(p) }

func (f *fakeTerm) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, p...)
	return len(p), nil
}

func (f *fakeTerm) Resize(cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, [2]uint16{cols, rows})
	return nil
}

func (f *fakeTerm) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		f.out.Close()
	}
	return nil
}

func (f *fakeTerm) Kind() string   { return "fake" }
func (f *fakeTerm) Detail() string { return "stand" }

func (f *fakeTerm) written() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.in...)
}

func (f *fakeTerm) resizes() [][2]uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]uint16(nil), f.sizes...)
}

type fakeOpener struct {
	term *fakeTerm
	err  error

	mu      sync.Mutex
	targets []string
	sizes   [][2]uint16
	opened  int
}

func (o *fakeOpener) Open(_ context.Context, target string, cols, rows uint16) (Terminal, error) {
	o.mu.Lock()
	o.targets = append(o.targets, target)
	o.sizes = append(o.sizes, [2]uint16{cols, rows})
	o.opened++
	o.mu.Unlock()
	if o.err != nil {
		return nil, o.err
	}
	return o.term, nil
}

func (o *fakeOpener) asked() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.targets...)
}

func stand(t *testing.T, opener Opener) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "tl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "t.sock")
	srv := NewServer(path, opener)
	ln, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return NewClient(path)
}

func TestTerminalStreamCarriesBytesBothWays(t *testing.T) {
	screen, feed := io.Pipe()
	term := &fakeTerm{out: screen}
	opener := &fakeOpener{term: term}
	client := stand(t, opener)

	stream, err := client.Open(context.Background(), "aacpanel", 120, 40)
	if err != nil {
		t.Fatalf("the terminal did not open: %v", err)
	}
	defer stream.Close()

	if stream.Kind() != "fake" || stream.Detail() != "stand" {
		t.Errorf("the readiness arrived empty: kind=%q detail=%q", stream.Kind(), stream.Detail())
	}
	if got := opener.asked(); len(got) != 1 || got[0] != "aacpanel" {
		t.Errorf("the wrong thing was opened: %v", got)
	}

	go func() {
		feed.Write([]byte("the screen of the session"))
	}()
	buf := make([]byte, 64)
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("the screen does not read: %v", err)
	}
	if got := string(buf[:n]); got != "the screen of the session" {
		t.Errorf("%q arrived from the screen", got)
	}

	if _, err := stream.Write([]byte("hello\r")); err != nil {
		t.Fatalf("the input did not leave: %v", err)
	}
	if err := stream.Resize(80, 24); err != nil {
		t.Fatalf("the resize did not leave: %v", err)
	}

	waitFor(t, func() bool { return string(term.written()) == "hello\r" })
	waitFor(t, func() bool {
		sizes := term.resizes()
		return len(sizes) == 1 && sizes[0] == [2]uint16{80, 24}
	})
}

func TestTerminalStreamKeepsRawBytes(t *testing.T) {
	head := []byte{0x1b, '[', '3', '1', 'm', 0xd1}
	tail := []byte{0x8f, 0xff, 0xfe, 0x00, 'o', 'k'}

	screen, feed := io.Pipe()
	term := &fakeTerm{out: screen}
	client := stand(t, &fakeOpener{term: term})

	stream, err := client.Open(context.Background(), "aacpanel", 80, 24)
	if err != nil {
		t.Fatalf("the terminal did not open: %v", err)
	}
	defer stream.Close()

	go func() {
		feed.Write(head)
		feed.Write(tail)
	}()

	want := append(append([]byte(nil), head...), tail...)
	got := make([]byte, 0, len(want))
	for len(got) < len(want) {
		buf := make([]byte, 8)
		n, err := stream.Read(buf)
		if err != nil {
			t.Fatalf("the read broke off at %d bytes: %v", len(got), err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the bytes are spoiled:\n expected %x\n got %x", want, got)
	}

	if _, err := stream.Write(head); err != nil {
		t.Fatalf("the input did not leave: %v", err)
	}
	waitFor(t, func() bool { return bytes.Equal(term.written(), head) })
}

func TestTerminalOpenFailureNamesReason(t *testing.T) {
	client := stand(t, &fakeOpener{err: errors.New("the session shop is not among the live ones")})

	_, err := client.Open(context.Background(), "shop", 80, 24)
	if err == nil {
		t.Fatal("opening a session that does not exist went through")
	}
	if !strings.Contains(err.Error(), "not among the live ones") {
		t.Errorf("the reason is lost: %v", err)
	}
}

type errRead struct{ err error }

func (e errRead) Read([]byte) (int, error) { return 0, e.err }
func (e errRead) Close() error             { return nil }

func TestTerminalEndTellsDetachFromBreakage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"end of file", io.EOF, ""},
		{"closed by us", os.ErrClosed, ""},
		{"the pty was left with no slave", &os.PathError{Op: "read", Path: "/dev/ptmx", Err: syscall.EIO}, ""},
		{"the master broke", errors.New("the device fell off"), "the device fell off"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := stand(t, &fakeOpener{term: &fakeTerm{out: errRead{err: c.err}}})

			stream, err := client.Open(context.Background(), "aacpanel", 80, 24)
			if err != nil {
				t.Fatalf("the terminal did not open: %v", err)
			}
			defer stream.Close()

			buf := make([]byte, 32)
			if _, err := stream.Read(buf); !errors.Is(err, io.EOF) {
				t.Fatalf("the stream did not end: %v", err)
			}
			if got := stream.End(); got != c.want {
				t.Errorf("the reason for the end is %q, expected %q", got, c.want)
			}
		})
	}
}

func TestTerminalRefusesSecondOpen(t *testing.T) {
	screen, _ := io.Pipe()
	term := &fakeTerm{out: screen}
	opener := &fakeOpener{term: term}
	client := stand(t, opener)

	stream, err := client.Open(context.Background(), "aacpanel", 80, 24)
	if err != nil {
		t.Fatalf("the terminal did not open: %v", err)
	}
	defer stream.Close()

	if err := stream.frame(Frame{Type: FrameOpen, Target: "another", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("the frame did not leave: %v", err)
	}

	buf := make([]byte, 32)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := stream.Read(buf); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a second open did not break the stream off")
		}
	}
	if !strings.Contains(stream.End(), "already open") {
		t.Errorf("the reason for the end is not clear: %q", stream.End())
	}
	if got := opener.asked(); len(got) != 1 {
		t.Errorf("opened %d times: %v", len(got), got)
	}
}

func TestTerminalLimitStopsFlood(t *testing.T) {
	opener := &openerPerCall{}
	client := stand(t, opener)

	var alive []*Stream
	defer func() {
		for _, s := range alive {
			s.Close()
		}
	}()

	for i := range MaxTerminals {
		s, err := client.Open(context.Background(), fmt.Sprintf("s%d", i), 80, 24)
		if err != nil {
			t.Fatalf("bridge %d did not open: %v", i, err)
		}
		alive = append(alive, s)
	}

	if _, err := client.Open(context.Background(), "one too many", 80, 24); err == nil {
		t.Fatal("a bridge opened past the ceiling")
	} else if !strings.Contains(err.Error(), "no more are held") {
		t.Errorf("a refusal with no reason: %v", err)
	}

	alive[0].Close()
	alive = alive[1:]
	waitFor(t, func() bool {
		s, err := client.Open(context.Background(), "again", 80, 24)
		if err != nil {
			return false
		}
		alive = append(alive, s)
		return true
	})
}

type openerPerCall struct{}

func (openerPerCall) Open(context.Context, string, uint16, uint16) (Terminal, error) {
	screen, _ := io.Pipe()
	return &fakeTerm{out: screen}, nil
}

func TestTerminalRefusesSillySize(t *testing.T) {
	client := stand(t, &fakeOpener{})

	for _, c := range []struct{ cols, rows uint16 }{{0, 24}, {80, 0}, {MaxCols + 1, 24}, {80, MaxRows + 1}} {
		if _, err := client.Open(context.Background(), "aacpanel", c.cols, c.rows); err == nil {
			t.Errorf("the size %dx%d was accepted", c.cols, c.rows)
		}
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("waited in vain")
}

func TestTerminalServerStopsWithOpenBridges(t *testing.T) {
	dir, err := os.MkdirTemp("", "tl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "t.sock")
	srv := NewServer(path, openerPerCall{})
	ln, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ctx, ln)
	}()

	if _, err := NewClient(path).Open(context.Background(), "aacpanel", 80, 24); err != nil {
		t.Fatalf("the terminal did not open: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop waits for an open bridge — in production that is a kill -9 on restart")
	}
}
