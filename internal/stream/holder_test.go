package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The test binary plays claude when asked to: it speaks just enough of the
// stream protocol and writes every line it received to a file, so a test can
// see what reached claude and not only what the holder says it sent.
func TestMain(m *testing.M) {
	if os.Getenv("STREAM_FAKE_CLAUDE") == "1" {
		os.Exit(fakeClaude())
	}
	os.Exit(m.Run())
}

func fakeClaude() int {
	if os.Getenv("FAKE_DIE") == "1" {
		fmt.Fprintln(os.Stderr, "boom: not signed in")
		return 3
	}
	logf, _ := os.OpenFile(os.Getenv("FAKE_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	out := func(obj any) {
		b, _ := json.Marshal(obj)
		fmt.Println(string(b))
	}
	result := func() { out(map[string]any{"type": "result", "subtype": "success", "result": "done"}) }
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	var held []map[string]any
	model, ultra := "claude-sonnet-5", false
	for in.Scan() {
		line := in.Bytes()
		fmt.Fprintln(logf, string(line))
		var msg map[string]any
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch msg["type"] {
		case "control_request":
			req := msg["request"].(map[string]any)
			body := map[string]any{}
			switch req["subtype"] {
			case "initialize":
				body = map[string]any{"commands": []any{map[string]any{"name": "context"}},
					"models": []any{map[string]any{"value": "haiku"}}, "current_permission_mode": "default"}
			case "interrupt":
				body = map[string]any{"still_queued": []any{}}
			case "set_model":
				model, _ = req["model"].(string)
				out(map[string]any{"type": "system", "subtype": "init", "model": req["model"]})
			case "apply_flag_settings":
				// Claude answers success either way, and a model without
				// xhigh turns nothing on.
				on, _ := req["settings"].(map[string]any)["ultracode"].(bool)
				ultra = on && model != "haiku"
			case "get_settings":
				body = map[string]any{
					"effective": map[string]any{"env": map[string]any{"GITLAB_TOKEN": "glpat-secret"}},
					"sources":   []any{map[string]any{"source": "userSettings", "settings": map[string]any{"env": "glpat-secret"}}},
					"applied":   map[string]any{"model": model, "effort": "xhigh", "advisor": nil, "ultracode": ultra},
				}
			case "cancel_async_message":
				found := false
				for i, m := range held {
					if m["uuid"] == req["message_uuid"] {
						held = append(held[:i], held[i+1:]...)
						found = true
						break
					}
				}
				body = map[string]any{"cancelled": found}
			}
			out(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": msg["request_id"], "response": body}})
		case "control_response":
			out(map[string]any{"type": "user", "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_result", "content": "answered"}}}})
			result()
		case "user":
			text := msg["message"].(map[string]any)["content"].(string)
			switch text {
			case "slow":
				held = append(held, msg)
				continue
			case "ask":
				out(map[string]any{"type": "control_request", "request_id": "cc-1", "request": map[string]any{
					"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "tool_use_id": "toolu_1",
					"input": map[string]any{"questions": []any{map[string]any{"question": "Q"}}}}})
				continue
			case "hook":
				out(map[string]any{"type": "control_request", "request_id": "cc-2", "request": map[string]any{
					"subtype": "hook_callback", "callback_id": "x"}})
			case "tasks":
				out(map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{
					map[string]any{"task_id": "b1", "task_type": "local_bash", "description": "a sleep"}}})
			case "compact", "compact-again", "mode-mid-compact", "compact-done", "boundary":
				switch text {
				case "compact", "compact-again":
					out(map[string]any{"type": "system", "subtype": "status", "status": "compacting"})
				case "mode-mid-compact":
					out(map[string]any{"type": "system", "subtype": "status", "status": nil, "permissionMode": "plan"})
				case "compact-done":
					out(map[string]any{"type": "system", "subtype": "status", "status": nil, "compact_result": "success"})
				case "boundary":
					out(map[string]any{"type": "system", "subtype": "compact_boundary",
						"compact_metadata": map[string]any{"trigger": "manual"}})
				}
				// The step is over once the holder has read this: the list of
				// tasks names the step, and the holder reads in order.
				out(map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{
					map[string]any{"task_id": text}}})
				continue
			}
			for _, h := range append(held, msg) {
				out(map[string]any{"type": "user", "uuid": h["uuid"], "message": h["message"]})
			}
			held = nil
			out(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "ok"}}}})
			result()
		}
	}
	return 0
}

