package codex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/codex/codextest"
)

// run keeps a link until the test ends.
func run(t *testing.T, l *Link) {
	t.Helper()
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
}

func turnTexts(t *testing.T, srv *codextest.Server) []string {
	t.Helper()
	var out []string
	for _, raw := range srv.Calls("turn/start") {
		var p struct {
			Input []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Path string `json:"path"`
			} `json:"input"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, in := range p.Input {
			parts = append(parts, in.Type+":"+in.Text+in.Path)
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

// A message taken back before the thread is free never goes; one that went
// is not there to take back.
func TestUnqueueTakesAMessageBackBeforeItGoes(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Running(threadA, "turn-tui")
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		if _, err := l.Send(ctx, threadA, Message{ID: id, Text: id + " words"}); err != nil {
			t.Fatal(err)
		}
	}
	if !l.Unqueue(threadA, "first") {
		t.Fatal("a waiting message was not found to take back")
	}
	if l.Unqueue(threadA, "first") {
		t.Error("a message was taken back twice")
	}
	stateSays(t, threadA, "the queue of one", func(st State) bool { return st.Queue == 1 })

	srv.Set(threadA, "idle")
	until(t, "the message left to go", func() bool { return len(srv.Calls("turn/start")) == 1 })
	if got := turnTexts(t, srv); got[0] != "text:second words" {
		t.Errorf("the queue sent %q", got)
	}
	if l.Unqueue(threadA, "second") {
		t.Error("a message that went as a turn was taken back")
	}
}

// The daemon answers turn/start before the thread turns active: a message
// sent in that moment finds the turn by the list of turns and waits for it,
// rather than going into it as a second turn/start would.
func TestATurnJustStartedCountsAsBusy(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Lag()
	ctx := context.Background()
	if place, err := l.Send(ctx, threadA, Message{Text: "first"}); err != nil || place != 0 {
		t.Fatalf("the first message: place %d, %v", place, err)
	}
	place, err := l.Send(ctx, threadA, Message{Text: "second"})
	if err != nil || place != 1 {
		t.Fatalf("a message right after a turn started: place %d, %v", place, err)
	}
	time.Sleep(5 * pollEvery)
	if n := len(srv.Calls("turn/start")); n != 1 {
		t.Errorf("a turn that had only just started got %d more turns", n-1)
	}
}

// A message waiting for a turn another client has just started keeps the link
// a client of the thread: the thread does not say it is active yet, and a link
// that left it would join it again at the next round, and leave, and join.
func TestAMessageWaitingKeepsTheLinkOnTheThread(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Starting(threadA, "turn-tui")
	if place, err := l.Send(context.Background(), threadA, Message{Text: "after it"}); err != nil || place != 1 {
		t.Fatalf("place %d, %v", place, err)
	}
	until(t, "the link to join", func() bool { return srv.Subscribed(threadA) })
	time.Sleep(8 * pollEvery)
	if n := len(srv.Calls("thread/unsubscribe")); n != 0 || len(srv.Calls("turn/start")) != 0 {
		t.Errorf("while a message waited the link left the thread %d times, turn/start %s", n, srv.Calls("turn/start"))
	}
	srv.Set(threadA, "idle")
	until(t, "the message to go", func() bool { return len(srv.Calls("turn/start")) == 1 })
}

// A message never overtakes the ones that wait before it: a thread found free
// before the queue went on takes the oldest first.
func TestAMessageGoesBehindTheOnesThatWait(t *testing.T) {
	was := pollEvery
	pollEvery = time.Hour
	t.Cleanup(func() { pollEvery = was })
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Running(threadA, "turn-tui")
	ctx := context.Background()
	if _, err := l.Send(ctx, threadA, Message{Text: "older"}); err != nil {
		t.Fatal(err)
	}
	until(t, "the link to join the busy thread", func() bool { return srv.Subscribed(threadA) })
	// The turn ends with no word to the link: the queue has not gone on yet.
	srv.Drop(threadA)
	srv.Set(threadA, "idle")
	if place, err := l.Send(ctx, threadA, Message{Text: "newer"}); err != nil || place != 2 {
		t.Fatalf("a message behind a waiting one: place %d, %v", place, err)
	}
	until(t, "the queue to go on", func() bool { return len(srv.Calls("turn/start")) == 1 })
	if got := turnTexts(t, srv); got[0] != "text:older" {
		t.Errorf("the first turn went with %q", got)
	}
}

// A thread that turns free takes the next message of the queue as the daemon
// says so, not at the next round of the poll.
func TestAFreedThreadTakesItsQueueAtOnce(t *testing.T) {
	was := pollEvery
	pollEvery = time.Hour
	t.Cleanup(func() { pollEvery = was })
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Running(threadA, "turn-tui")
	if _, err := l.Send(context.Background(), threadA, Message{Text: "next"}); err != nil {
		t.Fatal(err)
	}
	until(t, "the link to join the busy thread", func() bool { return srv.Subscribed(threadA) })
	srv.Set(threadA, "idle")
	until(t, "the queue to go once the thread is free", func() bool { return len(srv.Calls("turn/start")) == 1 })
}

// Pictures go into the turn as files codex reads itself, after the words.
func TestPicturesGoWithTheMessageAsLocalImages(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	if _, err := l.Send(context.Background(), threadA,
		Message{Text: "what is wrong here", Images: []string{"/srv/files/a.png", "/srv/files/b.jpg"}}); err != nil {
		t.Fatal(err)
	}
	if got := turnTexts(t, srv); len(got) != 1 ||
		got[0] != "text:what is wrong here localImage:/srv/files/a.png localImage:/srv/files/b.jpg" {
		t.Errorf("the turn went with %q", got)
	}
}

// The queue is the panel's and not the connection's: a link that dialled
// the daemon again, and an executor started again, deliver what waited.
func TestTheQueueOutlivesTheConnectionAndTheExecutor(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Running(threadA, "turn-tui")
	ctx := context.Background()
	if _, err := l.Send(ctx, threadA, Message{ID: "one", Text: "after the drop"}); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	until(t, "the link to lose the daemon", func() bool { return !written(t, threadA)() })
	srv.Start(t)
	until(t, "the link to come back with the queue", func() bool {
		st, _, ok := stateOf(t, threadA)
		return ok && st.Queue == 1
	})
	srv.Set(threadA, "idle")
	until(t, "the message to go after the drop", func() bool { return len(srv.Calls("turn/start")) == 1 })

	if _, err := l.Send(ctx, threadA, Message{ID: "two", Text: "after the restart"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outboxPath("acme", threadA)); err != nil {
		t.Fatalf("the queue is not on the disk: %v", err)
	}
	// Another executor reads the queue the way a new one would; the first
	// stays connected, as a holder outlives a restart of the executor too.
	again := NewLink(srv.Home, "acme")
	if again.Unqueue(threadA, "absent") || len(again.boxes[threadA].Queue) != 1 {
		t.Fatalf("an executor started again found %+v", again.boxes)
	}
	if again.boxes[threadA].Queue[0].Text != "after the restart" {
		t.Errorf("an executor started again found %+v", again.boxes[threadA].Queue)
	}
}

// What waits goes with its thread: closed by the panel, or unloaded by the
// daemon.
func TestAQueueGoesWithItsThread(t *testing.T) {
	srv, l, id := started(t, Begin{Message: "first"})
	ctx := context.Background()
	if _, err := l.Send(ctx, id, Message{ID: "x", Text: "waits"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outboxPath("acme", id)); !os.IsNotExist(err) {
		t.Errorf("the queue of a closed thread is still on the disk: %v", err)
	}
	srv.Set(id, "idle")
	time.Sleep(5 * pollEvery)
	if n := len(srv.Calls("turn/start")); n != 1 {
		t.Errorf("a closed thread got %d turns", n)
	}

	srv.Add(idle(threadA))
	until(t, "the second thread", written(t, threadA))
	srv.Running(threadA, "turn-tui")
	if _, err := l.Send(ctx, threadA, Message{ID: "y", Text: "waits too"}); err != nil {
		t.Fatal(err)
	}
	srv.Unload(threadA)
	until(t, "the queue of an unloaded thread to go", func() bool {
		_, err := os.Stat(outboxPath("acme", threadA))
		return os.IsNotExist(err)
	})
}
