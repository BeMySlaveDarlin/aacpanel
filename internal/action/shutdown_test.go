package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShutdownBoundsTheDrainOfAcceptedActions(t *testing.T) {
	shutdownAction(t, SessionRestart, 20*time.Millisecond)
}

func TestShutdownCancelsActionsWithoutALaunch(t *testing.T) {
	shutdownAction(t, SessionClose, time.Minute)
}

func shutdownAction(t *testing.T, kind Kind, drain time.Duration) {
	t.Helper()
	started := make(chan struct{})
	dir, err := os.MkdirTemp("", "drain")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "exec.sock")
	server := NewServer(socket, ExecutorFunc(func(ctx context.Context, _ Request) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}), time.Minute)
	server.drain = drain
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx, ln) }()
	reply := make(chan Response, 1)
	go func() {
		resp, err := NewClient(socket, time.Second).Do(t.Context(), request("shutdown-drain", kind, "probe"))
		if err != nil {
			resp.Error = err.Error()
		}
		reply <- resp
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the accepted action did not start")
	}
	stop()
	select {
	case resp := <-reply:
		if resp.OK || !strings.Contains(resp.Error, "context canceled") {
			t.Fatalf("the drain did not cancel the hanging action: %+v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown outlived its drain deadline")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the server did not stop after draining")
	}
}
