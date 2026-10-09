package executor

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/codex/codextest"
	registry "aacpanel/internal/contours"
)

const (
	codexThread = "019a1f00-0000-7000-8000-00000000abcd"
	codexName   = "codex-0000abcd"
)

// onCodex starts a fake codex daemon set up by setup and an executor linked
// to it, with no claude session alive. The daemon is set up before the link
// comes: the link reads its threads at once on connecting, and a request that
// waits then reaches it without the wait of a poll.
func onCodex(t *testing.T, setup func(*codextest.Server)) (*codextest.Server, *Executor) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	sessionFiles(t)
	srv := codextest.New(t)
	srv.Add(codextest.Thread{ID: codexThread, CWD: "/srv/proj", Model: "gpt-test", Created: 1791554348})
	if setup != nil {
		setup(srv)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil, "")
	e.codex = codex.Start(ctx, []registry.CodexHome{{Dir: srv.Home, Contour: "acme"}})
	t.Cleanup(func() {
		cancel()
		e.codex.Wait()
	})
	waitFor(t, "the executor to see the codex session", func() bool { return len(e.codex.Find(codexName)) == 1 })
	return srv, e
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func askedPermission(t *testing.T, e *Executor) *action.Permission {
	t.Helper()
	var d *action.Permission
	waitFor(t, "the request to reach the executor", func() bool {
		var err error
		d, err = e.Permission(context.Background(), codexName)
		if err != nil {
			t.Fatal(err)
		}
		return d != nil
	})
	return d
}

func decision(t *testing.T, a codextest.Answer) string {
	t.Helper()
	var body struct {
		Decision json.RawMessage `json:"decision"`
	}
	if err := json.Unmarshal(a.Result, &body); err != nil {
		t.Fatal(err)
	}
	return string(body.Decision)
}

func TestCodexPermissionOffersWhatTheDaemonOffersAndSendsThePickAsItIs(t *testing.T) {
	amend := `{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["touch","late.txt"]}}`
	var id int
	srv, e := onCodex(t, func(srv *codextest.Server) {
		id = srv.Ask(codexThread, "item/commandExecution/requestApproval", map[string]any{
			"turnId": "turn-x", "itemId": "exec-1", "command": "/bin/bash -lc 'touch late.txt'",
			"reason":             "it writes outside the sandbox",
			"availableDecisions": []any{"accept", json.RawMessage(amend), "cancel"}})
	})

	d := askedPermission(t, e)
	want := []action.PermOption{
		{N: 1, Text: "Yes"},
		{N: 2, Text: "Yes, and don't ask again for commands that start with `touch late.txt`", Lasting: true},
		{N: 3, Text: "No, and stop: tell Codex what to do differently"},
	}
	if len(d.Options) != len(want) {
		t.Fatalf("the options are %+v", d.Options)
	}
	for i := range want {
		if d.Options[i] != want[i] {
			t.Errorf("option %d is %+v, expected %+v", i+1, d.Options[i], want[i])
		}
	}
	if d.Tool != "Bash" || strings.Join(d.Action, "\n") != "/bin/bash -lc 'touch late.txt'" ||
		d.Fingerprint != "1" || len(d.Note) != 1 || !strings.Contains(d.Note[0], "outside the sandbox") {
		t.Errorf("the permission is %+v", d)
	}

	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionPermit, Target: codexName,
		Permit: &action.Permit{Option: 2, Fingerprint: "another"}}); err == nil ||
		!strings.Contains(err.Error(), "changed while you were looking") {
		t.Errorf("a press meant for another request: %v", err)
	}
	if len(srv.Answers()) != 0 {
		t.Fatal("a press meant for another request answered this one")
	}

	detail, err := e.Execute(context.Background(), action.Request{Kind: action.SessionPermit, Target: codexName,
		Permit: &action.Permit{Option: 2, Fingerprint: d.Fingerprint}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if a := srv.Answers()[0]; a.ID != id || decision(t, a) != amend {
		t.Errorf("the daemon got %d %s, expected %d with %s", a.ID, a.Result, id, amend)
	}
	if !strings.Contains(detail, "don't ask again") {
		t.Errorf("the answer says %q", detail)
	}
}

func TestCodexFileChangeShowsTheChangeTheItemHolds(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) {
		srv.Item(codexThread, "turn-x", map[string]any{"type": "fileChange", "id": "patch-1", "status": "inProgress",
			"changes": []any{map[string]any{"path": "/srv/proj/main.go", "kind": map[string]any{"type": "update"},
				"diff": "@@ -1 +1 @@\n-old line\n+new line\n"}}})
		srv.Ask(codexThread, "item/fileChange/requestApproval", map[string]any{"turnId": "turn-x", "itemId": "patch-1"})
	})

	d := askedPermission(t, e)
	if d.Tool != "Edit" || strings.Join(d.Action, "|") != "/srv/proj/main.go|@@ -1 +1 @@|-old line|+new line" {
		t.Errorf("the change reads %q as %s", d.Action, d.Tool)
	}
	var texts []string
	for _, o := range d.Options {
		texts = append(texts, o.Text)
	}
	if strings.Join(texts, "|") != "Yes|Yes, and don't ask again this session|No, go on without it|"+
		"No, and stop: tell Codex what to do differently" {
		t.Errorf("a request that names no decisions offers %q", texts)
	}

	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionEscape, Target: codexName}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if got := decision(t, srv.Answers()[0]); got != `"decline"` {
		t.Errorf("escape answered %s, expected a decline", got)
	}
}