type rig struct {
	t    *testing.T
	spec Spec
	log  string
	done chan error
	stop context.CancelFunc
}

func start(t *testing.T, mutate func(*Spec)) *rig {
	t.Helper()
	// Short on purpose: a unix socket path longer than 108 bytes is refused,
	// and a test's own temporary directory is longer than that.
	run, err := os.MkdirTemp("/tmp", "hold")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run) })
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("XDG_STATE_HOME", run+"/state")
	t.Setenv("STREAM_FAKE_CLAUDE", "1")
	logPath := run + "/claude.log"
	t.Setenv("FAKE_LOG", logPath)

	spec := Spec{Name: "demo", Dir: run, SessionID: NewSessionID(), Argv: []string{os.Args[0], "-test.run=^$"}}
	if mutate != nil {
		mutate(&spec)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{t: t, spec: spec, log: logPath, done: make(chan error, 1), stop: cancel}
	go func() { r.done <- Run(ctx, spec) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
			t.Error("the holder did not end after its context was cancelled")
		}
	})
	return r
}

func (r *rig) ask(req Request) Reply {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var last error
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		reply, err := Ask(ctx, r.spec.SessionID, req)
		if err == nil {
			return reply
		}
		last = err
	}
	r.t.Fatalf("the holder never answered %s: %v", req.Op, last)
	return Reply{}
}

func (r *rig) state() State {
	r.t.Helper()
	reply := r.ask(Request{Op: OpState})
	if !reply.OK || reply.State == nil {
		r.t.Fatalf("no state: %+v", reply)
	}
	return *reply.State
}

func (r *rig) waitFor(what string, ok func(State) bool) State {
	r.t.Helper()
	var s State
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if s = r.state(); ok(s) {
			return s
		}
	}
	r.t.Fatalf("%s never happened; the state is %+v", what, s)
	return s
}

func (r *rig) summary(ok func(Summary) bool) Summary {
	r.t.Helper()
	var sum Summary
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if got, held := Held(r.spec.SessionID, 0); held && ok(got) {
			return got
		}
		sum, _ = Held(r.spec.SessionID, 0)
	}
	r.t.Fatalf("the state file never caught up: %+v", sum)
	return sum
}

// eventually waits for claude to have read a line carrying what is looked
// for: the holder is done once it has written, claude reads when it gets to it.
func (r *rig) eventually(what string) string {
	r.t.Helper()
	var got string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if got = r.received(); strings.Contains(got, what) {
			return got
		}
	}
	r.t.Fatalf("claude never read %s:\n%s", what, got)
	return got
}

func (r *rig) received() string {
	b, _ := os.ReadFile(r.log)
	return string(b)
}

func TestTheHandshakeIsAnsweredAndTheSessionIsHeld(t *testing.T) {
	r := start(t, nil)
	s := r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	if s.Mode != "default" || s.PID <= 0 || s.Protocol != Protocol {
		t.Fatalf("mode %q, pid %d, protocol %d", s.Mode, s.PID, s.Protocol)
	}
	if s.StartMode != "default" {
		t.Errorf("the mode at the start is %q: a switch would not tell a changed mode from the launched one", s.StartMode)
	}
	sum, held := Held(r.spec.SessionID, s.PID)
	if !held || sum.Name != "demo" {
		t.Fatalf("the state file does not say the session is held: %+v", sum)
	}
	if _, held := Held(r.spec.SessionID, s.PID+1); held {
		t.Error("a claude under another pid was taken for the held one")
	}
}

