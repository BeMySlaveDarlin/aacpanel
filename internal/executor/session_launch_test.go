package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

func TestLauncherFinishesDuringExecutorShutdown(t *testing.T) {
	dir, err := os.MkdirTemp("", "launch")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	gate := filepath.Join(dir, "gate")
	if err := syscall.Mkfifo(gate, 0o600); err != nil {
		t.Fatal(err)
	}
	pipe, err := os.OpenFile(gate, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pipe.Close() })
	ready := filepath.Join(dir, "ready")
	launcherScript(t, "cat > %s\ntouch '"+ready+"'\nread -r line < '"+gate+"'\nprintf '{\"session\":\"probe\"}'\n")
	e, _ := newTest(t, "")
	launchContext := make(chan context.Context, 1)
	server := action.NewServer(filepath.Join(dir, "exec.sock"), action.ExecutorFunc(func(ctx context.Context, _ action.Request) (string, error) {
		launchContext <- ctx
		rep, err := e.runLauncher(ctx, project{Path: dir, Session: "probe"}, "")
		return rep.Session, err
	}), time.Second)
	ln, err := server.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx, ln) }()
	t.Cleanup(func() {
		stop()
		pipe.Write([]byte("finish\n"))
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("the executor did not stop")
		}
	})
	reply := make(chan action.Response, 1)
	go func() {
		resp, err := action.NewClient(filepath.Join(dir, "exec.sock"), 2*time.Second).Do(t.Context(),
			action.Request{ID: "launch-drain", Kind: action.SessionRestart, Target: "probe"})
		if err != nil {
			resp.Error = err.Error()
		}
		reply <- resp
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case resp := <-reply:
			t.Fatalf("launch ended before shutdown: %+v", resp)
		case <-deadline.C:
			t.Fatal("the launcher did not reach the gate")
		case <-tick.C:
		}
	}
	stop()
	// Release the launcher only after shutdown closes the listener.
	for {
		conn, err := ln.Accept()
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			break
		}
	}
	if err := (<-launchContext).Err(); err != nil {
		t.Fatalf("shutdown canceled the accepted launch: %v", err)
	}
	if _, err := pipe.Write([]byte("finish\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-reply:
		if !resp.OK || resp.Detail != "probe" {
			t.Fatalf("shutdown canceled the launcher: %+v", resp)
		}
	case <-deadline.C:
		t.Fatal("the launcher did not finish during shutdown")
	}
}

func TestLaunchJournalNamesTheIntent(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	fakeLauncher(t, launcher.Report{
		Session: "aacpanel", Konsole: 1, Agent: 2,
		Intent: "read the queue and take the first one",
	})
	e, _ := newTest(t, "")

	detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "read the queue and take the first one") {
		t.Errorf("the reply %q says nothing about the intent — the session started working and it is unclear on whose word", detail)
	}
}

func TestLaunchJournalShortensALongIntent(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	long := strings.Repeat("x", 400)
	fakeLauncher(t, launcher.Report{
		Session: "aacpanel", Konsole: 1, Agent: 2,
		Intent: "first line\nsecond " + long,
	})
	e, _ := newTest(t, "")

	detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail, "\n") {
		t.Errorf("a multi-line intent tore the action log entry apart: %q", detail)
	}
	if len([]rune(detail)) > 300 {
		t.Errorf("the action log entry grew to %d characters — the intent was not shortened: %q", len([]rune(detail)), detail)
	}
	if !strings.Contains(detail, "first line second") {
		t.Errorf("no beginning of the intent is left in the action log: %q", detail)
	}
}
