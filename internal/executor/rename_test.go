package executor

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

// A session on the stream is renamed with claude's own request, which writes
// the name where the panel looks a session up.
func TestRenameGoesAsClaudesRequest(t *testing.T) {
	f := onTheStream(t, false)
	e, _ := newTest(t, "")
	r := req(action.SessionRename, "demo")
	r.Rename = "demo-pilot"
	detail, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	asked := f.asked()
	if len(asked) != 1 || asked[0].Subtype != "rename_session" || asked[0].Fields["title"] != "demo-pilot" ||
		asked[0].Fields["source"] != "host" {
		t.Fatalf("the holder was asked %+v", asked)
	}
	if !strings.Contains(detail, "demo-pilot") {
		t.Errorf("the report %q does not say the new name", detail)
	}
}

// A name another live session answers to is refused before claude is asked:
// two sessions would answer to one name.
func TestRenameIntoATakenNameIsRefused(t *testing.T) {
	f := onTheStream(t, false)
	// The helpers lay out the whole table each time: the session on the stream
	// is laid out again beside its namesake.
	procFS(t,
		fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5555",
			args: []string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", streamSID}},
		fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
			args: []string{"claude", "-n", "term"}})
	sessionFiles(t,
		fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID},
		fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	r := req(action.SessionRename, "demo")
	r.Rename = "term"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a taken name was not refused: %v", err)
	}
	if asked := f.asked(); len(asked) != 0 {
		t.Errorf("claude was asked %+v for a name that is taken", asked)
	}
}

// A terminal is renamed on its own screen: the panel does not type into it blind.
func TestATerminalIsNotRenamedFromThePanel(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	r := req(action.SessionRename, "term")
	r.Rename = "term-2"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "own screen") {
		t.Errorf("a terminal was renamed: %v", err)
	}
}