func TestAMessageWaitsInTheQueueUntilClaudeTakesItUp(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	first := r.ask(Request{Op: OpSend, Text: "slow"})
	if !first.OK || !uuidLike(first.UUID) {
		t.Fatalf("send: %+v", first)
	}
	s := r.state()
	if len(s.Queue) != 1 || s.Queue[0].UUID != first.UUID || !s.Busy {
		t.Fatalf("a message claude has not read is not in the queue: %+v", s)
	}
	r.ask(Request{Op: OpSend, Text: "hello"})
	r.waitFor("the queue to empty", func(s State) bool { return len(s.Queue) == 0 && !s.Busy })
	if !strings.Contains(r.received(), first.UUID) {
		t.Error("the message went to claude without the id the panel knows it by")
	}
}

func TestAQuestionWaitsForAPersonAndTheAnswerReachesClaude(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	r.ask(Request{Op: OpSend, Text: "ask"})
	s := r.waitFor("the question", func(s State) bool { return len(s.Pending) == 1 })
	p := s.Pending[0]
	if p.Tool != "AskUserQuestion" || p.ToolUseID != "toolu_1" || !strings.Contains(string(p.Input), `"Q"`) {
		t.Fatalf("the question is not what claude asked: %+v", p)
	}
	// The file follows the socket by a write: a collector reads it every few
	// seconds, and what matters is that it gets there.
	sum := r.summary(func(s Summary) bool { return len(s.Waiting) == 1 })
	if sum.Waiting[0] != "AskUserQuestion" {
		t.Fatalf("the state file does not say what the session waits on: %+v", sum)
	}
	answer := json.RawMessage(`{"behavior":"allow","updatedInput":{"answers":{"Q":"Blue"}}}`)
	if reply := r.ask(Request{Op: OpRespond, RequestID: p.RequestID, Response: answer}); !reply.OK {
		t.Fatalf("respond: %+v", reply)
	}
	r.waitFor("the question to go", func(s State) bool { return len(s.Pending) == 0 })
	if got := r.eventually(`"Blue"`); !strings.Contains(got, `"request_id":"cc-1"`) {
		t.Fatalf("the answer did not reach claude:\n%s", got)
	}
}

// A message taken back from claude's queue leaves the holder's queue too: it
// will never be read, and a queue that kept it would block a switch for good.
func TestAMessageTakenBackLeavesTheQueue(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	const id = "11111111-2222-4333-8444-555555555555"
	if reply := r.ask(Request{Op: OpSend, Text: "slow", UUID: id}); !reply.OK || reply.UUID != id {
		t.Fatalf("send: %+v", reply)
	}
	reply := r.ask(Request{Op: OpControl, Subtype: "cancel_async_message", Fields: map[string]any{"message_uuid": id}})
	if !reply.OK || !strings.Contains(string(reply.Response), `"cancelled":true`) {
		t.Fatalf("cancel: %+v", reply)
	}
	if s := r.state(); len(s.Queue) != 0 {
		t.Errorf("a message claude took back is still in the holder's queue: %+v", s.Queue)
	}
	raw, err := os.ReadFile(WithdrawnPath(r.spec.SessionID))
	if err != nil || !strings.Contains(string(raw), Fingerprint("slow")) || strings.Contains(string(raw), "slow") {
		t.Errorf("the message taken back is not marked for the feed by its fingerprint alone: %v %s", err, raw)
	}
	again := r.ask(Request{Op: OpControl, Subtype: "cancel_async_message", Fields: map[string]any{"message_uuid": id}})
	if !again.OK || !strings.Contains(string(again.Response), `"cancelled":false`) {
		t.Errorf("a message no longer queued was reported taken back: %+v", again)
	}
}

func TestAnAnswerToNothingIsRefused(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	reply := r.ask(Request{Op: OpRespond, RequestID: "cc-9", Response: json.RawMessage(`{"behavior":"allow"}`)})
	if reply.OK {
		t.Fatal("an answer to a request nobody made went through")
	}
	if strings.Contains(r.received(), "cc-9") {
		t.Error("an answer to nothing reached claude")
	}
}

