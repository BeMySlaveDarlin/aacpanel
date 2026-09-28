package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/notify"
)

const (
	callerID = "4d89ed41-0000-4000-8000-000000000001"
	laterID  = "4d89ed41-0000-4000-8000-000000000002"
)

// fakeCollector answers the requests for the calls on the chat socket the way
// the collector does: at once for a count the panel has not seen, and
// otherwise when a call is taken or the wait is out.
type fakeCollector struct {
	path string

	mu      sync.Mutex
	seq     int64
	notes   []map[string]string
	changed chan struct{}
	waiting chan struct{}
}

func startCollector(t *testing.T) *fakeCollector {
	t.Helper()
	c := &fakeCollector{
		path:    socketPath(t, "chat.sock"),
		changed: make(chan struct{}),
		waiting: make(chan struct{}, 16),
	}
	l, err := net.Listen("unix", c.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go c.serve(conn)
		}
	}()
	return c
}

func (c *fakeCollector) serve(conn net.Conn) {
	defer conn.Close()
	var req struct {
		Notes *struct {
			After *int64  `json:"after"`
			Wait  float64 `json:"wait"`
		} `json:"notes"`
	}
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	if req.Notes == nil {
		json.NewEncoder(conn).Encode(map[string]any{"ok": false, "error": "only the calls are served here"})
		return
	}
	c.mu.Lock()
	changed := c.changed
	idle := req.Notes.After != nil && *req.Notes.After == c.seq
	c.mu.Unlock()
	if idle {
		select {
		case c.waiting <- struct{}{}:
		default:
		}
		select {
		case <-changed:
		case <-time.After(time.Duration(req.Notes.Wait * float64(time.Second))):
		}
	}
	c.mu.Lock()
	reply := map[string]any{"ok": true, "seq": c.seq, "notes": append([]map[string]string{}, c.notes...)}
	c.mu.Unlock()
	json.NewEncoder(conn).Encode(reply)
}

// call is a session calling the person: the collector takes it and wakes
// whoever waits.
func (c *fakeCollector) call(session, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes = append(c.notes, map[string]string{
		"sessionId": session, "text": text, "at": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	c.seq++
	close(c.changed)
	c.changed = make(chan struct{})
}

// awaitWaiting gives the panel up to a second to be waiting at the collector.
func (c *fakeCollector) awaitWaiting() {
	select {
	case <-c.waiting:
	case <-time.After(time.Second):
	}
}

// pushStore stands in for the database of the push queue. A worker of the
// queue lists the subscriptions for each message it takes, and that moment is
// what the tests time.
type pushStore struct {
	mu    sync.Mutex
	der   []byte
	taken chan time.Time
}

func (s *pushStore) Subscriptions(context.Context) ([]notify.Subscription, error) {
	s.taken <- time.Now()
	return nil, nil
}
func (s *pushStore) DropSubscription(context.Context, int64) error            { return nil }
func (s *pushStore) SubscriptionSent(context.Context, int64, time.Time) error { return nil }
func (s *pushStore) SubscriptionFailed(context.Context, int64, string) error  { return nil }
func (s *pushStore) SavePushKeys(_ context.Context, private, _ []byte) error {
	s.setKeys(private)
	return nil
}
func (s *pushStore) PushKeys(context.Context) ([]byte, error) { return s.keys(), nil }
func (s *pushStore) setKeys(der []byte)                       { s.mu.Lock(); s.der = der; s.mu.Unlock() }
func (s *pushStore) keys() []byte                             { s.mu.Lock(); defer s.mu.Unlock(); return s.der }

type logTail struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logTail) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logTail) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func tailLog(t *testing.T) *logTail {
	t.Helper()
	tail := &logTail{}
	was := log.Writer()
	log.SetOutput(tail)
	t.Cleanup(func() { log.SetOutput(was) })
	return tail
}

