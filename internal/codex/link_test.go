package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/stream"
)

func TestMain(m *testing.M) {
	pollEvery = 20 * time.Millisecond
	redialEvery = 50 * time.Millisecond
	callWait = 3 * time.Second
	os.Exit(m.Run())
}

const (
	threadA = "019a1f00-0000-7000-8000-00000000abcd"
	threadB = "019a1f00-0000-7000-8000-0000000000ef"
)

// linked starts a fake daemon with the thread on it and a link to it. The
// state files go to a runtime directory of the test's own.
func linked(t *testing.T, threads ...codextest.Thread) (*codextest.Server, *Link) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	for _, th := range threads {
		srv.Add(th)
	}
	l := NewLink(srv.Home, "acme")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return srv, l
}

func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func stateOf(t *testing.T, id string) (State, []byte, bool) {
	t.Helper()
	raw, err := os.ReadFile(stream.StatePath(id))
	if err != nil {
		return State{}, nil, false
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("the state file of %s does not read: %v\n%s", id, err, raw)
	}
	return st, raw, true
}

func written(t *testing.T, id string) func() bool {
	return func() bool {
		_, _, ok := stateOf(t, id)
		return ok
	}
}

func idle(id string) codextest.Thread {
	return codextest.Thread{ID: id, CWD: "/srv/proj", Path: "/srv/codex/sessions/rollout-" + id + ".jsonl",
		Model: "gpt-test", Effort: "low", Created: 1791554348}
}

