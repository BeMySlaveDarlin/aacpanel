package executor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

const streamSID = "66666666-6666-4666-8666-666666666666"

// fakeHolder answers on a holder's socket the way a holder does and keeps what
// it was asked, so a test sees what would have reached claude.
type fakeHolder struct {
	mu    sync.Mutex
	state stream.State
	got   []stream.Request
	// Ops the holder answers with an error, as a holder that cannot do them.
	fails map[string]string
}

func (f *fakeHolder) asked() []stream.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]stream.Request{}, f.got...)
}

func onTheStream(t *testing.T, busy bool, pending ...stream.Pending) *fakeHolder {
	t.Helper()
	procFS(t, fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5555",
		args: []string{"claude", "-p", "--input-format", "stream-json", "-n", "demo", "--session-id", streamSID}})
	sessionFiles(t, fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID})
	holdStream(t, streamSID, 5001)

	f := &fakeHolder{state: stream.State{Protocol: stream.Protocol, SessionID: streamSID, PID: 5001, Busy: busy,
		Pending: pending, Queue: []stream.Queued{}, Tasks: []stream.Task{}}}
	ln, err := net.Listen("unix", stream.SocketPath(streamSID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var r stream.Request
			_ = json.NewDecoder(conn).Decode(&r)
			f.mu.Lock()
			if r.Op != stream.OpState {
				f.got = append(f.got, r)
			}
			st := f.state
			failed := f.fails[r.Op]
			f.mu.Unlock()
			reply := stream.Reply{OK: true}
			if r.Op == stream.OpState {
				reply.State = &st
			}
			if failed != "" {
				reply = stream.Reply{Error: failed}
			}
			_ = json.NewEncoder(conn).Encode(reply)
			_ = conn.Close()
		}
	}()
	return f
}

func bash(requestID string, suggestions string) stream.Pending {
	p := stream.Pending{RequestID: requestID, Tool: "Bash", ToolUseID: "toolu_b",
		Input:  json.RawMessage(`{"command":"rm -rf build\nmake","description":"clean and build"}`),
		Reason: "Contains rm"}
	if suggestions != "" {
		p.Suggestions = json.RawMessage(suggestions)
	}
	return p
}

func only(t *testing.T, f *fakeHolder) stream.Request {
	t.Helper()
	got := f.asked()
	if len(got) != 1 {
		t.Fatalf("the holder was asked %d times: %+v", len(got), got)
	}
	return got[0]
}

func TestAMessageToAStreamSessionIsALineNotKeys(t *testing.T) {
	f := onTheStream(t, true)
	e, _ := newTest(t, "")
	r := req(action.SessionSend, "demo")
	r.Text = "run the tests"
	out, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	if got.Op != stream.OpSend || got.Text != "run the tests" {
		t.Fatalf("the holder was asked %+v", got)
	}
	if !strings.Contains(out, "on the stream") || !strings.Contains(out, "queue") {
		t.Errorf("a busy stream session was not said to queue the message: %q", out)
	}
}

func TestStopOnTheStreamInterruptsOnlyABusySession(t *testing.T) {
	f := onTheStream(t, true)
	e, _ := newTest(t, "")
	if _, err := e.Execute(context.Background(), req(action.SessionStop, "demo")); err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); got.Op != stream.OpControl || got.Subtype != "interrupt" {
		t.Fatalf("stop asked the holder %+v", got)
	}

	idle := onTheStream(t, false)
	if _, err := e.Execute(context.Background(), req(action.SessionStop, "demo")); err != nil {
		t.Fatal(err)
	}
	if got := idle.asked(); len(got) != 0 {
		t.Errorf("a free session was interrupted: %+v", got)
	}
}

func TestAPermissionOnTheStreamIsReadAsTheScreenKnowsIt(t *testing.T) {
	onTheStream(t, true, bash("cc-7", `[{"type":"addRules","rules":[{"toolName":"Bash"}]}]`))
	e, _ := newTest(t, "")
	d, err := e.Permission(context.Background(), "demo")
	if err != nil || d == nil {
		t.Fatalf("no permission: %v", err)
	}
	if d.Tool != "Bash" || d.Fingerprint != "cc-7" || strings.Join(d.Action, "|") != "rm -rf build|make" {
		t.Fatalf("permission %+v", d)
	}
	if !strings.Contains(strings.Join(d.Note, " "), "Contains rm") {
		t.Errorf("why claude asks is lost: %v", d.Note)
	}
	if len(d.Options) != 3 || !d.Options[1].Lasting || d.Options[2].Text != "No" {
		t.Fatalf("options %+v", d.Options)
	}
}

