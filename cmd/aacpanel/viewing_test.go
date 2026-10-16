package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/auth"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	"aacpanel/internal/store"
	"aacpanel/internal/testdb"
)

// pollProbe is a server over the test database with the live snapshot of one
// session, sentinel, and a device of its own to poll it from.
type pollProbe struct {
	srv    *Server
	db     *store.Store
	device int64
}

func newPollProbe(t *testing.T, ctx context.Context, name string) pollProbe {
	t.Helper()
	db, err := store.New(testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Open(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Pool()
	if err != nil {
		t.Fatal(err)
	}

	var device int64
	err = pool.QueryRow(ctx, `
		INSERT INTO devices (name, credential_id, public_key)
		VALUES ($1, $2, $3) RETURNING id`,
		name, []byte("cred-"+name), []byte("key-"+name)).Scan(&device)
	if err != nil {
		t.Fatalf("the device for the probe: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM devices WHERE id = $1`, device) })

	guard, err := auth.New("0123456789abcdef0123456789abcdef", auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	if err != nil {
		t.Fatalf("the session service: %v", err)
	}
	guard.UseDevices(aliveDevice{})
	return pollProbe{srv: &Server{auth: guard, db: db, host: hostWith(t, liveSnapshot)}, db: db, device: device}
}

// poll asks for the host as the screen of the device does and returns the answer.
func (p pollProbe) poll(t *testing.T, path string) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	p.srv.auth.IssueDevice(w, false, p.device)
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	got := httptest.NewRecorder()
	p.srv.apiHost(got, r)
	if got.Code != http.StatusOK {
		t.Fatalf("the poll %s answered %d: %s", path, got.Code, got.Body.String())
	}
	return got.Body.Bytes()
}

func TestHostPollMarksTheSessionOnScreenPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := newPollProbe(t, ctx, "probe phone")

	p.poll(t, "/api/host?viewing=sentinel")
	seen, err := p.db.SessionViews().Watching(ctx, "sentinel", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != p.device {
		t.Fatalf("the poll did not say what the device is looking at: %v", seen)
	}

	p.poll(t, "/api/host")
	seen, err = p.db.SessionViews().Watching(ctx, "sentinel", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("a poll without a session on the screen did not clear the mark: %v", seen)
	}
}

// sentinelID is the conversation of the session of liveSnapshot.
const sentinelID = "567f4d24-cd5f-48fa-bdc1-04c89d203494"

// calledOf reads the time of the call the poll puts on the session of the
// snapshot, empty when it puts none.
func calledOf(t *testing.T, body []byte) string {
	t.Helper()
	var snap struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			Called    string `json:"called"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("the poll answered %s: %v", body, err)
	}
	for _, s := range snap.Sessions {
		if s.SessionID == sentinelID {
			return s.Called
		}
	}
	t.Fatalf("the poll lost the session: %s", body)
	return ""
}

// A call marks its session in the lists of every device until one of them
// opens the session; a call made while a device has it on screen marks
// nothing.
func TestACallMarksItsSessionUntilTheSessionIsOpenedPG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := newPollProbe(t, ctx, "probe call")
	pool, err := p.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	forget := func() {
		pool.Exec(context.Background(), `DELETE FROM session_calls WHERE conversation = $1`, sentinelID)
	}
	forget()
	t.Cleanup(forget)

	w := &watcher{srv: p.srv}
	sessions := []notify.Session{{ID: sentinelID, Name: "sentinel"}}
	call := func(at string) {
		w.markCalls(ctx, []chat.SessionNote{{SessionID: sentinelID, Text: "Deploy is blocked", At: at}}, sessions)
	}

	if got := calledOf(t, p.poll(t, "/api/host")); got != "" {
		t.Fatalf("a session that never called is marked as called at %s", got)
	}

	const first = "2026-10-16T09:00:00Z"
	call(first)
	if got := calledOf(t, p.poll(t, "/api/host")); got != first {
		t.Fatalf("the session that called is marked %q, wanted %q", got, first)
	}
	call(first)
	if got := calledOf(t, p.poll(t, "/api/host")); got != first {
		t.Fatalf("the session listed again is marked %q, wanted %q", got, first)
	}

	if got := calledOf(t, p.poll(t, "/api/host?viewing=sentinel")); got != "" {
		t.Fatalf("the session is on screen and still marked as called at %s", got)
	}
	p.poll(t, "/api/host")
	call(first)
	if got := calledOf(t, p.poll(t, "/api/host")); got != "" {
		t.Fatalf("the collector listed the call again and the session the person opened is marked again at %s", got)
	}

	// The session is on screen when the next call comes: the device marked
	// it a moment ago, and the call is seen as it comes.
	p.poll(t, "/api/host?viewing=sentinel")
	call("2026-10-16T09:05:00Z")
	if got := calledOf(t, p.poll(t, "/api/host")); got != "" {
		t.Fatalf("a call made with the session on screen marks it, at %s", got)
	}
}