func TestOnlyTheListedControlsArePassedOn(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	if reply := r.ask(Request{Op: OpControl, Subtype: "end_session"}); reply.OK {
		t.Fatal("a request off the list was passed on")
	}
	if strings.Contains(r.received(), "end_session") {
		t.Fatal("a request off the list reached claude")
	}
	reply := r.ask(Request{Op: OpControl, Subtype: "interrupt"})
	if !reply.OK || !strings.Contains(string(reply.Response), "still_queued") {
		t.Fatalf("interrupt: %+v", reply)
	}
	r.ask(Request{Op: OpControl, Subtype: "set_model", Fields: map[string]any{"model": "sonnet"}})
	r.waitFor("the model to change", func(s State) bool { return s.Model == "sonnet" })
}

// A model and an effort chosen in the feed are remembered as they were chosen:
// the other side of a switch is started with them, and the id claude resolves
// a model to has lost its context window.
func TestAPickedModelAndEffortAreRemembered(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	if s := r.state(); s.Picked != "" || s.Effort != "" {
		t.Fatalf("a session that picked nothing remembers %q / %q", s.Picked, s.Effort)
	}
	r.ask(Request{Op: OpSend, Text: "/model opus[1m]"})
	r.ask(Request{Op: OpSend, Text: "/effort high"})
	r.ask(Request{Op: OpSend, Text: "tell me about /model sonnet"})
	r.waitFor("the choice", func(s State) bool { return s.Picked == "opus[1m]" && s.Effort == "high" && !s.Busy })
	r.ask(Request{Op: OpControl, Subtype: "set_model", Fields: map[string]any{"model": "haiku"}})
	if s := r.state(); s.Picked != "haiku" {
		t.Errorf("a model set by a control request is not remembered: %q", s.Picked)
	}
	raw, err := os.ReadFile(StatePath(r.spec.SessionID))
	if err != nil || !strings.Contains(string(raw), `"effort":"high"`) {
		t.Errorf("the state file does not carry the effort for the collector: %s", raw)
	}
}

// A mode set from the panel is in the state the moment claude accepts it: the
// picker shows the new one without waiting for claude's own event about it.
func TestAModeSetByTheRequestIsInTheStateAtOnce(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	reply := r.ask(Request{Op: OpControl, Subtype: "set_permission_mode", Fields: map[string]any{"mode": "plan"}})
	if !reply.OK {
		t.Fatalf("set_permission_mode: %+v", reply)
	}
	if s := r.state(); s.Mode != "plan" {
		t.Errorf("the mode after the request is %q", s.Mode)
	}
	raw, err := os.ReadFile(StatePath(r.spec.SessionID))
	if err != nil || !strings.Contains(string(raw), `"mode":"plan"`) {
		t.Errorf("the state file does not carry the mode for the collector: %s", raw)
	}
}

// Clearing on the stream starts a conversation under a new id, and the holder
// keeps its session by the old one: whichever way it comes, it is refused.
func TestClearingIsRefusedByTheHolder(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	for _, text := range []string{"/clear", "/reset", "  /new  please"} {
		if reply := r.ask(Request{Op: OpSend, Text: text}); reply.OK || !strings.Contains(reply.Error, "new id") {
			t.Errorf("%q was not refused: %+v", text, reply)
		}
	}
	if got := r.received(); strings.Contains(got, "/clear") || strings.Contains(got, "/reset") || strings.Contains(got, "/new") {
		t.Errorf("a clear reached claude:\n%s", got)
	}
	if reply := r.ask(Request{Op: OpSend, Text: "clear the table, please"}); !reply.OK {
		t.Errorf("a message that merely mentions clearing was refused: %+v", reply)
	}
}

