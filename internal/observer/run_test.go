package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
	"aacpanel/internal/session"
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
	var recordMu sync.Mutex
	type entry struct {
		Kind   action.Kind
		Target string
		Params action.WorkRecord
	}
	var records []entry
	recordStatus := http.StatusOK
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/actions" {
			http.NotFound(w, r)
			return
		}
		var rec entry
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			t.Error(err)
		}
		recordMu.Lock()
		records = append(records, rec)
		status := recordStatus
		recordMu.Unlock()
		w.WriteHeader(status)
		if status == http.StatusOK {
			fmt.Fprint(w, `{"ok":true,"logged":true}`)
		} else {
			fmt.Fprint(w, `{"error":"database unavailable","logged":false}`)
		}
	}))
	defer panel.Close()
	t.Setenv(session.PanelEnv, panel.URL)

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
	served := make(chan struct{})
	go func() {
		k.Serve(ctx, ln)
		close(served)
	}()
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
	alarms, err := k.Alarms(ctx, lab)
	if err != nil || len(alarms) != 1 {
		t.Fatalf("alarms before the cancel: %+v, %v", alarms, err)
	}
	recordMu.Lock()
	got := append([]entry{}, records...)
	recordMu.Unlock()
	if len(got) != 2 {
		t.Fatalf("successful starts made %d records; lists, refusals and stops must make none: %+v", len(got), got)
	}
	if got[0].Kind != action.ObserverStart || got[0].Target != "lab" || got[0].Params.From != lab.Conversation || got[0].Params.Name != "pwd" || got[0].Params.Command != `pwd; echo "$PATH"; sleep 30` || got[0].Params.Cwd != filepath.Join(work, "sub") {
		t.Errorf("observer record: %+v", got[0])
	}
	if got[1].Kind != action.AlarmSet || got[1].Target != "lab" || got[1].Params.From != lab.Conversation || got[1].Params.Name != "nap" || got[1].Params.At != alarms[0].Next.Format(time.RFC3339) || got[1].Params.Every != "" {
		t.Errorf("alarm record: %+v", got[1])
	}
	if text, failed := call(AlarmCancelName, `{"name":"nap"}`); failed || text != "Alarm nap cancelled: it rings no more." {
		t.Errorf("the cancel answered %q", text)
	}
	if text, _ := call(AlarmListName, `{}`); text != "This session has no alarms." {
		t.Errorf("the list after the cancel is %q", text)
	}
	codexBind := func() (mcp.Binding, error) {
		return mcp.Binding{SessionID: "019a1f00-0000-7000-8000-00000000abcd", Name: "codex-0000abcd", Codex: true, Place: mcp.Place{ConfigDir: "/home/u/.codex", Dir: work}}, nil
	}
	for _, tool := range CodexTools(path) {
		if tool.Name != AlarmSetName {
			continue
		}
		text, failed := tool.Call(ctx, codexBind, json.RawMessage(`{"name":"codex.poll","every":"10m"}`))
		if failed || !strings.Contains(text, "Written to the panel's journal.") {
			t.Fatalf("codex set: %s", text)
		}
	}
	recordMu.Lock()
	rec := records[len(records)-1]
	recordStatus = http.StatusServiceUnavailable
	recordMu.Unlock()
	if rec.Target != "codex-0000abcd" || rec.Params.From != "019a1f00-0000-7000-8000-00000000abcd" || rec.Params.Name != "codex.poll" || rec.Params.Every != "10m0s" {
		t.Errorf("codex record: %+v", rec)
	}
	for _, item := range []struct{ tool, args string }{
		{StartName, `{"name":"unlogged","command":"sleep 30","wake":"exit"}`},
		{AlarmSetName, `{"name":"unlogged","every":"10m"}`},
	} {
		text, failed := call(item.tool, item.args)
		if failed || !strings.Contains(text, "started, not written to the panel's journal: database unavailable") {
			t.Errorf("a failed record changed the successful start: %q, failed %v", text, failed)
		}
	}
	if list, err := k.List(ctx, lab); err != nil || len(list) != 1 {
		t.Errorf("a failed journal write undid the observer: %+v, %v", list, err)
	}
	if list, err := k.Alarms(ctx, lab); err != nil || len(list) != 1 {
		t.Errorf("a failed journal write undid the alarm: %+v, %v", list, err)
	}
	if text, failed := call(StopName, `{"name":"unlogged"}`); failed {
		t.Fatal(text)
	}
	if text, failed := call(AlarmCancelName, `{"name":"unlogged"}`); failed {
		t.Fatal(text)
	}
	cancel()
	<-served
	if text, failed := call(ListName, `{}`); !failed || !strings.Contains(text, "does not answer") {
		t.Errorf("a call with no executor answered %q", text)
	}
}
