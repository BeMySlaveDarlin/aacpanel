package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/mcp"
)

func observed(t *testing.T, command string) string {
	t.Helper()
	dir := t.TempDir()
	if err := writeRecord(dir, Record{ID: "0123456789abcdef", Spec: Spec{Name: "x", Command: command, Dir: t.TempDir(),
		Wake: EveryLine, Gather: 1, Path: "/opt/session-tools/bin:/usr/bin:/bin"}}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The observer in its unit writes what the command prints and how it ended,
// and the command runs with the PATH of the session.
func TestTheObserverWritesTheLogAndTheEnd(t *testing.T) {
	dir := observed(t, `echo "$PATH"; echo to stderr >&2; kill -TERM $$`)
	end, ok := readEnding(dir)
	if !ok || end.Signal != "TERM" || end.String() != "killed by signal TERM" {
		t.Errorf("the end is %+v", end)
	}
	lines, _, _, _, err := readOn(dir, cursor{}, true)
	if err != nil || !slices.Equal(lines, []string{"/opt/session-tools/bin:/usr/bin:/bin", "to stderr"}) {
		t.Errorf("the log reads %q, %v", lines, err)
	}
	if end, _ := readEnding(observed(t, "exit 7")); end.Code != 7 {
		t.Errorf("an exit of 7 is %+v", end)
	}
}

// A full log is moved aside once, and the reading goes on across the move
// line by line, the first look as well; a line not finished is left for the
// next look until the command has ended.
func TestTheLogIsReadOnAcrossTheMoveOfAFullFile(t *testing.T) {
	was := logMax
	logMax = 200
	t.Cleanup(func() { logMax = was })
	dir := observed(t, `for i in $(seq 1 15); do echo "line $i"; done; sleep 0.2; for i in $(seq 16 30); do echo "line $i"; done; printf unfinished`)
	if _, err := os.Stat(filepath.Join(dir, oldLogFile)); err != nil {
		t.Fatalf("the full log was not moved aside: %v", err)
	}
	var got []string
	at := cursor{}
	for range 10 {
		lines, next, skipped, more, err := readOn(dir, at, false)
		if err != nil || skipped {
			t.Fatalf("the reading failed: %v, skipped %v", err, skipped)
		}
		got, at = append(got, lines...), next
		if !more {
			break
		}
	}
	if len(got) != 30 {
		t.Fatalf("the reading took %d lines: %q", len(got), got)
	}
	for i, l := range got {
		if want := fmt.Sprintf("line %d", i+1); l != want {
			t.Fatalf("line %d reads %q: %q", i+1, l, got)
		}
	}
	if lines, _, _, _, _ := readOn(dir, at, true); !slices.Equal(lines, []string{"unfinished"}) {
		t.Errorf("the end of the log reads %q once the command ended", lines)
	}
	if tail := tail(dir, 3); !slices.Equal(tail, []string{"line 29", "line 30", "unfinished"}) {
		t.Errorf("the tail is %q", tail)
	}
}

// The tools of the server call the executor for the session the server
// serves: a start in the directory of the session unless it names another,
// with the PATH of the session, a list, a tail and a stop, and the set, the
// list and the cancel of an alarm. What the executor refuses comes back to
// the model as its own error.
func TestTheToolsReachTheKeeperForTheSessionCalling(t *testing.T) {
	quick(t)
	socket, err := os.MkdirTemp("", "o")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socket) })
	path := filepath.Join(socket, "s")
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	units, owners := newUnits(), &fakeOwners{alive: true}
	k := NewKeeper(filepath.Join(t.TempDir(), "observers"), units, owners)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go k.Serve(ctx, ln)
	keeping(t, k)

	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(work, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/opt/session-tools/bin:"+os.Getenv("PATH"))
	bind := func() (mcp.Binding, error) {
		return mcp.Binding{Place: mcp.Place{ConfigDir: "/home/u/.claude", Dir: work}, Name: "lab",
			SessionID: lab.Conversation, PID: lab.PID}, nil
	}
	call := func(name, args string) (string, bool) {
		for _, tool := range Tools(path) {
			if tool.Name == name {
				return tool.Call(context.Background(), bind, json.RawMessage(args))
			}
		}
		t.Fatalf("there is no tool %s", name)
		return "", false
	}
	text, failed := call(StartName, `{"name":"pwd","command":"pwd; echo \"$PATH\"; sleep 30","cwd":"sub","wake":"exit"}`)
	if failed || !strings.Contains(text, "Observer pwd started") {
		t.Fatalf("the start answered %q", text)
	}
	waitFor(t, "the log", func() bool {
		text, _ := call(TailName, `{"name":"pwd"}`)
		return strings.Contains(text, "The last 2 lines")
	})
	text, _ = call(TailName, `{"name":"pwd","lines":2}`)
	if !strings.Contains(text, filepath.Join(work, "sub")+"\n/opt/session-tools/bin:") {
		t.Errorf("the command ran elsewhere or with another PATH:\n%s", text)
	}
	if text, _ := call(ListName, `{}`); !strings.Contains(text, "- pwd: `pwd;") {
		t.Errorf("the list is %q", text)
	}
	if text, failed := call(StartName, `{"name":"pwd","command":"true","wake":"exit","after":1}`); !failed ||
		!strings.Contains(text, "not the tool's") {
		t.Errorf("an argument the tool does not take answered %q", text)
	}
	if text, failed := call(StartName, `{"name":"pwd","command":"true","wake":"exit"}`); !failed ||
		!strings.Contains(text, "has an observer pwd already") {
		t.Errorf("a second observer of the name answered %q", text)
	}
	if text, failed := call(StopName, `{"name":"pwd"}`); failed || !strings.Contains(text, "stopped") {
		t.Errorf("the stop answered %q", text)
	}
	if text, _ := call(ListName, `{}`); text != "This session has no observers." {
		t.Errorf("the list after the stop is %q", text)
	}

	if text, failed := call(AlarmSetName, `{"name":"nap","in":"1h","note":"Wake up."}`); failed ||
		!strings.Contains(text, "Alarm nap set: it rings once, at ") {
		t.Errorf("the set answered %q", text)
	}
	if text, failed := call(AlarmSetName, `{"name":"poll","every":"10s"}`); !failed ||
		!strings.Contains(text, "less than 1m") {
		t.Errorf("a repeat the executor does not take answered %q", text)
	}
	if text, failed := call(AlarmSetName, `{"name":"poll","in":"1h","after":1}`); !failed ||
		!strings.Contains(text, "not the tool's") {
		t.Errorf("an argument the tool does not take answered %q", text)
	}
	if text, _ := call(AlarmListName, `{}`); !strings.Contains(text, "- nap: rings once, at ") ||
		!strings.Contains(text, "note: Wake up.") {
		t.Errorf("the list of alarms is %q", text)
	}
	if text, failed := call(AlarmCancelName, `{"name":"nap"}`); failed || text != "Alarm nap cancelled: it rings no more." {
		t.Errorf("the cancel answered %q", text)
	}
	if text, _ := call(AlarmListName, `{}`); text != "This session has no alarms." {
		t.Errorf("the list after the cancel is %q", text)
	}
	cancel()
	time.Sleep(20 * time.Millisecond)
	if text, failed := call(ListName, `{}`); !failed || !strings.Contains(text, "does not answer") {
		t.Errorf("a call with no executor answered %q", text)
	}
}
