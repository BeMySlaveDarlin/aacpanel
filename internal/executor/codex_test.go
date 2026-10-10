package executor

import (
	"context"
	"encoding/json"
	"os"
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sessionFiles(t)
	srv := codextest.New(t)
	srv.Add(codextest.Thread{ID: codexThread, CWD: "/srv/proj", Model: "gpt-test", Created: 1791554348})
	if setup != nil {
		setup(srv)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := New(nil, "")
	e.codex = codex.Start(ctx, []registry.CodexHome{{Dir: srv.Home, Contour: "acme"}}, nil)
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
	if len(srv.Calls("turn/start")) != 1 || len(srv.Calls("turn/steer")) != 0 ||
		!strings.Contains(detail, "waits in the panel's queue, first") {
		t.Fatalf("a message to a busy session: %q, turn/start %s", detail, srv.Calls("turn/start"))
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
		action.SessionPermit: true, action.SessionClose: true, action.SessionSet: true, action.SessionUnqueue: true,
		action.SessionFile: true, action.SessionAnswer: true, action.SessionDismiss: true, action.SessionCommand: true,
		action.SessionRename: true, action.TaskStop: true}
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
	got, err := e.CodexModels(context.Background(), "")
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

// The modes a request names for codex are the ones the link knows how to set.
func TestTheCodexModesOfAnActionAreTheLinks(t *testing.T) {
	if !reflect.DeepEqual(action.CodexModes, codex.Modes) {
		t.Errorf("an action offers %v, and the link sets %v", action.CodexModes, codex.Modes)
	}
}

func codexCatalogue(srv *codextest.Server) {
	srv.Catalogue(
		codextest.Model{ID: "gpt-test", Model: "gpt-test", Name: "GPT test", Efforts: []string{"low", "high"}, Default: "low"},
		codextest.Model{ID: "gpt-other", Model: "gpt-other", Name: "GPT other", Efforts: []string{"medium", "ultra"},
			Default: "medium"},
	)
}

func set(s action.Setting) action.Request {
	return action.Request{Kind: action.SessionSet, Target: codexName, Setting: &s}
}

// A model, an effort, a mode and the plan of a codex session change through
// its daemon, checked against the daemon's catalogue first; what codex has no
// way for is refused with the reason.
func TestCodexSettingsGoThroughTheDaemon(t *testing.T) {
	srv, e := onCodex(t, codexCatalogue)
	ctx := context.Background()
	on := true

	detail, err := e.Execute(ctx, set(action.Setting{Model: "gpt-other", Effort: "ultra"}))
	if err != nil || detail != codexName+" runs gpt-other at the ultra effort from its next turn" {
		t.Fatalf("a model with its effort: %q, %v", detail, err)
	}
	for _, c := range []struct {
		set  action.Setting
		says string
	}{
		{action.Setting{Model: "gpt-z"}, "codex offers no model"},
		{action.Setting{Effort: "high"}, `gpt-other does not take the effort "high"`},
		{action.Setting{Mode: "acceptEdits"}, "it has no acceptEdits mode; its modes are read-only, ask, auto"},
		{action.Setting{Model: "gpt-test", Scope: action.ScopeDefault}, "config.toml"},
	} {
		if _, err := e.Execute(ctx, set(c.set)); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%+v: %v, meant to say %q", c.set, err, c.says)
		}
	}
	if detail, err := e.Execute(ctx, set(action.Setting{Mode: "auto"})); err != nil || !strings.Contains(detail, "the auto mode") {
		t.Errorf("the auto mode: %q, %v", detail, err)
	}
	if detail, err := e.Execute(ctx, set(action.Setting{Plan: &on})); err != nil || !strings.Contains(detail, "plans") {
		t.Errorf("plan mode: %q, %v", detail, err)
	}
	if th := srv.Settings(codexThread); th.Model != "gpt-other" || th.Effort != "ultra" || !th.Plan ||
		th.Reviewer != "auto_review" || th.Profile != ":workspace" {
		t.Errorf("the thread runs with %+v", th)
	}
	if n := len(srv.Calls("thread/settings/update")); n != 3 {
		t.Errorf("thread/settings/update went %d times for three changes", n)
	}

	srv.Refuse("thread/settings/update")
	detail, err = e.Execute(ctx, set(action.Setting{Model: "gpt-test"}))
	if err != nil || !strings.Contains(detail, "goes with the next message") || !strings.Contains(detail, "gpt-test at the low effort") {
		t.Errorf("a model on a daemon that does not change a running thread: %q, %v", detail, err)
	}
	if _, err := e.Execute(ctx, set(action.Setting{Mode: "ask"})); err == nil || !strings.Contains(err.Error(), "unknown variant") {
		t.Errorf("a mode on a daemon that does not change a running thread: %v", err)
	}
}

// A message for a busy codex session waits in the panel's queue and is taken
// back from there; one that went is a turn already.
func TestCodexUnqueueTakesBackWhatWaits(t *testing.T) {
	srv, e := onCodex(t, func(srv *codextest.Server) { srv.Running(codexThread, "turn-tui") })
	ctx := context.Background()
	id := "8b0c6a52-9f1e-4d39-a2ad-5b5e6f7f0a11"
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionSend, Target: codexName, Text: "later",
		MessageID: id}); err != nil {
		t.Fatal(err)
	}
	detail, err := e.Execute(ctx, action.Request{Kind: action.SessionUnqueue, Target: codexName, MessageID: id})
	if err != nil || !strings.Contains(detail, "taken back") {
		t.Fatalf("taking back: %q, %v", detail, err)
	}
	if _, err := e.Execute(ctx, action.Request{Kind: action.SessionUnqueue, Target: codexName, MessageID: id}); err == nil ||
		!strings.Contains(err.Error(), "already delivered") {
		t.Errorf("taking back twice: %v", err)
	}
	srv.Set(codexThread, "idle")
	time.Sleep(200 * time.Millisecond)
	if n := len(srv.Calls("turn/start")); n != 0 {
		t.Errorf("a message taken back went as %d turns", n)
	}
}

