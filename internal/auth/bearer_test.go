package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func bearerOf(t *testing.T, a *Service, r *http.Request) string {
	t.Helper()
	token, err := a.IssueBearer(r)
	if err != nil {
		t.Fatalf("bearer: %v", err)
	}
	return token
}

func withBearer(path, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestBearerKeepsIssuedAndRefreshesSeen(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	issued := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	token := bearerOf(t, a, sessionRequest(t, a, issued, time.Now().Add(-10*time.Minute), 1))

	info, ok := a.parse(token)
	if !ok {
		t.Fatal("a bearer does not parse through the same parse as a cookie")
	}
	if !info.issued.Equal(issued) {
		t.Errorf("issued %v, was %v — the bearer extended the absolute limit", info.issued, issued)
	}
	if time.Since(info.seen) > time.Minute {
		t.Errorf("seen %v — the activity mark was not set on issue", info.seen)
	}
	if info.device != 1 {
		t.Errorf("device %d, was 1", info.device)
	}
	if err := a.Verify(withBearer("/api/tree", token)); err != nil {
		t.Errorf("a bearer was not accepted on /api/: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	r.Header.Set("Authorization", "bearer "+token)
	if err := a.Verify(r); err != nil {
		t.Errorf("the scheme in lower case: %v", err)
	}
}

func TestBearerOnlyOnAPIAndOnlyFromCookie(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	token := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), 1))

	if err := a.Verify(withBearer("/app", token)); !errors.Is(err, ErrNoSession) {
		t.Errorf("a bearer let through to a page: %v", err)
	}
	if _, err := a.IssueBearer(withBearer("/api/session/token", token)); !errors.Is(err, ErrNoSession) {
		t.Errorf("a bearer issued itself a new bearer: %v", err)
	}
	if _, err := a.IssueBearer(httptest.NewRequest(http.MethodGet, "/api/session/token", nil)); !errors.Is(err, ErrNoSession) {
		t.Errorf("a bearer without a session: %v", err)
	}
}

func TestBearerRejectsForeignSignature(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	token := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), 1))

	payload, _, _ := strings.Cut(token, ".")
	other, err := New("another-secret-0123456789abcdef", SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	forged := other.sign(payload)
	if err := a.Verify(withBearer("/api/tree", forged)); !errors.Is(err, ErrNoSession) {
		t.Errorf("a bearer with a signature from another service was accepted: %v", err)
	}
	if err := a.Verify(withBearer("/api/tree", payload+".")); !errors.Is(err, ErrNoSession) {
		t.Errorf("a bearer without a signature was accepted: %v", err)
	}
	tampered := strings.Replace(token, "|1.", "|2.", 1)
	if tampered == token {
		t.Fatal("fixture: the device number is not present in the payload")
	}
	if err := a.Verify(withBearer("/api/tree", tampered)); !errors.Is(err, ErrNoSession) {
		t.Errorf("a bearer with a swapped device was accepted: %v", err)
	}
}

func TestBearerChecksExpiryAndDevice(t *testing.T) {
	a, devices := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	if _, err := a.IssueBearer(sessionRequest(t, a, time.Now().Add(-13*time.Hour), time.Now(), 1)); !errors.Is(err, ErrExpired) {
		t.Errorf("a bearer was issued for an expired session: %v", err)
	}
	short, _ := testService(t, SessionTTL{Idle: time.Nanosecond, Absolute: time.Hour})
	stale := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), 1))
	time.Sleep(time.Millisecond)
	if err := short.Verify(withBearer("/api/tree", stale)); !errors.Is(err, ErrIdle) {
		t.Errorf("a bearer that went stale: %v", err)
	}
	token := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), 1))
	if err := devices.Revoke(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	a.ForgetDevice(1)
	if err := a.Verify(withBearer("/api/tree", token)); !errors.Is(err, ErrRevoked) {
		t.Errorf("a bearer of a revoked device: %v", err)
	}
}

func TestGuardDoesNotRefreshBearer(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 2 * time.Second, Absolute: time.Hour})
	token := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), 1))
	time.Sleep(300 * time.Millisecond)
	w := httptest.NewRecorder()
	if err := a.Guard(w, withBearer("/api/tree", token), true); err != nil {
		t.Fatalf("Guard on a bearer: %v", err)
	}
	if got := w.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("Guard on a bearer set a cookie: %q", got)
	}
}

func TestQueryTokenOnlyForStreams(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	token := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), 1))
	stream := httptest.NewRequest(http.MethodGet, "/api/stream?token="+url.QueryEscape(token), nil)
	if err := a.VerifyStream(stream); err != nil {
		t.Errorf("a stream by ?token=: %v", err)
	}
	if err := a.Verify(stream); !errors.Is(err, ErrNoSession) {
		t.Errorf("the ordinary check accepted ?token=: %v", err)
	}
	if _, ok := a.Deadline(stream, true); !ok {
		t.Error("a stream by ?token= has no deadline")
	}
	if _, ok := a.Deadline(stream, false); ok {
		t.Error("a deadline by ?token= was computed outside a stream")
	}
	page := httptest.NewRequest(http.MethodGet, "/app?token="+url.QueryEscape(token), nil)
	if err := a.VerifyStream(page); !errors.Is(err, ErrNoSession) {
		t.Errorf("a page by ?token=: %v", err)
	}
}

func TestSessionBearerIsNotATokenMiss(t *testing.T) {
	a, tokens := serviceWithToken(t, testToken)
	stale := bearerOf(t, a, sessionRequest(t, a, time.Now(), time.Now(), tokens.Device()))
	r := withBearer("/api/tree", stale)
	r.RemoteAddr = "203.0.113.7:1"
	for i := 0; i < 10; i++ {
		if tokens.Bearer(r) {
			t.Fatal("a bearer session passed for the long-lived token")
		}
	}
	if wait := a.Throttle("203.0.113.7"); wait > 0 {
		t.Errorf("ten bearer sessions in the header throttled token login for %s", wait)
	}
	bad := withBearer("/api/tree", "not-a-token-and-not-a-session")
	bad.RemoteAddr = "203.0.113.8:1"
	for i := 0; i < 5; i++ {
		tokens.Bearer(bad)
	}
	if a.Throttle("203.0.113.8") == 0 {
		t.Error("five misses in the header did not throttle anything — then this check checks nothing")
	}
}
