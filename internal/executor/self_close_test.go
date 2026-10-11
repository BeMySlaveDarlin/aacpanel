package executor

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// closeAskedBy is a close of the session demo asked for by the conversation
// it names, as a session closing itself asks for one.
func closeAskedBy(conversation string) action.Request {
	r := req(action.SessionClose, "demo")
	r.Conversation = conversation
	return r
}

// A close a session asks for names the conversation it is asked by, and goes
// ahead only while the session runs it: a late repeat of the call would close
// the session brought up under the name since.
func TestACloseOfItselfClosesOnlyTheConversationItNames(t *testing.T) {
	consoleStand(t, "idle")
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	_, err := e.Execute(context.Background(), closeAskedBy(streamSID))
	if err == nil || !strings.Contains(err.Error(), "runs conversation "+consoleSID) ||
		!strings.Contains(err.Error(), "a close closes only") {
		t.Fatalf("a close asked for by a conversation the session does not run answered %v", err)
	}
	if len(signals.sent) != 0 {
		t.Fatalf("signals %v went out before the refusal", signals.sent)
	}

	own := closeAskedBy(consoleSID)
	own.ID = "close-own"
	detail, err := e.Execute(context.Background(), own)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(signals.sent, ",") != "1004:terminated" || !strings.Contains(detail, "closed gracefully") {
		t.Errorf("the close of its own conversation sent %v and said %q", signals.sent, detail)
	}
}

// A session on the stream closing itself is closed by its input, as the
// panel's Close closes it: claude finishes the turn the call is part of and
// goes.
func TestACloseOfItselfOnTheStreamGoesByItsInput(t *testing.T) {
	_, holder := streamStand(t, func(*stream.State) {})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	if _, err := e.Execute(context.Background(), closeAskedBy(streamSID)); err != nil {
		t.Fatal(err)
	}
	if asked := holder.asked(); len(asked) != 1 || asked[0].Op != stream.OpClose {
		t.Errorf("the holder was asked %+v", asked)
	}
}

// A thread of codex closing itself names itself, and the close goes to that
// thread alone: a close that names another thread interrupts nothing and
// leaves nothing.
func TestACodexThreadClosingItselfClosesOnlyItself(t *testing.T) {
	srv, e, p := onCodexContour(t)
	_, name, err := open(t, e, p, `{"agent":"codex","intent":"go"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := e.codex.Find(name)[0].ID
	newTmuxStub(t, nil, "")

	ctx := context.Background()
	other := action.Request{ID: "close-other", Kind: action.SessionClose, Target: name,
		Conversation: "019a2000-0000-7000-8000-0000beef0099"}
	if _, err := e.Execute(ctx, other); err == nil || !strings.Contains(err.Error(), "runs thread "+id) {
		t.Fatalf("a close naming another thread answered %v", err)
	}
	if n := len(srv.Calls("turn/interrupt")) + len(srv.Calls("thread/unsubscribe")); n != 0 {
		t.Fatalf("a refused close touched the thread %d times", n)
	}

	own := action.Request{ID: "close-own", Kind: action.SessionClose, Target: name, Conversation: id}
	if _, err := e.Execute(ctx, own); err != nil {
		t.Fatal(err)
	}
	if leaves := srv.Calls("thread/unsubscribe"); len(leaves) != 1 || paramsOf(t, leaves[0])["threadId"] != id {
		t.Errorf("thread/unsubscribe went as %s", leaves)
	}
}
