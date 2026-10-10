package codex

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
)

func ready(t *testing.T, l *Link) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}

func params(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// polls lets the link read the daemon a few rounds.
func polls() { time.Sleep(8 * pollEvery) }

// A thread starts in the directory with only what the map chose: a choice
// left empty is not sent, so the config.toml of the home decides it.
func TestAThreadStartsWithWhatTheMapChoseAndNothingElse(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()

	bare, err := l.Start(ctx, Begin{CWD: "/srv/proj"})
	if err != nil {
		t.Fatal(err)
	}
	full, err := l.Start(ctx, Begin{CWD: "/srv/proj", Model: "gpt-5.5", Approval: "untrusted", Sandbox: "read-only",
		Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	starts := srv.Calls("thread/start")
	if len(starts) != 2 {
		t.Fatalf("thread/start went %d times", len(starts))
	}
	if got := params(t, starts[0]); !reflect.DeepEqual(got, map[string]any{"cwd": "/srv/proj"}) {
		t.Errorf("a thread of a map that chose nothing started with %v", got)
	}
	want := map[string]any{"cwd": "/srv/proj", "model": "gpt-5.5", "approvalPolicy": "untrusted", "sandbox": "read-only",
		"config": map[string]any{"model_reasoning_effort": "high"}}
	if got := params(t, starts[1]); !reflect.DeepEqual(got, want) {
		t.Errorf("a thread started with %v, expected %v", got, want)
	}
	if n := len(srv.Calls("turn/start")) + len(srv.Calls("thread/name/set")); n != 0 {
		t.Errorf("a thread with no first message and no name had %d turns and namings", n)
	}
	if bare == full || SessionName(full) != "codex-beef0002" {
		t.Errorf("the threads are %s and %s", bare, full)
	}
	if st, _, ok := stateOf(t, full); !ok || st.CWD != "/srv/proj" || st.Model != "gpt-5.5" || st.Effort != "high" ||
		st.Name != "codex-beef0002" {
		t.Errorf("the new thread is on the map as %+v (%v)", st, ok)
	}
}

// A thread for codex in a terminal is named at its start: the naming writes
// it to the disk, and codex resumes only a thread written there. The name is
// the panel's and the row shows it, until codex in the terminal titles the
// thread after its first request.
func TestAThreadForATerminalIsNamed(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	id, err := l.Start(context.Background(), Begin{CWD: "/srv/proj", Name: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	names := srv.Calls("thread/name/set")
	if len(names) != 1 || !reflect.DeepEqual(params(t, names[0]), map[string]any{"threadId": id, "name": "shop"}) {
		t.Errorf("thread/name/set went as %s", names)
	}
	if st, raw, _ := stateOf(t, id); st.Title != "shop" {
		t.Errorf("the name the panel gave at the start is not on the state: %s", raw)
	}
	srv.Update(id, func(th *codextest.Thread) { th.Name = "Look into the router" })
	until(t, "the title codex gave to take the panel's name off the state", func() bool {
		st, _, _ := stateOf(t, id)
		return st.Title == ""
	})
}

// A first message starts the first turn at once, with the effort of the map
// when it chose one.
func TestAFirstMessageStartsTheFirstTurnAtTheEffort(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()

	id, err := l.Start(ctx, Begin{CWD: "/srv/proj", Message: "run the tests", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Start(ctx, Begin{CWD: "/srv/proj", Message: "and the linter"}); err != nil {
		t.Fatal(err)
	}
	turns := srv.Calls("turn/start")
	if len(turns) != 2 {
		t.Fatalf("turn/start went %d times", len(turns))
	}
	first := params(t, turns[0])
	input, _ := json.Marshal(first["input"])
	if first["threadId"] != id || first["effort"] != "high" ||
		string(input) != `[{"text":"run the tests","text_elements":[],"type":"text"}]` {
		t.Errorf("the first turn went as %v", first)
	}
	if _, set := params(t, turns[1])["effort"]; set {
		t.Errorf("a map with no effort sent one: %s", turns[1])
	}
}

// The panel stays a client of a thread it holds — nothing else holds it, and
// the daemon unloads a thread nobody holds — and leaves one it does not.
// Closed, the held thread is interrupted where a turn runs, left, unmarked,
// and kept off the map until the daemon unloads it; a turn another client
// starts in it brings it back.
func TestAHeldThreadIsLeftOnlyWhenClosed(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	ctx := context.Background()

	held, err := l.Start(ctx, Begin{CWD: "/srv/proj", Message: "go", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := l.Start(ctx, Begin{CWD: "/srv/proj"})
	if err != nil {
		t.Fatal(err)
	}
	srv.Set(held, "idle")
	until(t, "the link to leave the thread it does not hold", func() bool { return !srv.Subscribed(loose) })
	polls()
	if !srv.Subscribed(held) {
		t.Fatal("the link left a thread it holds")
	}
	if _, err := os.Stat(heldPath("acme", held)); err != nil {
		t.Errorf("the held thread is not marked: %v", err)
	}

	srv.Running(held, "turn-9")
	interrupted, err := l.Close(ctx, held)
	if err != nil || !interrupted {
		t.Fatalf("the close: %v, interrupted %v", err, interrupted)
	}
	stops := srv.Calls("turn/interrupt")
	if len(stops) != 1 || params(t, stops[0])["turnId"] != "turn-9" {
		t.Errorf("turn/interrupt went as %s", stops)
	}
	if srv.Subscribed(held) {
		t.Error("the closed thread was not left")
	}
	if _, err := os.Stat(heldPath("acme", held)); !os.IsNotExist(err) {
		t.Errorf("the mark of the closed thread stayed: %v", err)
	}
	polls()
	if _, _, ok := stateOf(t, held); ok {
		t.Error("the closed thread came back on the map before the daemon unloaded it")
	}

	srv.Running(held, "turn-10")
	until(t, "a turn of another client to bring the thread back", written(t, held))
}

// The panel takes back the threads it held when the executor starts again,
// before the daemon unloads them; a mark of a thread the daemon no longer has
// is dropped.
func TestTheNextExecutorTakesBackTheHeldThreads(t *testing.T) {
	srv, l := linked(t)
	ready(t, l)
	id, err := l.Start(context.Background(), Begin{CWD: "/srv/proj", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	gone := "019a1f00-0000-7000-8000-00000000dead"
	if err := os.WriteFile(heldPath("acme", gone), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	next := NewLink(srv.Home, "acme")
	srv.Drop(id)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		next.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	until(t, "the next link to join the held thread", func() bool {
		for _, raw := range srv.Calls("thread/resume") {
			if params(t, raw)["threadId"] == id {
				return srv.Subscribed(id)
			}
		}
		return false
	})
	until(t, "the mark of an unloaded thread to go", func() bool {
		_, err := os.Stat(heldPath("acme", gone))
		return os.IsNotExist(err)
	})
	if _, err := os.Stat(heldPath("acme", id)); err != nil {
		t.Errorf("the mark of the held thread went: %v", err)
	}
}

// Waiting for the link dials at once rather than at the next round: a daemon
// the executor has just started is used within the action that started it.
func TestReadyDialsAtOnce(t *testing.T) {
	was := redialEvery
	redialEvery = time.Hour
	defer func() { redialEvery = was }()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := codextest.New(t)
	srv.Close()
	l := NewLink(srv.Home, "acme")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	time.Sleep(50 * time.Millisecond)
	if l.Up() {
		t.Fatal("a closed daemon is up")
	}
	srv.Start(t)
	if !l.Up() {
		t.Fatal("a daemon listening is not up")
	}
	wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := l.Ready(wait); err != nil {
		t.Fatalf("the link waited for the next round: %v", err)
	}
}

// A close may land while a poll reads the thread, between what the daemon said
// and what the link does with it. Closed before the poll takes what it read,
// the thread is not taken back; closed after, the link finds nothing to keep
// and goes on, its lock free for the next caller.
func TestAThreadClosedWhileAPollReadsItStaysGone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	l := NewLink(t.TempDir(), "acme")
	info := threadInfo{ID: "th-closed", Status: threadStatus{Type: "idle"}}

	l.closed[info.ID] = ""
	if l.seen(info, time.Now()) || l.threads[info.ID] != nil {
		t.Error("a poll took back a thread the panel closed while it was read")
	}

	delete(l.closed, info.ID)
	if !l.seen(info, time.Now()) {
		t.Fatal("a poll did not take a thread nobody closed")
	}
	l.remove(info.ID)
	done := make(chan any)
	go func() {
		defer func() { done <- recover() }()
		l.keep(context.Background(), nil, info.ID)
	}()
	select {
	case broke := <-done:
		if broke != nil {
			t.Fatalf("keep of a thread closed since it was seen: %v", broke)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("keep of a thread closed since it was seen did not return")
	}
	if !l.mu.TryLock() {
		t.Fatal("the link's lock stayed held")
	}
	l.mu.Unlock()
}