func TestCodexEscapeCancelsWhereNoDeclineIsOffered(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) {
		srv.Ask(codexThread, "item/commandExecution/requestApproval", map[string]any{
			"turnId": "turn-x", "itemId": "exec-1", "command": "make", "availableDecisions": []any{"accept", "cancel"}})
	})
	askedPermission(t, e)
	if _, err := e.Execute(context.Background(), action.Request{Kind: action.SessionEscape, Target: codexName}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answer to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	if got := decision(t, srv.Answers()[0]); got != `"cancel"` {
		t.Errorf("escape answered %s, expected a cancel", got)
	}
}

func TestCodexSendAndStopGoThroughTheDaemon(t *testing.T) {
	srv, e := onCodex(t, nil)
	ctx := context.Background()

	detail, err := e.Execute(ctx, action.Request{Kind: action.SessionSend, Target: codexName, Text: "run the tests"})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.Calls("turn/start")) != 1 || !strings.Contains(detail, "a turn started") {
		t.Fatalf("a message to a free session: %q, turn/start %s", detail, srv.Calls("turn/start"))
	}
	detail, err = e.Execute(ctx, action.Request{Kind: action.SessionSend, Target: codexName, Text: "and the linter"})
	if err != nil {
		t.Fatal(err)
	}
	steers := srv.Calls("turn/steer")
	if len(steers) != 1 || !strings.Contains(string(steers[0]), `"expectedTurnId":"turn-1"`) ||
		!strings.Contains(detail, "the turn that runs") {
		t.Fatalf("a message to a busy session: %q, turn/steer %s", detail, steers)
	}

	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionStop, Target: codexName}); err != nil {
		t.Fatal(err)
	}
	stops := srv.Calls("turn/interrupt")
	if len(stops) != 1 || !strings.Contains(string(stops[0]), `"turnId":"turn-1"`) {
		t.Errorf("turn/interrupt went as %s", stops)
	}
}

func TestEveryOtherActionOnACodexSessionIsRefused(t *testing.T) {
	_, e := onCodex(t, nil)
	ctx := context.Background()
	taken := map[action.Kind]bool{action.SessionSend: true, action.SessionStop: true, action.SessionEscape: true,
		action.SessionPermit: true}
	for _, k := range action.Kinds {
		if !sessionTarget(k) || taken[k] {
			continue
		}
		_, err := e.Execute(ctx, action.Request{Kind: k, Target: codexName})
		if err == nil || !strings.Contains(err.Error(), codexName+" is a codex session") {
			t.Errorf("%s on a codex session: %v", k, err)
		}
	}

	asks := map[string]func() error{
		"window":   func() error { _, err := e.Window(ctx, codexName); return err },
		"models":   func() error { _, err := e.Models(ctx, codexName); return err },
		"mcp":      func() error { _, err := e.Mcp(ctx, codexName); return err },
		"status":   func() error { _, err := e.Status(ctx, codexName); return err },
		"commands": func() error { _, err := e.Commands(ctx, codexName); return err },
		"side":     func() error { _, err := e.Side(ctx, codexName, "why?", nil); return err },
		"setup":    func() error { _, err := e.Setup(ctx, codexName, action.SetupAgents); return err },
	}
	for name, ask := range asks {
		if err := ask(); err == nil || !strings.Contains(err.Error(), "is a codex session") {
			t.Errorf("the %s question about a codex session: %v", name, err)
		}
	}
}

func TestAClaudeSessionOfTheSameNameWins(t *testing.T) {
	_, e := onCodex(t, nil)
	procFS(t, fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/srv/proj", start: "5555", args: []string{"claude"}})
	sessionFiles(t, fakeSession{pid: 5001, name: codexName, start: "5555"})

	th, err := e.codexSession(codexName)
	if err != nil || th != nil {
		t.Errorf("a live claude session is named %s, and the action went to codex: %+v, %v", codexName, th, err)
	}
}

// The models codex offers come from the daemon's own catalogue, every page of
// it, each with the efforts it takes and the one it starts at; a model codex
// keeps out of its picker stays out. The question names no session and asks
// no thread: a launch parameter is picked before any session runs.
func TestCodexModelsAreTheDaemonsCatalogue(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) {
		srv.Catalogue(
			codextest.Model{ID: "gpt-5.5-codex", Model: "gpt-5.5-codex", Name: "GPT-5.5 Codex",
				Efforts: []string{"low", "medium", "high", "xhigh"}, Default: "medium"},
			codextest.Model{ID: "gpt-5.5", Model: "gpt-5.5", Name: "GPT-5.5", Efforts: []string{"minimal", "low", "high"}, Default: "low"},
			codextest.Model{ID: "gpt-old", Model: "gpt-old", Name: "GPT old", Efforts: []string{"medium"}, Hidden: true},
			codextest.Model{ID: "gpt-5.5-mini", Model: "gpt-5.5-mini", Name: "GPT-5.5 mini", Efforts: []string{"medium"}, Default: "medium"},
		)
	})
	got, err := e.CodexModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []action.CodexModel{
		{Model: "gpt-5.5-codex", Name: "GPT-5.5 Codex", Efforts: []string{"low", "medium", "high", "xhigh"}, Effort: "medium"},
		{Model: "gpt-5.5", Name: "GPT-5.5", Efforts: []string{"minimal", "low", "high"}, Effort: "low"},
		{Model: "gpt-5.5-mini", Name: "GPT-5.5 mini", Efforts: []string{"medium"}, Effort: "medium"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the models are\n%+v\nmeant\n%+v", got, want)
	}
	if n := len(srv.Calls("model/list")); n != 2 {
		t.Errorf("model/list was asked %d times — the catalogue is two pages", n)
	}
	if n := len(srv.Calls("thread/resume")) + len(srv.Calls("turn/start")); n != 0 {
		t.Errorf("listing the models touched a thread %d times", n)
	}
}
