package codex

import (
	"testing"
)

func TestDaemonRestartWaitsForEveryLoadedTurn(t *testing.T) {
	l := &Link{
		conn: &conn{closed: make(chan struct{})}, polled: true,
		activity: map[string]string{"parent": "idle", "child": "active"},
		threads:  map[string]*thread{"parent": {}},
	}
	restarted := 0
	restart := func() error { restarted++; return nil }
	if n, err := l.RestartIdle(restart); err != nil || n != 1 || restarted != 0 {
		t.Fatalf("a child turn was cut: running %d, restarts %d, error %v", n, restarted, err)
	}
	l.activity["child"] = "idle"
	l.threads["parent"].ours = true
	if n, err := l.RestartIdle(restart); err != nil || n != 1 || restarted != 0 {
		t.Fatalf("a turn/start ahead of its active status was cut: %d, %d, %v", n, restarted, err)
	}
	l.threads["parent"].ours = false
	l.activity["unread"] = ""
	if _, err := l.RestartIdle(restart); err == nil || restarted != 0 {
		t.Fatal("an unread thread was treated as idle")
	}
	delete(l.activity, "unread")
	if n, err := l.RestartIdle(restart); err != nil || n != 0 || restarted != 1 {
		t.Fatalf("idle daemon was not restarted: %d, %d, %v", n, restarted, err)
	}
	close(l.conn.closed)
	if _, err := l.RestartIdle(restart); err == nil || restarted != 1 {
		t.Fatal("a disconnected daemon was treated as idle")
	}
}
