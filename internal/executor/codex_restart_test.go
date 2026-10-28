package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/stream"
)

func TestACodexRestartKeepsItsPlaceAndClosesAfterTheCallingTurn(t *testing.T) {
	for _, parent := range []string{"", "lead"} {
		t.Run("parent="+parent, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			newTmuxStub(t, nil, "")
			p.Parent = parent
			// An idle status does not mean an ended turn: the daemon can
			// acknowledge turn/start before publishing the active status.
			srv.Lag()
			_, name, err := open(t, e, p, `{"agent":"codex","codexModel":"gpt-old","codexEffort":"high","codexSandbox":"read-only","intent":"work"}`)
			if err != nil {
				t.Fatal(err)
			}
			old := e.codex.Find(name)[0]
			if err := old.Link.Rename(t.Context(), old.ID, "cve fix"); err != nil {
				t.Fatal(err)
			}
			owner := stream.SessionOwner{Name: "report-recipient", Conversation: streamSID}
			if err := stream.KeepOwner(old.ID, owner); err != nil {
				t.Fatal(err)
			}
			childID, err := old.Link.Start(t.Context(), codex.Begin{CWD: p.Path, Hold: true, Name: "child", Parent: name,
				Owner: stream.SessionOwner{Name: name, Conversation: old.ID}})
			if err != nil {
				t.Fatal(err)
			}
			procFS(t,
				fakeProc{pid: 5001, comm: "claude", start: "5555", args: []string{"claude", "-n", "claude-child"}},
				fakeProc{pid: 5002, comm: "claude", start: "5556", args: []string{"claude", "-n", "detached"}})
			sessionFiles(t, fakeSession{pid: 5001, name: "claude-child", start: "5555"},
				fakeSession{pid: 5002, name: "detached", start: "5556"})
			// The executor cannot read a session's environment: the thread's
			// Claude children are known by the opening records of their processes.
			for _, pid := range []int{5001, 5002} {
				key, err := processOwnerKey(pid)
				if err == nil {
					err = stream.KeepOwner(key, stream.SessionOwner{Name: name, Conversation: old.ID})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := e.sessionDetach("detached"); err != nil {
				t.Fatal(err)
			}
			cwd := p.Path
			p.Path, p.ConfigDir, p.Parent = t.TempDir(), "/another/home", "stale-parent"
			p.Launch = json.RawMessage(`{"agent":"codex","codexModel":"gpt-map","codexEffort":"low","codexSandbox":"workspace-write","codexTransport":"tmux","intent":"map message"}`)
			r := action.Request{Kind: action.SessionRestart, Target: name, Project: p, Conversation: old.ID,
				Agent: action.ResumeCodex, Text: "Read the saved handoff"}
			detail, err := e.Execute(t.Context(), r)
			if err != nil {
				t.Fatal(err)
			}
			starts := srv.Calls("thread/start")
			if len(starts) != 3 {
				t.Fatalf("restart started %d threads, wanted three including old and child", len(starts))
			}
			start := paramsOf(t, starts[2])
			config := start["config"].(map[string]any)
			if start["cwd"] != cwd || start["model"] != "gpt-old" || start["sandbox"] != "read-only" ||
				start["approvalPolicy"] != "never" || config["model_reasoning_effort"] != "high" ||
				config["mcp_servers.aacpanel.command"] != panelTools {
				t.Fatalf("the replacement lost the live settings: %v", start)
			}
			turns := srv.Calls("turn/start")
			if len(turns) != 2 || !strings.Contains(string(turns[1]), r.Text) {
				t.Fatalf("the first message was not the restart text: %s", turns)
			}
			newID := paramsOf(t, turns[1])["threadId"].(string)
			replacement := e.codex.Find(codex.SessionName(newID))
			if len(replacement) != 1 || replacement[0].Link != old.Link || !heldMark(t, newID) {
				t.Fatalf("the replacement is not held by the old home: %+v", replacement)
			}
			settings, err := old.Link.RestartSettings(newID)
			if err != nil || settings.Name != "cve fix" {
				t.Fatalf("the replacement is not called as the old thread was: %+v, %v", settings, err)
			}
			if settings.Parent != parent || stream.OwnerOf(newID) != owner {
				t.Fatalf("the replacement lost its parent or opener: %+v, %+v, %v", settings, stream.OwnerOf(newID), err)
			}
			child, err := old.Link.RestartSettings(childID)
			if err != nil || child.Parent != replacement[0].Name ||
				stream.OwnerOf(childID) != (stream.SessionOwner{Name: replacement[0].Name, Conversation: newID}) {
				t.Fatalf("the child did not follow the replacement: %+v, %+v, %v", child, stream.OwnerOf(childID), err)
			}
			if got := keptParent(5001, name); got != replacement[0].Name {
				t.Fatalf("the Claude child still stands under %s", got)
			}
			if got := keptParent(5002, name); got != "" {
				t.Fatalf("a detached Claude session was moved under %s", got)
			}
			if !strings.Contains(detail, "after this turn") || !strings.Contains(detail, "writable roots") ||
				!srv.Subscribed(old.ID) || !heldMark(t, old.ID) || len(srv.Calls("turn/interrupt")) != 0 {
				t.Fatalf("the calling turn was cut off or the limitation hidden: %s", detail)
			}
			if _, err := e.Execute(t.Context(), r); err == nil || len(srv.Calls("thread/start")) != 3 {
				t.Fatalf("a repeated restart started another replacement: %v", err)
			}
			srv.Complete(old.ID, "completed", "")
			waitFor(t, "the old thread to close after its turn", func() bool { return len(e.codex.Find(name)) == 0 })
			if srv.Subscribed(old.ID) || heldMark(t, old.ID) || len(srv.Calls("turn/interrupt")) != 0 {
				t.Fatal("the old thread was not let go without interrupting its turn")
			}
		})
	}
}

func TestACodexRestartFailureLeavesTheOldThreadAndChildrenAlone(t *testing.T) {
	for _, method := range []string{"thread/start", "turn/start"} {
		t.Run(method, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			newTmuxStub(t, nil, "")
			_, name, err := open(t, e, p, `{"agent":"codex","intent":"work"}`)
			if err != nil {
				t.Fatal(err)
			}
			old := e.codex.Find(name)[0]
			childID, err := old.Link.Start(t.Context(), codex.Begin{CWD: p.Path, Parent: name, Hold: true})
			if err != nil {
				t.Fatal(err)
			}
			srv.Refuse(method)
			_, err = e.Execute(t.Context(), action.Request{Kind: action.SessionRestart, Target: name, Project: p, Text: "continue"})
			if err == nil || !strings.Contains(err.Error(), "stays open") || !strings.Contains(err.Error(), method) {
				t.Fatalf("the startup failure has no cause: %v", err)
			}
			child, err := old.Link.RestartSettings(childID)
			if err != nil || child.Parent != name || len(e.codex.Find(name)) != 1 || old.Link.Closing(old.ID) ||
				!srv.Subscribed(old.ID) || !heldMark(t, old.ID) || len(srv.Calls("turn/interrupt")) != 0 {
				t.Fatalf("a failed restart touched the old thread or child: %+v, %v", child, err)
			}
			for _, raw := range srv.Calls("thread/unsubscribe") {
				id := paramsOf(t, raw)["threadId"].(string)
				if id == old.ID || heldMark(t, id) {
					t.Fatalf("the wrong thread was released: %s", raw)
				}
			}
		})
	}
}

