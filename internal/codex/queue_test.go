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

// A message taken back before a turn takes it never goes; one that went is
// not there to take back.
func TestUnqueueTakesAMessageBackBeforeItGoes(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	if err := l.Compact(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		if sent, err := l.Send(ctx, threadA, Message{ID: id, Text: id + " words"}); err != nil || sent.Place == 0 {
			t.Fatalf("a message to a compaction: %+v, %v", sent, err)
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
// sent in that moment finds the turn by the list of turns and goes into it,
// rather than as a second turn/start.
func TestATurnJustStartedCountsAsBusy(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Lag()
	ctx := context.Background()
	if sent, err := l.Send(ctx, threadA, Message{Text: "first"}); err != nil || sent != (Sent{}) {
		t.Fatalf("the first message: %+v, %v", sent, err)
	}
	sent, err := l.Send(ctx, threadA, Message{Text: "second"})
	if err != nil || sent != (Sent{Steered: true}) {
		t.Fatalf("a message right after a turn started: %+v, %v", sent, err)
	}
	if steers := srv.Calls("turn/steer"); len(steers) != 1 || params(t, steers[0])["expectedTurnId"] != "turn-1" {
		t.Errorf("the message went as %s", steers)
	}
	time.Sleep(5 * pollEvery)
	if n := len(srv.Calls("turn/start")); n != 1 {
		t.Errorf("a turn that had only just started got %d more turns", n-1)
	}
}

// A message waiting for a compaction that has just started keeps the link a
// client of the thread: the thread does not say it is active yet, and a link
// that left it would join it again at the next round, and leave, and join.
func TestAMessageWaitingKeepsTheLinkOnTheThread(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	srv.Lag()
	ctx := context.Background()
	if err := l.Compact(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	if sent, err := l.Send(ctx, threadA, Message{Text: "after it"}); err != nil || sent.Place != 1 {
		t.Fatalf("a message to a compaction: %+v, %v", sent, err)
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
// before the queue went on takes the oldest first, and the newer one goes into
// the turn it started.
func TestAMessageGoesBehindTheOnesThatWait(t *testing.T) {
	was := pollEvery
	pollEvery = time.Hour
	t.Cleanup(func() { pollEvery = was })
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	if err := l.Compact(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	if sent, err := l.Send(ctx, threadA, Message{ID: "older", Text: "older"}); err != nil || sent.Place != 1 {
		t.Fatalf("a message to a compaction: %+v, %v", sent, err)
	}
	// The daemon counts the subscription before join returns and the poll
	// tries the queue. With polling paused, only that poll's final save writes
	// the busy state; enqueue still writes the idle state read at the start.
	stateSays(t, threadA, "the busy state after joining the thread", func(st State) bool {
		return st.Busy && st.Queue == 1
	})
	// The compaction ends with no word to the link: the queue has not gone on yet.
	srv.Drop(threadA)
	srv.Set(threadA, "idle")
	if sent, err := l.Send(ctx, threadA, Message{ID: "newer", Text: "newer"}); err != nil || sent.Place != 2 {
		t.Fatalf("a message behind a waiting one: %+v, %v", sent, err)
	}
	until(t, "the queue to go on", func() bool { return len(steersOf(t, srv, "newer")) == 1 })
	if got := turnTexts(t, srv); len(got) != 1 || got[0] != "text:older" {
		t.Errorf("the queue started turns with %q", got)
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
	ctx := context.Background()
	if err := l.Compact(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	if sent, err := l.Send(ctx, threadA, Message{Text: "next"}); err != nil || sent.Place != 1 {
		t.Fatalf("a message to a compaction: %+v, %v", sent, err)
	}
	// Wait past join and drain so only the notification can wake the queue.
	stateSays(t, threadA, "the busy state after joining the thread", func(st State) bool {
		return st.Busy && st.Queue == 1
	})
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
// the daemon again, and an executor started again, deliver what waited, and
// the disk keeps nothing of it once a turn took it.
func TestTheQueueOutlivesTheConnectionAndTheExecutor(t *testing.T) {
	srv, l := linked(t, idle(threadA))
	until(t, "the state file", written(t, threadA))
	ctx := context.Background()
	if err := l.Compact(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	if sent, err := l.Send(ctx, threadA, Message{ID: "one", Text: "after the drop"}); err != nil || sent.Place != 1 {
		t.Fatalf("a message to a compaction: %+v, %v", sent, err)
	}
	// Another executor reads the queue the way a new one would; the first
	// stays connected, as a holder outlives a restart of the executor too.
	again := NewLink(srv.Home, "acme")
	if again.Unqueue(threadA, "absent") || len(again.boxes[threadA].Queue) != 1 ||
		again.boxes[threadA].Queue[0].Text != "after the drop" {
		t.Fatalf("an executor started again found %+v", again.boxes)
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
	until(t, "the queue to leave the disk", func() bool {
		_, err := os.Stat(outboxPath("acme", threadA))
		return os.IsNotExist(err)
	})
}

// What waits goes with its thread: closed by the panel, or unloaded by the
// daemon.
func TestAQueueGoesWithItsThread(t *testing.T) {
	srv, l, id := started(t, Begin{Message: "first"})
	ctx := context.Background()
	srv.Set(id, "idle")
	if err := l.Compact(ctx, id); err != nil {
		t.Fatal(err)
	}
	if sent, err := l.Send(ctx, id, Message{ID: "x", Text: "waits"}); err != nil || sent.Place != 1 {
		t.Fatalf("a message to a compaction: %+v, %v", sent, err)
	}
	if _, err := l.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outboxPath("acme", id)); !os.IsNotExist(err) {
		t.Errorf("the queue of a closed thread is still on the disk: %v", err)
	}
	srv.Set(id, "idle")
	srv.Add(idle(threadA))
	// Seeing the next thread means the poll passed the closed one too.
	until(t, "the second thread", written(t, threadA))
	if n := len(srv.Calls("turn/start")); n != 1 {
		t.Errorf("a closed thread got %d turns", n)
	}

	if err := l.Compact(ctx, threadA); err != nil {
		t.Fatal(err)
	}
	if sent, err := l.Send(ctx, threadA, Message{ID: "y", Text: "waits too"}); err != nil || sent.Place != 1 {
		t.Fatalf("a message to a compaction: %+v, %v", sent, err)
	}
	// A pending subscription would resume the fake thread after Unload.
	// The daemon loses a queued thread only after the link has joined it.
	until(t, "the queue's subscription", func() bool { return srv.Subscribed(threadA) })
	srv.Unload(threadA)
	until(t, "the queue of an unloaded thread to go", func() bool {
		_, err := os.Stat(outboxPath("acme", threadA))
		return os.IsNotExist(err)
	})
}