func TestARequestThePanelDoesNotServeIsRefusedAtOnce(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	r.ask(Request{Op: OpSend, Text: "hook"})
	// claude reads the refusal after it has finished the turn it was sent in,
	// so what is waited for is the refusal itself, not the end of the turn.
	got := r.eventually(`"request_id":"cc-2"`)
	if !strings.Contains(got, `"subtype":"error"`) {
		t.Fatalf("a hook request was left unanswered, which hangs the turn:\n%s", got)
	}
	if s := r.state(); len(s.Pending) != 0 {
		t.Fatalf("a hook request was put before a person: %+v", s.Pending)
	}
}

func TestBackgroundTasksFollowTheStream(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	r.ask(Request{Op: OpSend, Text: "tasks"})
	s := r.waitFor("the task list", func(s State) bool { return len(s.Tasks) == 1 })
	if s.Tasks[0].ID != "b1" {
		t.Fatalf("tasks: %+v", s.Tasks)
	}
	r.summary(func(s Summary) bool { return s.Tasks == 1 })
}

// step sends a message the fake claude answers with events and no turn, and
// waits until the holder has read them.
func (r *rig) step(text string) State {
	r.t.Helper()
	r.ask(Request{Op: OpSend, Text: text})
	return r.waitFor(text, func(s State) bool { return len(s.Tasks) == 1 && s.Tasks[0].ID == text })
}

func (r *rig) compacting() time.Time {
	r.t.Helper()
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	s := r.step("compact")
	if s.Compacting == nil {
		r.t.Fatal("a compaction claude reported is not in the state")
	}
	return *s.Compacting
}

// Claude repeats "compacting" every half a minute while it compacts; the
// clock of the compaction runs from the first report, not the last one.
func TestACompactionIsTimedFromItsFirstReport(t *testing.T) {
	r := start(t, nil)
	began := r.compacting()
	time.Sleep(20 * time.Millisecond)
	s := r.step("compact-again")
	if s.Compacting == nil || !s.Compacting.Equal(began) {
		t.Fatalf("the repeated report moved the start of the compaction: %v, then %v", began, s.Compacting)
	}
}

// A change of mode comes as a status too, with no status in it; it is not the
// end of a compaction that is running.
func TestAChangeOfModeDoesNotEndACompaction(t *testing.T) {
	r := start(t, nil)
	began := r.compacting()
	s := r.step("mode-mid-compact")
	if s.Mode != "plan" {
		t.Errorf("the mode reported during the compaction was not taken: %q", s.Mode)
	}
	if s.Compacting == nil || !s.Compacting.Equal(began) {
		t.Fatalf("a change of mode ended the compaction: %v", s.Compacting)
	}
}

// The end of a compaction is a status with its outcome; the state file says
// a compaction runs while it runs, and says nothing of it after.
func TestTheEndOfACompactionIsReported(t *testing.T) {
	r := start(t, nil)
	r.compacting()
	r.summary(func(s Summary) bool { return s.Compacting != nil })
	if s := r.step("compact-done"); s.Compacting != nil {
		t.Fatalf("the compaction is still running after claude reported its end: %v", s.Compacting)
	}
	r.summary(func(s Summary) bool { return s.Compacting == nil })
}

func TestTheBoundaryOfACompactionEndsIt(t *testing.T) {
	r := start(t, nil)
	r.compacting()
	if s := r.step("boundary"); s.Compacting != nil {
		t.Fatalf("the compaction is still running after its boundary: %v", s.Compacting)
	}
}

// A compaction interrupted ends with the turn, and claude reports nothing
// else about it.
func TestAnEndedTurnEndsTheCompaction(t *testing.T) {
	r := start(t, nil)
	r.compacting()
	r.ask(Request{Op: OpSend, Text: "hi"})
	if s := r.waitFor("the end of the turn", func(s State) bool { return !s.Busy }); s.Compacting != nil {
		t.Fatalf("the compaction outlived the turn: %v", s.Compacting)
	}
}