// writeSnapshot puts the sessions into the snapshot the way the collector
// does, in one rename.
func writeSnapshot(t *testing.T, path string, rows ...string) {
	t.Helper()
	now := time.Now().Unix()
	body := fmt.Sprintf(`{"at": %d, "sessionsAt": %d, "sessions": [%s]}`, now, now, strings.Join(rows, ","))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func sessionRow(name, id, note string) string {
	row := fmt.Sprintf(`{"session": %q, "sessionId": %q, "cwd": "/srv/proj/%s", "profile": "personal", "status": "busy"`,
		name, id, name)
	if note != "" {
		row += fmt.Sprintf(`, "note": {"text": %q, "at": %q}`, note, time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	}
	return row + "}"
}

// watchWith runs the watcher of a panel as it runs in production, tick and
// all, over the snapshot at path and the collector c, with a push queue whose
// takings land in the returned store.
func watchWith(t *testing.T, path string, c *fakeCollector) *pushStore {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	store := &pushStore{taken: make(chan time.Time, 16)}
	push := notify.New(store, "")
	go push.Run(ctx)

	w := newWatcher(&Server{host: host.NewReader(path), chat: chat.New(c.path), push: push}, nil)
	// The panel has started before and looked at the host: the first look
	// after a first start takes what stands in silence.
	w.tracker.Step(ctx, notify.Report{})
	go w.Run(ctx)
	return store
}

func taken(t *testing.T, store *pushStore, within time.Duration, what string) time.Time {
	t.Helper()
	select {
	case at := <-store.taken:
		return at
	case <-time.After(within):
		t.Fatalf("%s has not reached the push queue in %s — it waits for the look of the host, which comes every %s",
			what, within, watchEvery)
		return time.Time{}
	}
}

func nothingTaken(t *testing.T, store *pushStore, during time.Duration, why string) {
	t.Helper()
	select {
	case <-store.taken:
		t.Errorf("a push went out %s", why)
	case <-time.After(during):
	}
}

// A call reaches the push queue the moment the collector takes it, and not at
// the next look of the host: "the person has been called" followed by a
// minute of silence tells the model a lie. The look keeps its tick of
// production here, and the snapshot carries the call at once, which is the
// best the snapshot could ever do; the call still has to beat the tick.
func TestACallReachesThePushQueueBeforeTheTick(t *testing.T) {
	if watchEvery < 10*time.Second {
		t.Fatalf("the tick of the watcher is %s: a call arriving in time proves nothing about its path", watchEvery)
	}
	said := tailLog(t)
	path := filepath.Join(t.TempDir(), "state.json")
	writeSnapshot(t, path, sessionRow("aacpanel", callerID, ""), sessionRow("lms", laterID, ""))
	c := startCollector(t)
	store := watchWith(t, path, c)
	c.awaitWaiting()

	made := time.Now()
	c.call(callerID, "stuck on the migration, need you")
	writeSnapshot(t, path, sessionRow("aacpanel", callerID, "stuck on the migration, need you"),
		sessionRow("lms", laterID, ""))

	took := taken(t, store, 3*time.Second, "the call").Sub(made)
	if took > 2*time.Second {
		t.Errorf("the call reached the push queue %s after the collector took it", took.Round(time.Millisecond))
	}
	t.Logf("the call reached the push queue %s after the collector took it; the tick is %s", took, watchEvery)
	if !strings.Contains(said.String(), "notify: aacpanel is calling — stuck on the migration, need you · personal · aacpanel") {
		t.Errorf("the push is not the call of the session it came from:\n%s", said)
	}

	// The board lists the first call as long as it stands. A second session
	// calling wakes the panel with both on it, and only the second is news.
	c.awaitWaiting()
	c.call(laterID, "the stand is down")
	taken(t, store, 3*time.Second, "the second call")
	nothingTaken(t, store, 500*time.Millisecond, "for a call already carried: the phone buzzes each time the board is read")
	if n := strings.Count(said.String(), "aacpanel is calling"); n != 1 {
		t.Errorf("the first call was pushed %d times:\n%s", n, said)
	}
	if !strings.Contains(said.String(), "notify: lms is calling — the stand is down") {
		t.Errorf("the second call is not the one pushed:\n%s", said)
	}
}

// A session the snapshot does not show yet has no name for the push and
// nothing for a tap to open. Its call waits for it and goes out once it shows,
// without waiting for another call to wake the panel.
func TestACallWaitsForItsSessionToReachTheSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	writeSnapshot(t, path, sessionRow("aacpanel", callerID, ""))
	c := startCollector(t)
	store := watchWith(t, path, c)
	c.awaitWaiting()

	c.call(laterID, "the stand is down")
	nothingTaken(t, store, time.Second, "for a session nobody sees: it has no name and a tap opens nothing")

	shown := time.Now()
	writeSnapshot(t, path, sessionRow("aacpanel", callerID, ""), sessionRow("lms", laterID, ""))
	if took := taken(t, store, 5*time.Second, "the call of a session that showed").Sub(shown); took > 4*time.Second {
		t.Errorf("the call went out %s after its session showed", took.Round(time.Millisecond))
	}
}
