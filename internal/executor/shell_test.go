package executor

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

const runID = "33333333-4444-4555-8666-777777777777"

// A command after "!" goes to the holder as a command, under the id the feed
// follows it by, and never as a message the model would read as a request.
func TestAShellCommandGoesToTheHolderAsACommand(t *testing.T) {
	f := onTheStream(t, false)
	e, _ := newTest(t, "")
	r := req(action.SessionShell, "demo")
	r.Text, r.MessageID = "git -C /srv/app status", runID
	detail, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	if got.Op != stream.OpShell || got.Text != "git -C /srv/app status" || got.UUID != runID {
		t.Fatalf("the holder was asked %+v", got)
	}
	if !strings.Contains(detail, "runs in demo") || !strings.Contains(detail, "output goes into the conversation") {
		t.Errorf("the report %q does not say the command runs and where its output goes", detail)
	}
}

// A busy session runs the command all the same — beside the answer, not after
// it — and the report says the model reads the output along the way.
func TestAShellCommandRunsBesideABusyAnswer(t *testing.T) {
	f := onTheStream(t, true)
	e, _ := newTest(t, "")
	r := req(action.SessionShell, "demo")
	r.Text = "ls"
	detail, err := e.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); got.Op != stream.OpShell {
		t.Fatalf("a busy session was asked %+v", got)
	}
	if !strings.Contains(detail, "along the way") {
		t.Errorf("the report %q does not say the busy session reads the output on its way", detail)
	}
}

// What the holder refuses is the answer, word for word.
func TestAShellCommandTheHolderRefusesIsRefused(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.fails = map[string]string{stream.OpShell: "the session is closing and takes no more input"}
	f.mu.Unlock()
	e, _ := newTest(t, "")
	r := req(action.SessionShell, "demo")
	r.Text = "ls"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("a refused command passed: %v", err)
	}
}

// A session started before "!" existed keeps its old holder for its whole
// life, and that holder answers with the bare name of an operation it does
// not know: the person is told what to do about it instead.
func TestAShellCommandToAnOlderHolderSaysToRestartTheSession(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.fails = map[string]string{stream.OpShell: stream.NoSuchOp + `: "shell"`}
	f.mu.Unlock()
	e, _ := newTest(t, "")
	r := req(action.SessionShell, "demo")
	r.Text = "ls"
	_, err := e.Execute(context.Background(), r)
	if err == nil || !strings.Contains(err.Error(), "restart the session") ||
		!strings.Contains(err.Error(), "conversation is kept") || strings.Contains(err.Error(), "no such operation") {
		t.Fatalf("an older holder's refusal reached the person as %v", err)
	}
}

// A terminal runs "!" in its own composer, and the composer sends it there as
// a message: a command meant for the stream that finds the session in tmux is
// refused rather than typed into it.
func TestAShellCommandIsNotTypedIntoATerminal(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	r := req(action.SessionShell, "term")
	r.Text = "ls"
	if _, err := e.Execute(context.Background(), r); err == nil || !strings.Contains(err.Error(), "lives in tmux") {
		t.Errorf("a command went to a terminal: %v", err)
	}
}