func TestTheOpeningMessageIsSentAfterTheHandshake(t *testing.T) {
	r := start(t, func(s *Spec) { s.Intent = "start with the tests" })
	r.waitFor("the opening turn", func(s State) bool { return len(s.Init) > 0 && !s.Busy && len(s.Queue) == 0 })
	got := r.received()
	init, intent := strings.Index(got, `"initialize"`), strings.Index(got, "start with the tests")
	if init < 0 || intent < 0 || intent < init {
		t.Fatalf("the opening message did not follow the handshake:\n%s", got)
	}
}

func TestTheStateFileKeepsNoWordsOfTheConversation(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	r.ask(Request{Op: OpSend, Text: "slow"})
	r.ask(Request{Op: OpSend, Text: "ask"})
	r.waitFor("the question", func(s State) bool { return len(s.Pending) == 1 })
	raw, err := os.ReadFile(StatePath(r.spec.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	for _, words := range []string{"slow", `"Q"`, "questions"} {
		if strings.Contains(string(raw), words) {
			t.Errorf("the state file carries %q from the conversation: %s", words, raw)
		}
	}
}

func TestClosingEndsTheSessionAndLeavesNothingBehind(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	if reply := r.ask(Request{Op: OpClose}); !reply.OK {
		t.Fatalf("close: %+v", reply)
	}
	select {
	case err := <-r.done:
		r.done <- err
		if err != nil {
			t.Fatalf("the holder ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("closing did not end the session")
	}
	for _, path := range []string{SocketPath(r.spec.SessionID), StatePath(r.spec.SessionID), LogPath(r.spec.SessionID)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is left behind", path)
		}
	}
	if _, held := Held(r.spec.SessionID, 0); held {
		t.Error("an ended session is still reported held")
	}
}

func TestOneConversationHasOneHolder(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	second := make(chan error, 1)
	go func() { second <- Run(ctx, r.spec) }()
	select {
	case err := <-second:
		if err == nil || !strings.Contains(err.Error(), "already held") {
			t.Fatalf("a second holder of one conversation was let in: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a second holder of one conversation was let in and started its own claude")
	}
}

func TestAClaudeThatDiesAtStartLeavesItsWordsInTheLog(t *testing.T) {
	t.Setenv("FAKE_DIE", "1")
	r := start(t, nil)
	select {
	case err := <-r.done:
		r.done <- err
	case <-time.After(10 * time.Second):
		t.Fatal("the holder outlived its claude")
	}
	log, _ := os.ReadFile(LogPath(r.spec.SessionID))
	if !strings.Contains(string(log), "exit 3") || !strings.Contains(string(log), "not signed in") {
		t.Fatalf("the log does not say why the session died:\n%s", log)
	}
	// The handshake was still waiting when claude died; its end must not
	// write a state file for a session that is already gone.
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(StatePath(r.spec.SessionID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a state file outlived its session: the collector would put a dead session on the map")
	}
}

func TestASpecThatIsNotASessionIsRefused(t *testing.T) {
	for name, spec := range map[string]Spec{
		"no uuid":      {Name: "a", Dir: "/tmp", SessionID: "abc", Argv: []string{"x"}},
		"no name":      {Dir: "/tmp", SessionID: NewSessionID(), Argv: []string{"x"}},
		"relative dir": {Name: "a", Dir: "tmp", SessionID: NewSessionID(), Argv: []string{"x"}},
		"no command":   {Name: "a", Dir: "/tmp", SessionID: NewSessionID()},
	} {
		if err := checkSpec(spec); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOnlyALaunchThatFailedKeepsItsLog(t *testing.T) {
	for _, c := range []struct {
		code  int
		lived time.Duration
		keeps bool
	}{
		{0, time.Second, false},
		{3, time.Second, true},
		{143, 30 * time.Minute, false},
		{1, 2 * time.Minute, false},
	} {
		if got := failedStart(c.code, c.lived); got != c.keeps {
			t.Errorf("exit %d after %s: failed start %v, want %v", c.code, c.lived, got, c.keeps)
		}
	}
}

// The collector reads the fingerprints in Python: the same constant stands in
// its test, and a change of the hash on one side only is caught on both.
func TestTheFingerprintIsTheCollectors(t *testing.T) {
	if got := Fingerprint("  slow  "); got != "5e0cf7bd1dfa3831788b0cf6dedcdd22" {
		t.Errorf("Fingerprint = %s", got)
	}
}

func TestSettingsRequestsPassOnlyThePanelsShape(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	for _, c := range []struct {
		subtype string
		fields  map[string]any
	}{
		{"apply_flag_settings", map[string]any{"settings": map[string]any{"ultracode": true, "hooks": map[string]any{"Stop": "rm"}}}},
		{"apply_flag_settings", map[string]any{"settings": map[string]any{"permissions": map[string]any{"allow": "Bash"}}}},
		{"apply_flag_settings", map[string]any{"settings": map[string]any{"ultracode": "yes"}}},
		{"update_settings", map[string]any{"source": "localSettings", "settings": map[string]any{"effortLevel": "high"}}},
		{"update_settings", map[string]any{"source": "userSettings", "settings": map[string]any{"effortLevel": "max"}}},
		{"update_settings", map[string]any{"source": "userSettings", "settings": map[string]any{"effortLevel": "high", "model": "x"}}},
		{"get_settings", map[string]any{"source": "userSettings"}},
	} {
		reply := r.ask(Request{Op: OpControl, Subtype: c.subtype, Fields: c.fields})
		if reply.OK {
			t.Errorf("%s %v was passed on", c.subtype, c.fields)
		}
	}
	for _, word := range []string{"hooks", "permissions", "localSettings", `"max"`, `"model":"x"`, "get_settings"} {
		if strings.Contains(r.received(), word) {
			t.Errorf("claude read %s", word)
		}
	}
}

func TestSettingsAreAnsweredWithoutTheEnvironment(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	reply := r.ask(Request{Op: OpControl, Subtype: "get_settings"})
	if !reply.OK {
		t.Fatalf("get_settings was refused: %+v", reply)
	}
	if strings.Contains(string(reply.Response), "glpat") || strings.Contains(string(reply.Response), "env") {
		t.Errorf("the answer carries the environment of the session: %s", reply.Response)
	}
	if !strings.Contains(string(reply.Response), `"applied"`) {
		t.Errorf("the answer lost what the session runs with: %s", reply.Response)
	}
}

func TestAnEffortClaudeSavesIsPassedOn(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	reply := r.ask(Request{Op: OpControl, Subtype: "update_settings",
		Fields: map[string]any{"source": "userSettings", "settings": map[string]any{"effortLevel": "high"}}})
	if !reply.OK {
		t.Fatalf("an effort claude saves was refused: %+v", reply)
	}
	r.eventually(`"effortLevel":"high"`)
}

func TestUltracodeIsTheEffortOnlyOnceClaudeRunsIt(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	on := Request{Op: OpControl, Subtype: "apply_flag_settings", Fields: map[string]any{"settings": map[string]any{"ultracode": true}}}

	r.ask(Request{Op: OpControl, Subtype: "set_model", Fields: map[string]any{"model": "haiku"}})
	if reply := r.ask(on); reply.OK || !strings.Contains(reply.Error, "did not take") {
		t.Errorf("ultracode on a model that does not run it was reported on: %+v", reply)
	}
	if s := r.state(); s.Effort == Ultracode {
		t.Error("the state says ultracode where claude runs without it")
	}

	r.ask(Request{Op: OpControl, Subtype: "set_model", Fields: map[string]any{"model": "sonnet"}})
	if reply := r.ask(on); !reply.OK {
		t.Fatalf("ultracode claude runs was refused: %+v", reply)
	}
	r.waitFor("ultracode in the state", func(s State) bool { return s.Effort == Ultracode })
	r.summary(func(sum Summary) bool { return sum.Effort == Ultracode })

	r.ask(Request{Op: OpControl, Subtype: "apply_flag_settings", Fields: map[string]any{"settings": map[string]any{"ultracode": false}}})
	r.waitFor("ultracode off", func(s State) bool { return s.Effort == "xhigh" })
}