func TestACodexRestartRefusesAnotherConversationAndResume(t *testing.T) {
	srv, e, p := onCodexContour(t)
	newTmuxStub(t, nil, "")
	_, name, err := open(t, e, p, `{"agent":"codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []action.Request{
		{Kind: action.SessionRestart, Target: name, Project: p, Conversation: streamSID},
		{Kind: action.SessionRestart, Target: name, Project: p, Resume: streamSID},
	} {
		if _, err := e.Execute(context.Background(), r); err == nil {
			t.Fatalf("a foreign conversation or resume was accepted: %+v", r)
		}
	}
	if len(srv.Calls("thread/start")) != 1 || len(srv.Calls("thread/unsubscribe")) != 0 {
		t.Fatal("a refused restart changed threads")
	}
}

func TestACodexRestartUsesTheMapMessageOrWaits(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		clean         bool
	}{{"map message", "Read the handoff", false}, {"empty", "", false}, {"clean", "Read the handoff", true}} {
		t.Run(tc.name, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			newTmuxStub(t, nil, "")
			_, name, err := open(t, e, p, `{"agent":"codex"}`)
			if err != nil {
				t.Fatal(err)
			}
			p.Launch, err = json.Marshal(map[string]any{"intent": tc.message})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Execute(t.Context(), action.Request{Kind: action.SessionRestart, Target: name, Project: p, Clean: tc.clean}); err != nil {
				t.Fatal(err)
			}
			turns := srv.Calls("turn/start")
			if tc.message != "" && !tc.clean {
				if len(turns) != 1 || !strings.Contains(string(turns[0]), tc.message) {
					t.Fatalf("the map's message did not arrive: %s", turns)
				}
			} else if len(turns) != 0 {
				t.Fatalf("an empty restart started a turn: %s", turns)
			}
			if len(srv.Calls("thread/name/set")) != 1 {
				t.Fatal("the replacement was not materialized for follow-ups")
			}
			waitFor(t, "the idle old thread to close", func() bool { return len(e.codex.Find(name)) == 0 })
		})
	}
}

func TestACodexRestartFallsBackToTheMapForUnknownSettings(t *testing.T) {
	srv, e := onCodex(t, nil)
	newTmuxStub(t, nil, "")
	p := &action.Project{Path: "/map/root", Launch: json.RawMessage(`{"codexModel":"gpt-map","codexEffort":"high","codexSandbox":"read-only","codexApproval":"untrusted"}`)}
	if _, err := e.Execute(t.Context(), action.Request{Kind: action.SessionRestart, Target: codexName, Project: p}); err != nil {
		t.Fatal(err)
	}
	starts := srv.Calls("thread/start")
	if len(starts) != 1 {
		t.Fatalf("the replacement did not start: %s", starts)
	}
	got := paramsOf(t, starts[0])
	config := got["config"].(map[string]any)
	if got["cwd"] != "/srv/proj" || got["model"] != "gpt-test" || got["sandbox"] != "read-only" ||
		got["approvalPolicy"] != "untrusted" || config["model_reasoning_effort"] != "high" {
		t.Fatalf("unknown settings did not fall back to the map: %v", got)
	}
}
