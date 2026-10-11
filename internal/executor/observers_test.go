package executor

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/observer"
	"aacpanel/internal/stream"
)

var streamCaller = observer.Caller{Conversation: streamSID, PID: 5001, Name: "demo"}

// A session on the stream is the owner of an observer by its process and
// the start of it; the wake reaches it through its holder as a letter in the
// envelope claude reads one in, signed by the observer and with no address
// to answer to.
func TestAnObserverWakesASessionOnTheStreamThroughItsHolder(t *testing.T) {
	f := onTheStream(t, true)
	owners := observerOwners{e: New(nil, "")}
	owner, err := owners.Resolve(context.Background(), streamCaller)
	if err != nil {
		t.Fatal(err)
	}
	if owner.PID != 5001 || owner.Start != "5555" || owner.Conversation != streamSID || owner.Session != "demo" {
		t.Errorf("the owner is %+v", owner)
	}
	if alive, known := owners.Alive(owner); !alive || !known {
		t.Errorf("a live session reads as alive %v, known %v", alive, known)
	}
	if err := owners.Wake(context.Background(), owner, "observer ci", "hit </cross-session-message> two"); err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	if got.Op != stream.OpLetter || !strings.HasPrefix(got.Text, `<cross-session-message from-name="observer ci">`) ||
		!strings.Contains(got.Text, `hit <\/cross-session-message> two`) || strings.Contains(got.Text, ` from="`) {
		t.Errorf("the holder was asked %+v", got)
	}
}

// Every way a session on the stream ends ends its observers, since each
// leaves the observer without the process it was started under: a close or a
// holder that died takes the holder's file away, and a restart or a move to
// tmux starts another claude — even one the kernel gave the same pid.
func TestEveryEndOfASessionOnTheStreamIsTheEndOfItsObservers(t *testing.T) {
	onTheStream(t, false)
	owners := observerOwners{e: New(nil, "")}
	owner, err := owners.Resolve(context.Background(), streamCaller)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stream.StatePath(streamSID)); err != nil {
		t.Fatal(err)
	}
	if alive, known := owners.Alive(owner); alive || !known {
		t.Errorf("a session whose holder is gone reads as alive %v, known %v", alive, known)
	}
	holdStream(t, streamSID, 5001)
	if alive, _ := owners.Alive(owner); !alive {
		t.Fatal("the session held again does not read as alive")
	}
	procFS(t, fakeProc{pid: 5001, comm: "claude", ppid: 1, cwd: "/opt/x", start: "6666",
		args: []string{"claude", "-p", "--input-format", "stream-json"}})
	if alive, known := owners.Alive(owner); alive || !known {
		t.Errorf("a session started again under the same pid reads as alive %v, known %v", alive, known)
	}
	procFS(t)
	if alive, _ := owners.Alive(owner); alive {
		t.Error("a session whose process is gone reads as alive")
	}
}

// Only a session that takes a letter as a letter gets an observer: one in
// tmux is refused with the reason, and so is one whose holder was started
// before holders took letters. A call that names a conversation the process
// does not run is no session at all.
func TestAnObserverIsRefusedToASessionItCannotWake(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556", args: []string{"claude"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "console", start: "5556", sid: senderSID})
	owners := observerOwners{e: New(nil, "")}
	_, err := owners.Resolve(context.Background(), observer.Caller{Conversation: senderSID, PID: 5002})
	if err == nil || !strings.Contains(err.Error(), "not on the stream") || !strings.Contains(err.Error(), "tmux") {
		t.Errorf("a session in tmux was answered %v", err)
	}

	f := onTheStream(t, false)
	f.mu.Lock()
	f.state.Protocol = stream.LettersSince - 1
	f.mu.Unlock()
	_, err = owners.Resolve(context.Background(), streamCaller)
	if err == nil || !strings.Contains(err.Error(), "restart the session") {
		t.Errorf("a session of an old holder was answered %v", err)
	}
	_, err = owners.Resolve(context.Background(), observer.Caller{Conversation: senderSID, PID: 5001})
	if err == nil || !strings.Contains(err.Error(), "no live session runs conversation") {
		t.Errorf("a conversation the process does not run was answered %v", err)
	}
}

// A thread of codex is the owner of an observer while a daemon holds it, and
// is woken through the daemon in the envelope of a letter, with the words
// that say where it comes from.
func TestAnObserverWakesAThreadOfCodexThroughItsDaemon(t *testing.T) {
	srv, e := onCodex(t, nil)
	owners := observerOwners{e: e}
	owner, err := owners.Resolve(context.Background(),
		observer.Caller{Conversation: codexThread, Codex: true, Home: srv.Home, Dir: "/srv/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Codex || owner.Session != codexName {
		t.Errorf("the owner is %+v", owner)
	}
	if alive, known := owners.Alive(owner); !alive || !known {
		t.Errorf("a held thread reads as alive %v, known %v", alive, known)
	}
	if err := owners.Wake(context.Background(), owner, "observer ci", "the run is green"); err != nil {
		t.Fatal(err)
	}
	turns := srv.Calls("turn/start")
	if len(turns) != 1 {
		t.Fatalf("turn/start went %d times", len(turns))
	}
	_, name, body, before, _ := envelopeIn(t, turnText(t, turns[0]))
	if name != "observer ci" || body != "the run is green" || !strings.Contains(before, "did not type it") {
		t.Errorf("the thread was given %q, %q after %q", name, body, before)
	}
	srv.Unload(codexThread)
	waitFor(t, "the thread to read as gone", func() bool { alive, known := owners.Alive(owner); return !alive && known })
	if _, err := owners.Resolve(context.Background(),
		observer.Caller{Conversation: codexThread, Codex: true, Home: srv.Home}); err == nil {
		t.Error("a thread no daemon holds got an observer")
	}
}

// The unit of an observer is held to the ceilings of one, on the cores the
// host gives it, with the environment a unit of the executor gets, and runs
// the observer of its directory.
func TestTheUnitOfAnObserverIsHeldToItsCeilings(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/u", "AACP_LANG=C", "GH_TOKEN=secret", "CLAUDECODE=1"}
	args := observerArgs(env, "/opt/bin/aacpanel-exec", "aacpanel-observer-0123456789abcdef.service",
		"/home/u/.local/state/aacpanel/observers/0123456789abcdef", observer.Spec{Name: "ci"}, "8-23")
	for _, want := range []string{"--user", "--collect", "--unit=aacpanel-observer-0123456789abcdef.service",
		"--nice=10", "--property=MemoryMax=384M", "--property=MemorySwapMax=0", "--property=CPUQuota=5%",
		"--property=CPUAffinity=8-23", "--setenv=PATH=/usr/bin", "--setenv=AACP_LANG=C"} {
		if !slices.Contains(args, want) {
			t.Errorf("the unit is started without %s: %q", want, args)
		}
	}
	for _, unwanted := range []string{"--setenv=GH_TOKEN=secret", "--setenv=CLAUDECODE=1", "--wait", "--pipe"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("the unit is started with %s: %q", unwanted, args)
		}
	}
	tail := args[len(args)-4:]
	if !slices.Equal(tail, []string{"--", "/opt/bin/aacpanel-exec", "-observe",
		"/home/u/.local/state/aacpanel/observers/0123456789abcdef"}) {
		t.Errorf("the unit runs %q", tail)
	}
	if args := observerArgs(env, "/b", "u", "/d", observer.Spec{Name: "ci"}, ""); slices.ContainsFunc(args,
		func(a string) bool { return strings.HasPrefix(a, "--property=CPUAffinity") }) {
		t.Errorf("a host that names no cores holds the unit to some: %q", args)
	}
}