func TestPermitOnTheStreamAnswersTheRequestItWasMeantFor(t *testing.T) {
	ctx := context.Background()
	permit := func(option int, fingerprint string) action.Request {
		r := req(action.SessionPermit, "demo")
		r.Permit = &action.Permit{Option: option, Fingerprint: fingerprint}
		return r
	}
	rules := `[{"type":"addRules","rules":[{"toolName":"Bash"}]}]`

	t.Run("yes", func(t *testing.T) {
		f := onTheStream(t, true, bash("cc-7", rules))
		e, _ := newTest(t, "")
		if _, err := e.Execute(ctx, permit(1, "cc-7")); err != nil {
			t.Fatal(err)
		}
		got := only(t, f)
		if got.Op != stream.OpRespond || got.RequestID != "cc-7" || !strings.Contains(string(got.Response), `"allow"`) ||
			strings.Contains(string(got.Response), "updatedPermissions") {
			t.Fatalf("allow once sent %+v %s", got, got.Response)
		}
	})
	t.Run("yes and do not ask again", func(t *testing.T) {
		f := onTheStream(t, true, bash("cc-7", rules))
		e, _ := newTest(t, "")
		if _, err := e.Execute(ctx, permit(2, "cc-7")); err != nil {
			t.Fatal(err)
		}
		if got := only(t, f); !strings.Contains(string(got.Response), `"updatedPermissions":[{"type":"addRules"`) {
			t.Fatalf("a lasting allow did not carry the rule claude offered: %s", got.Response)
		}
	})
	t.Run("no", func(t *testing.T) {
		f := onTheStream(t, true, bash("cc-7", rules))
		e, _ := newTest(t, "")
		if _, err := e.Execute(ctx, permit(3, "cc-7")); err != nil {
			t.Fatal(err)
		}
		if got := only(t, f); !strings.Contains(string(got.Response), `"deny"`) {
			t.Fatalf("no sent %s", got.Response)
		}
	})
	t.Run("a press meant for another request", func(t *testing.T) {
		f := onTheStream(t, true, bash("cc-8", rules))
		e, _ := newTest(t, "")
		if _, err := e.Execute(ctx, permit(1, "cc-7")); err == nil {
			t.Fatal("a press meant for an earlier request answered the next one")
		}
		if got := f.asked(); len(got) != 0 {
			t.Fatalf("the holder was answered anyway: %+v", got)
		}
	})
}

func question() stream.Pending {
	return stream.Pending{RequestID: "cc-9", Tool: "AskUserQuestion", ToolUseID: "toolu_q",
		Input: json.RawMessage(`{"questions":[` +
			`{"question":"Pick a colour","options":[{"label":"Red","preview":"R"},{"label":"Blue","preview":"B"}]},` +
			`{"question":"Which checks","multiSelect":true,"options":[{"label":"unit"},{"label":"lint"},{"label":"e2e"}]},` +
			`{"question":"Anything else","options":[{"label":"no"}]}]}`)}
}

func TestAnAnswerOnTheStreamIsTheWordsOfTheOptions(t *testing.T) {
	f := onTheStream(t, true, question())
	e, _ := newTest(t, "")
	r := req(action.SessionAnswer, "demo")
	r.Answer = &action.Answer{AskID: "toolu_q", Picks: [][]int{{2}, {1, 3}, {}}, Texts: []string{"", "", "ship it tonight"}}
	if _, err := e.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	var body struct {
		Behavior     string `json:"behavior"`
		UpdatedInput struct {
			Answers   map[string]string `json:"answers"`
			Questions []any             `json:"questions"`
		} `json:"updatedInput"`
	}
	if err := json.Unmarshal(got.Response, &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Pick a colour": "Blue", "Which checks": "unit, e2e", "Anything else": "ship it tonight"}
	for q, a := range want {
		if body.UpdatedInput.Answers[q] != a {
			t.Errorf("%q answered %q, want %q", q, body.UpdatedInput.Answers[q], a)
		}
	}
	if body.Behavior != "allow" || len(body.UpdatedInput.Questions) != 3 {
		t.Errorf("the answer lost the questions it answers: %s", got.Response)
	}
}

func TestAnAnswerToAQuestionNoLongerAskedIsRefused(t *testing.T) {
	f := onTheStream(t, true, question())
	e, _ := newTest(t, "")
	r := req(action.SessionAnswer, "demo")
	r.Answer = &action.Answer{AskID: "toolu_old", Picks: [][]int{{1}, {1}, {1}}}
	if _, err := e.Execute(context.Background(), r); err == nil {
		t.Fatal("an answer to a withdrawn question went through")
	}
	if len(f.asked()) != 0 {
		t.Fatal("the holder was answered anyway")
	}
}

func TestDismissAndEscapeOnTheStreamRefuseWithAReason(t *testing.T) {
	f := onTheStream(t, true, question())
	e, _ := newTest(t, "")
	r := req(action.SessionDismiss, "demo")
	r.Answer = &action.Answer{AskID: "toolu_q"}
	if _, err := e.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); !strings.Contains(string(got.Response), `"deny"`) || !strings.Contains(string(got.Response), "conversation") {
		t.Fatalf("dismiss sent %s", got.Response)
	}

	esc := onTheStream(t, true, bash("cc-7", ""))
	if _, err := e.Execute(context.Background(), req(action.SessionEscape, "demo")); err != nil {
		t.Fatal(err)
	}
	if got := only(t, esc); got.RequestID != "cc-7" || !strings.Contains(string(got.Response), "refused") {
		t.Fatalf("escape sent %+v %s", got, got.Response)
	}
}

func TestATerminalSessionNeverAsksAHolder(t *testing.T) {
	if onStream(liveSession{Name: "demo", PID: 5001, SessionID: "s-5001"}) {
		t.Fatal("a session with no holder was taken for a stream one")
	}
	_ = os.Getpid()
}
