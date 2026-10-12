package cdp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func TestPipeRoutesSessionsCancelsAndWakesOnExit(t *testing.T) {
	a, b := net.Pipe()
	events := make(chan Message, 1)
	p := New(a, a, func(m Message) { events <- m })
	defer p.Close()
	defer b.Close()
	go func() {
		in := bufio.NewReader(b)
		for {
			raw, err := in.ReadBytes(0)
			if err != nil {
				return
			}
			var m Message
			if json.Unmarshal(raw[:len(raw)-1], &m) != nil {
				return
			}
			if m.Method == "wait" {
				continue
			}
			ev, _ := json.Marshal(Message{Method: "Page.frameNavigated", SessionID: m.SessionID})
			b.Write(append(ev, 0))
			out, _ := json.Marshal(Message{ID: m.ID, Result: json.RawMessage(`{"ok":true}`)})
			b.Write(append(out, 0))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out struct{ OK bool }
	if err := p.Call(ctx, "page-a", "Page.enable", struct{}{}, &out); err != nil || !out.OK {
		t.Fatalf("call: %+v %v", out, err)
	}
	if ev := <-events; ev.SessionID != "page-a" {
		t.Fatal("event lost its session")
	}
	short, stop := context.WithTimeout(ctx, 10*time.Millisecond)
	defer stop()
	if err := p.Call(short, "page-a", "wait", struct{}{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	ended := make(chan error, 1)
	go func() { ended <- p.Call(ctx, "page-a", "wait", struct{}{}, nil) }()
	b.Close()
	if err := <-ended; err == nil {
		t.Fatal("pipe exit did not wake pending call")
	}
}

func TestPipeDeadlineCoversAChromeThatDoesNotRead(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	p := New(a, a, nil)
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- p.Call(ctx, "", "Browser.close", struct{}{}, nil) }()
	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("a command nobody read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Chrome held the caller inside a blocked write")
	}
}
