package executor

import (
	"context"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionpin"
)

func pinRequest(target string, on bool) action.Request {
	r := req(action.SessionPin, target)
	r.Pin = &on
	return r
}

func checkSessionPin(t *testing.T, e *Executor, place mcp.Place, name string) {
	t.Helper()
	for _, on := range []bool{true, true, false} {
		if _, err := e.Execute(context.Background(), pinRequest(name, on)); err != nil {
			t.Fatal(err)
		}
		pin, err := sessionpin.Read(sessionpin.Dir(), place, name)
		if err != nil || (pin != nil) != on {
			t.Fatalf("pin %v: the session has %+v, %v", on, pin, err)
		}
	}
}

func TestThePinReachesTheSessionWithoutWritingToItsConversation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f := onTheStream(t, false)
	e, _ := newTest(t, "")
	checkSessionPin(t, e, sessionPlace(t, "demo"), "demo")
	if asked := f.asked(); len(asked) != 0 {
		t.Errorf("pinning wrote to the conversation: %+v", asked)
	}
	if _, err := e.Execute(context.Background(), pinRequest("gone", true)); err == nil {
		t.Fatal("a session that does not live was pinned")
	}
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	checkSessionPin(t, e, sessionPlace(t, "term"), "term")
}

func TestACodexPinUsesItsAccountAndDirectory(t *testing.T) {
	srv, e := onCodex(t, nil)
	checkSessionPin(t, e, mcp.Place{ConfigDir: srv.Home, Dir: "/srv/proj"}, codexName)
	if turns := srv.Calls("turn/start"); len(turns) != 0 {
		t.Errorf("pinning started turns: %s", turns)
	}
}
