package executor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func shortBusTimeout(t *testing.T) {
	t.Helper()
	was := konsoleTimeout
	konsoleTimeout = 150 * time.Millisecond
	t.Cleanup(func() { konsoleTimeout = was })
}

func stallStand(t *testing.T) {
	t.Helper()
	procFS(t,
		fakeProc{pid: 500, comm: "konsole", args: []string{"konsole"}, ppid: 1},
		fakeProc{pid: 501, comm: "claude", args: []string{"claude"}, ppid: 500, start: "77"},
	)
}

func TestKonsoleTabForRetriesAfterStall(t *testing.T) {
	stallStand(t)
	shortBusTimeout(t)
	fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 501}, stallTree: 1})

	tab, err := konsoleTabFor(t.Context(), 501)
	if err != nil {
		t.Fatalf("the window came back to life, yet the tab was not found: %v", err)
	}
	if tab.Path != "/Sessions/1" {
		t.Errorf("tab %q, expected /Sessions/1", tab.Path)
	}
	if tab.Attempts != 2 {
		t.Errorf("%d attempts, expected 2: a stall that leaves no trace in the reply is visible nowhere", tab.Attempts)
	}
}

func TestKonsoleStallIsNamedAndStopsSearch(t *testing.T) {
	stallStand(t)
	shortBusTimeout(t)
	const first = "unix:path=/tmp/aacpanel-test-first"
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", first)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 501}, bus: first, stallTree: 2})

	_, err := konsoleTabFor(t.Context(), 501)
	if !errors.Is(err, errBusStall) {
		t.Fatalf("a stall was not called a stall: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"did not answer", "tree", "150ms"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error %q has no %q", msg, want)
		}
	}
	for _, bad := range []string{"killed", "exit status"} {
		if strings.Contains(msg, bad) {
			t.Errorf("the error %q says %q instead of the reason", msg, bad)
		}
	}
}

func TestKonsoleTabForNamesUnansweredTabs(t *testing.T) {
	stallStand(t)
	fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 501}, tabError: "Call failed: Connection timed out"})

	_, err := konsoleTabFor(t.Context(), 501)
	if err == nil {
		t.Fatal("the tab did not answer, yet the search succeeded")
	}
	msg := err.Error()
	if !strings.Contains(msg, "do not answer") || !strings.Contains(msg, "Connection timed out") {
		t.Errorf("the error %q names neither that the tabs are not answering nor why", msg)
	}
	if strings.Contains(msg, "no tab with process") {
		t.Errorf("the error %q calls for restarting the session where waiting is what is needed", msg)
	}
}

func TestSessionSendStallOnPasteSaysUnknown(t *testing.T) {
	socket, got := listenFake(t)
	stallStand(t)
	shortBusTimeout(t)
	sessionFiles(t, fakeSession{pid: 501, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 501}, swallow: 1, stallSend: true})

	e := &Executor{}
	_, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs")
	if err == nil {
		t.Fatal("konsole stayed silent on sendText, yet the send succeeded")
	}
	msg := err.Error()
	for _, want := range []string{"is unknown", "look at the feed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error %q has no %q", msg, want)
		}
	}
	select {
	case letter := <-got:
		t.Errorf("the reply went out as a letter: %q", letter)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionSendReportsSecondAttempt(t *testing.T) {
	socket, _ := listenFake(t)
	stallStand(t)
	shortBusTimeout(t)
	sessionFiles(t, fakeSession{pid: 501, name: "aacpanel", start: "77", socket: socket, status: "idle"})
	fakeBusctlAs(t, fakeBus{tabs: map[string]int{"/Sessions/1": 501}, swallow: 1, stallTree: 1})

	e := &Executor{}
	detail, err := e.sessionSend(t.Context(), "aacpanel", "check the stack logs")
	if err != nil {
		t.Fatalf("the window came back to life, yet the send failed: %v", err)
	}
	if !strings.Contains(detail, "typed into") || !strings.Contains(detail, "on the second attempt") {
		t.Errorf("the reply %q does not say konsole answered on the second attempt", detail)
	}
}
