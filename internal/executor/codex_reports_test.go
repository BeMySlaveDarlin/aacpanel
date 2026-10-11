package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

// One daemon and holder exercise the whole channel, including repeated turns:
// the parent writes immediately, and the last message comes back without an
// MCP call by the child or a collector snapshot of the new thread.
func TestACodexHelperReportsEveryTurnToItsOpeningSession(t *testing.T) {
	srv, e, p := onCodexContour(t)
	f := onTheStream(t, true)
	p.OpenedBy, p.Parent = streamSID, "demo"
	p.Codex = &action.CodexLaunch{Model: "gpt-6.1-sol", Effort: "high", WritableRoots: []string{p.Path}}
	_, name, err := open(t, e, p, `{"agent":"codex","codexTransport":"tmux","codexModel":"gpt-test",`+
		`"codexApproval":"on-request","codexSandbox":"danger-full-access","intent":"map task"}`)
	if err != nil {
		t.Fatal(err)
	}
	th := e.codex.Find(name)[0]
	start := paramsOf(t, srv.Calls("thread/start")[0])
	config := start["config"].(map[string]any)
	if start["model"] != "gpt-6.1-sol" || start["approvalPolicy"] != "never" || start["sandbox"] != "workspace-write" ||
		config["model_reasoning_effort"] != "high" || config["sandbox_workspace_write.network_access"] != true ||
		!reflect.DeepEqual(config["sandbox_workspace_write.writable_roots"], []any{p.Path}) {
		t.Fatalf("the helper started with %v", start)
	}
	if len(srv.Calls("turn/start")) != 0 || !heldMark(t, th.ID) {
		t.Fatal("the helper must wait for a letter, held by the daemon")
	}
	ctx := context.Background()
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionLetter, Target: name, From: streamSID, Text: "check the router"}); err != nil {
		t.Fatal(err)
	}
	first := string(srv.Calls("turn/start")[0])
	if !strings.Contains(first, "Your parent session demo, which opened you") || !strings.Contains(first, "check the router") ||
		strings.Contains(first, "weigh it as a request") || !strings.Contains(first, "stay open for follow-ups") {
		t.Fatalf("the task is not framed as the parent's: %s", first)
	}
	long := strings.Repeat("a complete report with Unicode: λ\n", 2500)
	for i, c := range []struct{ status, failure, text string }{
		{"completed", "", "the final answer"}, {"failed", "quota exhausted", "partial findings"},
		{"interrupted", "", "stopped here"}, {"completed", "", long},
	} {
		turn := "helper-turn-" + c.status + string(rune('a'+i))
		srv.Running(th.ID, turn)
		srv.Item(th.ID, turn, map[string]any{"type": "agentMessage", "id": "progress", "text": "earlier progress"})
		srv.Item(th.ID, turn, map[string]any{"type": "agentMessage", "id": "final", "text": c.text})
		srv.Complete(th.ID, c.status, c.failure)
		// A duplicate, even with different contents, cannot generate another letter.
		srv.Notify(th.ID, "turn/completed", map[string]any{"turn": map[string]any{"id": turn, "status": c.status}})
		waitFor(t, "one report for the turn", func() bool { return len(f.asked()) >= i+1 })
		got := f.asked()[i]
		if got.Op != stream.OpLetter || !strings.Contains(got.Text, c.status) || !strings.Contains(got.Text, c.failure) ||
			strings.Contains(got.Text, "earlier progress") {
			t.Fatalf("report %d: %+v", i, got)
		}
		if i < 3 {
			if !strings.Contains(got.Text, c.text) {
				t.Fatalf("the last message is missing: %s", got.Text)
			}
		} else {
			prefix := "Read the full report at "
			_, tail, ok := strings.Cut(got.Text, prefix)
			path, _, _ := strings.Cut(tail, "\n")
			body, err := os.ReadFile(path)
			if !ok || err != nil || !strings.HasSuffix(string(body), long) {
				t.Fatalf("the full report was not delivered by file: %s, %v", got.Text, err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 || len(got.Text) >= reportInlineMax {
				t.Fatal("the file is not private or the letter still exceeds its ceiling")
			}
		}
		if _, err := e.Execute(ctx, action.Request{Kind: action.SessionLetter, Target: name, From: streamSID, Text: "continue"}); err != nil {
			t.Fatal(err)
		}
	}
	// A later delivery on the same serial worker proves the duplicate events
	// ahead of it have been consumed; no guessed pause checks their absence.
	if len(f.asked()) != 4 {
		t.Fatalf("one turn produced more than one letter: %d", len(f.asked()))
	}
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionClose, Target: name, Caller: streamSID}); err != nil {
		t.Fatal(err)
	}
	if len(e.codex.Find(name)) != 0 || heldMark(t, th.ID) {
		t.Fatal("the parent's close left its child alive")
	}
	waitFor(t, "the interrupted turn's report after closing", func() bool { return len(f.asked()) >= 5 })
	if len(f.asked()) != 5 || !strings.Contains(f.asked()[4].Text, "interrupted") {
		t.Fatal("closing the helper lost or duplicated the interrupted turn's report")
	}
}

