package main

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/executor"
	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionpin"
)

func wakeSocket(t *testing.T) *net.UnixListener {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AACP_SEEN_DIR", dir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "seen.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func readWake(t *testing.T, listener *net.UnixListener) net.Conn {
	t.Helper()
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("the collector received no wake: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		t.Fatal(err)
	}
	if len(request) != 1 || request["wake"] != "sessions" {
		t.Fatalf("the collector received %v instead of the sessions wake", request)
	}
	return conn
}

func TestPinWake(t *testing.T) {
	proc, config, state := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("AACP_PROC", proc)
	t.Setenv("AACP_SESSIONS", filepath.Join(config, "sessions"))
	t.Setenv("XDG_STATE_HOME", state)
	fakeClaude(t, proc, config, 4242, "5555", "pin-conversation", "lab")
	listener := wakeSocket(t)
	a := audited{next: executor.New(nil, "")}
	on := true
	req := action.Request{Kind: action.SessionPin, Target: "lab", Pin: &on}
	done := make(chan error, 1)
	go func() {
		_, err := a.Execute(t.Context(), req)
		done <- err
	}()
	conn := readWake(t, listener)
	// A silent collector must not hold the result of the pin in its socket read.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("the pin waited for the collector's reply")
	}
	if err := json.NewEncoder(conn).Encode(map[string]bool{"ok": true}); err != nil {
		t.Fatal(err)
	}
	place := mcp.Place{ConfigDir: config, Dir: "/srv/proj/lab"}
	if pin, err := sessionpin.Read(sessionpin.Dir(), place, "lab"); err != nil || pin == nil {
		t.Fatalf("the wake preceded the successful pin: %+v, %v", pin, err)
	}

	t.Setenv("AACP_SEEN_DIR", filepath.Join(state, "no-collector"))
	on = false
	if _, err := a.Execute(t.Context(), req); err != nil {
		t.Fatalf("an absent collector failed the unpin: %v", err)
	}
	if pin, err := sessionpin.Read(sessionpin.Dir(), place, "lab"); err != nil || pin != nil {
		t.Fatalf("an absent collector prevented the unpin: %+v, %v", pin, err)
	}
}

func TestActionWake(t *testing.T) {
	listener := wakeSocket(t)
	for _, kind := range []action.Kind{action.SessionPin, action.SessionClose, action.SessionKill,
		action.SessionRestart, action.SessionSwitch, action.SessionOpen, action.SessionResume} {
		t.Run(string(kind), func(t *testing.T) {
			if _, err := (audited{next: ableExec{}}).Execute(t.Context(), action.Request{Kind: kind}); err != nil {
				t.Fatal(err)
			}
			conn := readWake(t, listener)
			_ = json.NewEncoder(conn).Encode(map[string]bool{"ok": true})
		})
	}
	for _, req := range []action.Request{
		{Kind: action.SessionOpen},
		{Kind: action.SessionResume, Agent: action.ResumeCodex},
	} {
		if _, session, err := (audited{next: openingExec{}}).Open(t.Context(), req); err != nil || session == "" {
			t.Fatalf("opening a session answered %q, %v", session, err)
		}
		conn := readWake(t, listener)
		_ = json.NewEncoder(conn).Encode(map[string]bool{"ok": true})
	}
	for _, test := range []struct {
		name string
		next action.Executor
		kind action.Kind
	}{
		{"failed pin", muteExec{}, action.SessionPin},
		{"successful send", ableExec{}, action.SessionSend},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _ = (audited{next: test.next}).Execute(t.Context(), action.Request{Kind: test.kind})
			if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			conn, err := listener.Accept()
			if err == nil {
				_ = conn.Close()
				t.Fatal("an action that needs no fresh snapshot woke the collector")
			}
			if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
				t.Fatal(err)
			}
		})
	}
}