func TestPollWritesTheStateOfAThreadAndNothingOfTheConversation(t *testing.T) {
	sub := idle(threadB)
	sub.Parent = threadA
	srv, _ := linked(t, idle(threadA), sub)
	until(t, "the state file", written(t, threadA))

	st, raw, _ := stateOf(t, threadA)
	if st.Agent != "codex" || st.Name != "codex-0000abcd" || st.SessionID != threadA || st.Holder != os.Getpid() ||
		st.PID != 0 || st.Busy || st.CWD != "/srv/proj" || st.Contour != "acme" || st.CodexHome != srv.Home ||
		st.Transcript != "/srv/codex/sessions/rollout-"+threadA+".jsonl" || st.Model != "gpt-test" ||
		st.Effort != "low" || !st.Started.Equal(time.Unix(1791554348, 0)) || st.Protocol != stream.Protocol {
		t.Errorf("the state file says %s", raw)
	}
	if !strings.Contains(string(raw), `"waiting":[]`) {
		t.Errorf("a thread that waits on nothing has to say so with an empty list: %s", raw)
	}
	for _, words := range []string{"the first words", "a title made of"} {
		if strings.Contains(string(raw), words) {
			t.Errorf("the state file carries the conversation (%q): %s", words, raw)
		}
	}
	time.Sleep(5 * pollEvery)
	if _, _, ok := stateOf(t, threadB); ok {
		t.Error("the thread of a subagent is on the map as a session of its own")
	}
	if srv.Subscribed(threadA) || len(srv.Calls("thread/resume")) > 0 {
		t.Error("the link subscribed to a thread that waits on nothing: the daemon would never unload it")
	}

	hello := srv.Calls("initialize")
	if len(hello) != 1 || len(srv.Calls("initialized")) != 1 {
		t.Fatalf("the handshake: %d initialize, %d initialized", len(hello), len(srv.Calls("initialized")))
	}
	var params struct {
		ClientInfo   struct{ Name string } `json:"clientInfo"`
		Capabilities struct {
			Experimental bool     `json:"experimentalApi"`
			Attestation  bool     `json:"requestAttestation"`
			Quiet        []string `json:"optOutNotificationMethods"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(hello[0], &params); err != nil {
		t.Fatal(err)
	}
	if params.ClientInfo.Name != "aacpanel" || !params.Capabilities.Experimental || params.Capabilities.Attestation {
		t.Errorf("initialize went with %s", hello[0])
	}
	for _, m := range []string{"item/completed", "item/agentMessage/delta", "turn/completed", "turn/diff/updated",
		"turn/plan/updated", "item/reasoning/textDelta", "thread/started"} {
		if !slices.Contains(params.Capabilities.Quiet, m) {
			t.Errorf("%s carries the conversation and is not turned off", m)
		}
	}
	if slices.Contains(params.Capabilities.Quiet, "serverRequest/resolved") {
		t.Error("the word that a request was answered is turned off: a request answered in the TUI would wait on the phone")
	}
	for m, why := range map[string]string{
		"thread/status/changed":      "a thread that turns free would wait a round for the next message of the queue",
		"thread/settings/updated":    "a model or a mode changed in the TUI would not reach the panel",
		"thread/tokenUsage/updated":  "the fill of the context would wait for the rollout",
		"account/rateLimits/updated": "the limits would wait for the next read",
	} {
		if slices.Contains(params.Capabilities.Quiet, m) {
			t.Errorf("%s is turned off: %s", m, why)
		}
	}
}

func TestAnApprovalReachesThePanelAndTakesTheDecisionAsItIs(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))

	decisions := []any{"accept", map[string]any{"acceptWithExecpolicyAmendment": map[string]any{
		"execpolicy_amendment": []string{"touch", "late.txt"}}}, "cancel"}
	id := srv.Ask(threadA, "item/commandExecution/requestApproval", map[string]any{
		"turnId": "turn-x", "itemId": "exec-1", "command": "/bin/bash -lc 'touch late.txt'",
		"availableDecisions": decisions})
	until(t, "the request to reach the link", func() bool { return len(l.Pending(threadA)) == 1 })
	if !srv.Subscribed(threadA) {
		t.Error("the request reached the link without a subscription")
	}
	until(t, "the state to say what waits", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Busy && slices.Equal(st.Waiting, []string{"Bash"})
	})

	r := l.Pending(threadA)[0]
	if r.Command != "/bin/bash -lc 'touch late.txt'" || r.TurnID != "turn-x" || len(r.Decisions()) != 3 {
		t.Errorf("the request is %+v", r)
	}
	chosen := r.Decisions()[1]
	if err := l.Respond(context.Background(), threadA, r.Key(), chosen); err != nil {
		t.Fatal(err)
	}
	until(t, "the answer to reach the daemon", func() bool { return len(srv.Answers()) == 1 })
	answer := srv.Answers()[0]
	var got struct {
		Decision json.RawMessage `json:"decision"`
	}
	_ = json.Unmarshal(answer.Result, &got)
	if answer.ID != id || string(got.Decision) != string(chosen) {
		t.Errorf("the daemon got %d %s, expected %d with the decision %s", answer.ID, answer.Result, id, chosen)
	}
	if err := l.Respond(context.Background(), threadA, r.Key(), chosen); err != ErrAnswered {
		t.Errorf("a second answer to the same request: %v", err)
	}

	srv.Set(threadA, "idle")
	until(t, "the link to leave the free thread", func() bool { return !srv.Subscribed(threadA) })
	until(t, "the state to wait on nothing", func() bool {
		st, _, _ := stateOf(t, threadA)
		return !st.Busy && len(st.Waiting) == 0
	})
}

func TestAnApprovalAnsweredElsewhereLeavesThePanel(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	id := srv.Ask(threadA, "item/fileChange/requestApproval", map[string]any{"turnId": "turn-x", "itemId": "patch-1"})
	until(t, "the request to reach the link", func() bool { return len(l.Pending(threadA)) == 1 })
	if r := l.Pending(threadA)[0]; !r.FileChange() || r.Tool() != "Edit" || len(r.Decisions()) != 4 {
		t.Errorf("a change to files is %+v with %d decisions", r, len(r.Decisions()))
	}

	srv.Resolve(id)
	until(t, "the request to go", func() bool { return len(l.Pending(threadA)) == 0 })
	until(t, "the state to wait on nothing", func() bool {
		st, _, _ := stateOf(t, threadA)
		return len(st.Waiting) == 0
	})
}

// A message to a free thread starts a turn. A message to a busy one waits in
// the panel's queue — the turn that runs takes nothing of it — and goes as a
// turn of its own once the thread is free, one message a turn, in the order
// they came. The link stays a client of the thread while anything waits.
func TestSendStartsATurnOnAFreeThreadAndQueuesForABusyOne(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()

	place, err := l.Send(ctx, threadA, Message{Text: "look at the tests"})
	if err != nil || place != 0 {
		t.Fatalf("a message to a free thread: place %d, %v", place, err)
	}
	starts := srv.Calls("turn/start")
	if len(starts) != 1 || !strings.Contains(string(starts[0]), `"text":"look at the tests"`) {
		t.Fatalf("turn/start went as %s", starts)
	}

	for i, text := range []string{"and the docs", "and the linter"} {
		place, err := l.Send(ctx, threadA, Message{Text: text, ID: "id-" + text})
		if err != nil || place != i+1 {
			t.Fatalf("a message to a busy thread: place %d, %v", place, err)
		}
	}
	if n := len(srv.Calls("turn/start")); n != 1 || len(srv.Calls("turn/steer")) != 0 {
		t.Fatalf("a message to a busy thread went at once: %d turn/start, %s", n, srv.Calls("turn/steer"))
	}
	until(t, "the state to count the queue", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Queue == 2
	})
	if _, raw, _ := stateOf(t, threadA); strings.Contains(string(raw), "the docs") {
		t.Errorf("the state file carries the words of a queued message: %s", raw)
	}

	srv.Set(threadA, "idle")
	until(t, "the first message of the queue to go", func() bool { return len(srv.Calls("turn/start")) == 2 })
	if second := string(srv.Calls("turn/start")[1]); !strings.Contains(second, `"text":"and the docs"`) ||
		!strings.Contains(second, `"clientUserMessageId":"id-and the docs"`) {
		t.Errorf("the queue sent %s first", second)
	}
	time.Sleep(5 * pollEvery)
	if n := len(srv.Calls("turn/start")); n != 2 {
		t.Fatalf("the queue sent %d turns into one free moment", n-1)
	}
	until(t, "the state to count one", func() bool {
		st, _, _ := stateOf(t, threadA)
		return st.Queue == 1
	})
	srv.Set(threadA, "idle")
	until(t, "the second message of the queue to go", func() bool { return len(srv.Calls("turn/start")) == 3 })
	if !srv.Subscribed(threadA) {
		t.Error("the link left the thread while the turn of its queue runs")
	}
	srv.Set(threadA, "idle")
	until(t, "the link to leave the thread once it is free", func() bool { return !srv.Subscribed(threadA) })
	if len(srv.Calls("thread/unsubscribe")) == 0 {
		t.Error("the link left without saying so")
	}
}

// A message for a thread whose turn another client runs waits for that turn
// to end; the link joins the thread meanwhile, or it would not hear the end.
func TestAMessageForATurnOfAnotherClientWaitsForItsEnd(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Running(threadA, "turn-tui")

	place, err := l.Send(context.Background(), threadA, Message{Text: "one more thing"})
	if err != nil || place != 1 {
		t.Fatalf("place %d, %v", place, err)
	}
	until(t, "the link to subscribe while the message waits", func() bool { return srv.Subscribed(threadA) })
	if len(srv.Calls("turn/start")) != 0 {
		t.Fatal("the message went into the turn of another client")
	}
	srv.Set(threadA, "idle")
	until(t, "the message to go once the turn ended", func() bool { return len(srv.Calls("turn/start")) == 1 })
	srv.Set(threadA, "idle")
	until(t, "the link to leave the free thread", func() bool { return !srv.Subscribed(threadA) })
}

func TestInterruptStopsTheRunningTurnOnly(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()

	if stopped, err := l.Interrupt(ctx, threadA); err != nil || stopped {
		t.Fatalf("a free thread: stopped %v, %v", stopped, err)
	}
	if len(srv.Calls("turn/interrupt")) != 0 {
		t.Fatal("a free thread was interrupted")
	}
	srv.Running(threadA, "turn-tui")
	if stopped, err := l.Interrupt(ctx, threadA); err != nil || !stopped {
		t.Fatalf("a busy thread: stopped %v, %v", stopped, err)
	}
	calls := srv.Calls("turn/interrupt")
	if len(calls) != 1 || !strings.Contains(string(calls[0]), `"turnId":"turn-tui"`) {
		t.Errorf("turn/interrupt went as %s", calls)
	}
}

func TestAnUnloadedThreadLeavesTheMap(t *testing.T) {
	srv, _ := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Unload(threadA)
	until(t, "the state file to go", func() bool { return !written(t, threadA)() })
}

func TestAWaitingThreadIsJoinedAgainWhateverTheLinkBelieves(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	if _, err := l.Send(context.Background(), threadA, Message{Text: "go on"}); err != nil {
		t.Fatal(err)
	}
	until(t, "the link to follow the turn it started", func() bool { return srv.Subscribed(threadA) })
	srv.Drop(threadA)
	srv.Ask(threadA, "item/commandExecution/requestApproval", map[string]any{"command": "ls"})
	until(t, "the waiting request to reach the panel", func() bool { return len(l.Pending(threadA)) == 1 })
}

func TestADaemonOfADeepHomeIsDialledWhereItsSocketLies(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	srv.Add(idle(threadA))
	home := filepath.Join(t.TempDir(), strings.Repeat("deep/", 20), ".codex-profiles", "acme")
	sock := SocketPath(home)
	if len(sock) <= 108 {
		t.Fatalf("the home is not deep enough to pass the address of a socket: %d bytes", len(sock))
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(srv.Socket(), sock); err != nil {
		t.Fatal(err)
	}
	l := NewLink(home, "acme")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	until(t, "the state file of the deep home's thread", written(t, threadA))
}

func TestTheLinkComesBackWithTheDaemon(t *testing.T) {
	srv, _ := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Close()
	until(t, "the state file to go with the daemon", func() bool { return !written(t, threadA)() })
	srv.Start(t)
	until(t, "the state file to come back with the daemon", written(t, threadA))
}

func TestSweepRemovesOnlyTheFilesOfADeadExecutor(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	dead := gone.Process.Pid
	files := map[string]map[string]any{
		"dead-codex":  {"agent": "codex", "holder": dead},
		"live-codex":  {"agent": "codex", "holder": os.Getpid()},
		"dead-claude": {"sessionId": "dead-claude", "holder": dead},
	}
	if err := os.MkdirAll(stream.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		raw, _ := json.Marshal(body)
		if err := os.WriteFile(filepath.Join(stream.Dir(), name+".json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	Start(context.Background(), nil)

	for name, keep := range map[string]bool{"dead-codex": false, "live-codex": true, "dead-claude": true} {
		_, err := os.Stat(filepath.Join(stream.Dir(), name+".json"))
		if (err == nil) != keep {
			t.Errorf("%s: kept %v, expected %v", name, err == nil, keep)
		}
	}
}

func TestSessionNameTakesTheTailOfTheID(t *testing.T) {
	if got := SessionName("01a120f5-e6eb-7b20-95e9-e4adc7c8da30"); got != "codex-c7c8da30" {
		t.Errorf("the name is %s", got)
	}
}