func TestAVisualParentGrantsNoReportOrChildClose(t *testing.T) {
	srv, e, p := onCodexContour(t)
	f := onTheStream(t, false)
	p.Parent = "demo"
	_, name, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	th := e.codex.Find(name)[0]
	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionClose, Target: name, Caller: streamSID}); err == nil || !strings.Contains(err.Error(), "not opened") {
		t.Fatalf("a grouped thread was closed by a non-owner: %v", err)
	}
	srv.Running(th.ID, "grouped-turn")
	srv.Complete(th.ID, "completed", "")
	if err := th.Link.SetParent(th.ID, "someone"); err != nil {
		t.Fatal(err)
	}
	// An actual helper's report is the barrier after the non-helper event.
	p.OpenedBy = streamSID
	_, child, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	helper := e.codex.Find(child)[0]
	srv.Running(helper.ID, "owned-turn")
	srv.Complete(helper.ID, "completed", "")
	waitFor(t, "the owned thread's report", func() bool { return len(f.asked()) > 0 })
	if got := only(t, f); strings.Contains(got.Text, "grouped-turn") || !strings.Contains(got.Text, "owned-turn") {
		t.Fatalf("a visual parent got a report: %s", got.Text)
	}
	if len(e.codex.Find(name)) != 1 {
		t.Fatal("the refusal closed the non-child")
	}
}

func TestACodexParentReceivesItsChildsReportThroughTheSameDaemon(t *testing.T) {
	srv, e, p := onCodexContour(t)
	_, parent, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.OpenedBy = e.codex.Find(parent)[0].ID
	_, child, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	th := e.codex.Find(child)[0]
	srv.Running(th.ID, "done-turn")
	srv.Item(th.ID, "done-turn", map[string]any{"type": "agentMessage", "text": "done for parent"})
	srv.Complete(th.ID, "completed", "")
	waitFor(t, "the report to start the parent's turn", func() bool { return len(srv.Calls("turn/start")) == 1 })
	turn := paramsOf(t, srv.Calls("turn/start")[0])
	raw, _ := json.Marshal(turn["input"])
	if turn["threadId"] != p.OpenedBy || !strings.Contains(string(raw), "done for parent") {
		t.Fatalf("the report went to another thread: %v", turn)
	}
	// Reparenting by hand does not transfer the opener's authority.
	if err := th.Link.SetParent(th.ID, "another-parent"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionClose, Target: child, Caller: th.ID}); err == nil || !strings.Contains(err.Error(), "not opened") {
		t.Fatalf("a live non-owner closed the helper: %v", err)
	}
	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionClose, Target: child, Caller: p.OpenedBy}); err != nil {
		t.Fatal(err)
	}
}

func TestAReportDoesNotReachATmuxOrGoneParent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556", args: []string{"claude"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "console", start: "5556", sid: senderSID})
	e := New(nil, "")
	r := codex.TurnReport{Thread: codexThread, Turn: "turn", Status: "completed", Text: "done",
		Owner: stream.SessionOwner{Name: "console", Conversation: senderSID}}
	if err := e.reportCodexTurn(context.Background(), r); err == nil || !strings.Contains(err.Error(), "tmux") {
		t.Fatalf("a tmux parent received a report: %v", err)
	}
	procFS(t)
	if err := e.reportCodexTurn(context.Background(), r); err == nil {
		t.Fatal("a gone parent received a report")
	}
}

func TestACodexHelperCanStartReadOnlyAndRefusesUnknownLaunchValues(t *testing.T) {
	srv, e, p := onCodexContour(t)
	onTheStream(t, false)
	p.OpenedBy = streamSID
	for _, c := range []action.CodexLaunch{{Effort: "ultracode"}, {Sandbox: "danger-full-access"},
		{WritableRoots: []string{"relative"}}, {Sandbox: "read-only", WritableRoots: []string{p.Path}}} {
		p.Codex = &c
		if _, _, err := open(t, e, p, `{"agent":"codex"}`); err == nil {
			t.Errorf("invalid launch was accepted: %+v", c)
		}
	}
	if len(srv.Calls("thread/start")) != 0 {
		t.Fatal("an invalid launch reached the daemon")
	}
	p.Codex = &action.CodexLaunch{Sandbox: "read-only"}
	if _, _, err := open(t, e, p, `{"agent":"codex","codexModel":"gpt-6.1-sol","codexEffort":"low"}`); err != nil {
		t.Fatal(err)
	}
	start := paramsOf(t, srv.Calls("thread/start")[0])
	if start["sandbox"] != "read-only" || start["approvalPolicy"] != "never" || start["model"] != "gpt-6.1-sol" ||
		start["config"].(map[string]any)["sandbox_workspace_write.network_access"] != nil {
		t.Fatalf("read-only did not reach the daemon: %v", start)
	}
	// The provenance outlives an executor; grouping remains a separate file.
	entries, _ := filepath.Glob(filepath.Join(filepath.Dir(stream.OpenedPath("x")), "*.json"))
	if len(entries) != 1 || stream.OwnerOf("019a2000-0000-7000-8000-0000beef0001").Conversation != streamSID {
		t.Fatalf("the helper has no persistent opening record: %v", entries)
	}
}