// A picture goes into the turn as a file codex reads itself, any other file
// as its path after the caption; both are where a claude session gets them.
func TestCodexFileSendsPicturesAsImagesAndFilesAsPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(filesEnv, dir)
	srv, e := onCodex(t, nil)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	detail, err := e.Execute(context.Background(), action.Request{Kind: action.SessionFile, Target: codexName,
		Text: "look at both", Files: []action.File{{Name: "shot.png", Data: png}, {Name: "notes.txt", Data: []byte("words")}}})
	if err != nil {
		t.Fatal(err)
	}
	starts := srv.Calls("turn/start")
	if len(starts) != 1 {
		t.Fatalf("turn/start went %d times", len(starts))
	}
	var p struct {
		Input []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Path string `json:"path"`
		} `json:"input"`
	}
	if err := json.Unmarshal(starts[0], &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Input) != 2 || p.Input[0].Type != "text" || p.Input[1].Type != "localImage" ||
		!strings.HasPrefix(p.Input[0].Text, "look at both\n"+dir+"/") || !strings.HasSuffix(p.Input[0].Text, "-notes.txt") ||
		!strings.HasPrefix(p.Input[1].Path, dir+"/") || !strings.HasSuffix(p.Input[1].Path, "-shot.png") {
		t.Errorf("the turn went with %+v", p.Input)
	}
	for _, path := range []string{p.Input[1].Path, strings.Split(p.Input[0].Text, "\n")[1]} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a file the turn names is not there: %v", err)
		}
	}
	if !strings.Contains(detail, "2 files") {
		t.Errorf("the answer says %q", detail)
	}
}

// What only codex takes is refused for claude before the session is looked
// for.
func TestClaudeRefusesWhatOnlyCodexTakes(t *testing.T) {
	e := New(nil, "")
	on := true
	for _, c := range []struct {
		set  action.Setting
		says string
	}{
		{action.Setting{Plan: &on}, "no plan switch"},
		{action.Setting{Model: "sonnet", Effort: "high"}, "one setting at a time"},
		{action.Setting{Model: "gpt-6.1-sol"}, `no model "gpt-6.1-sol"`},
		{action.Setting{Effort: "ultra"}, `no effort "ultra"`},
	} {
		if _, err := e.sessionSet(context.Background(), "aacpanel", &c.set); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%+v for claude: %v", c.set, err)
		}
	}
}
