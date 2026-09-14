package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aacpanel/internal/auth"
)

type aliveDevice struct{ auth.DeviceStore }

func (aliveDevice) Exists(context.Context, int64) (bool, error) { return true, nil }

func testServer(t *testing.T, ttl auth.SessionTTL) *Server {
	t.Helper()
	guard, err := auth.New("0123456789abcdef0123456789abcdef", ttl)
	if err != nil {
		t.Fatalf("the session service: %v", err)
	}
	guard.UseDevices(aliveDevice{})
	return &Server{auth: guard}
}

func withSession(t *testing.T, s *Server, method, path string) *http.Request {
	t.Helper()
	w := httptest.NewRecorder()
	s.auth.IssueDevice(w, false, 1)

	r := httptest.NewRequest(method, path, nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}

func TestProtectLimitsHandlerToSessionDeadline(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})

	var deadline time.Time
	var ok bool
	h := s.protect(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok = r.Context().Deadline()
	})
	h.ServeHTTP(httptest.NewRecorder(), withSession(t, s, "GET", "/api/stream"))

	if !ok {
		t.Fatal("the handler got a context without a deadline: the stream would outlive the session")
	}
	if d := time.Until(deadline); d > 30*time.Minute+time.Second || d < 29*time.Minute {
		t.Fatalf("the stream deadline is %s, expected the idle limit", d)
	}
}

func TestProtectExplainsExpiry(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Nanosecond, Absolute: time.Hour})

	called := false
	h := s.protect(func(w http.ResponseWriter, r *http.Request) { called = true })

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withSession(t, s, "GET", "/app"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("an expired session on a page: %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login?reason=idle&next=%2Fapp" {
		t.Fatalf("the wrong place and no reason: %q", loc)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, withSession(t, s, "GET", "/api/tree"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("an expired session in the API: %d", w.Code)
	}
	var body struct {
		Error    string `json:"error"`
		Reason   string `json:"reason"`
		AfterSec int    `json:"after_sec"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, w.Body)
	}
	if body.Reason != "idle" || body.Error == "" {
		t.Fatalf("the reason is not named: %+v", body)
	}
	if body.AfterSec != 0 {
		t.Fatalf("the idle limit is %d s, expected a nanosecond (it rounds to zero)", body.AfterSec)
	}

	if called {
		t.Fatal("the handler ran with an expired session")
	}
}

func TestProtectRedirectsAnonymous(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	h := s.protect(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the handler ran without a session")
	})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/app", nil))
	if loc := w.Header().Get("Location"); loc != "/login?reason=none&next=%2Fapp" {
		t.Fatalf("an anonymous visitor was led to the wrong place: %q", loc)
	}
}

func TestStreamDoesNotRefreshSession(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: 2 * time.Second, Absolute: time.Hour})

	aged := func(path string) *http.Request {
		w := httptest.NewRecorder()
		s.auth.IssueDevice(w, false, 1)
		r := httptest.NewRequest("GET", path, nil)
		for _, c := range w.Result().Cookies() {
			r.AddCookie(c)
		}
		time.Sleep(300 * time.Millisecond)
		return r
	}

	w := httptest.NewRecorder()
	s.protect(func(http.ResponseWriter, *http.Request) {}).ServeHTTP(w, aged("/app"))
	if got := len(w.Result().Cookies()); got != 1 {
		t.Fatalf("an ordinary request did not extend the session: %d cookies — the test checks nothing", got)
	}

	w = httptest.NewRecorder()
	served := false
	s.protectStream(func(http.ResponseWriter, *http.Request) { served = true }).ServeHTTP(w, aged("/api/stream"))
	if !served {
		t.Fatal("the stream was not let through with a live session")
	}
	if got := len(w.Result().Cookies()); got != 0 {
		t.Fatalf("the stream extended the session: %d cookies", got)
	}
}

func TestStreamStillChecksSession(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Nanosecond, Absolute: time.Hour})

	w := httptest.NewRecorder()
	s.protectStream(func(http.ResponseWriter, *http.Request) {
		t.Fatal("the stream was opened on an expired session")
	}).ServeHTTP(w, withSession(t, s, "GET", "/api/stream"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("an expired session in a stream: %d", w.Code)
	}
}

func TestExpiryReportsConfiguredLimits(t *testing.T) {
	cases := map[string]struct {
		ttl    auth.SessionTTL
		reason string
	}{
		"idle limit":     {auth.SessionTTL{Idle: time.Second, Absolute: time.Hour}, "idle"},
		"absolute limit": {auth.SessionTTL{Idle: time.Second, Absolute: time.Second}, "expired"},
	}

	for name, c := range cases {
		s := testServer(t, c.ttl)
		r := withSession(t, s, "GET", "/api/tree")
		time.Sleep(1100 * time.Millisecond)

		w := httptest.NewRecorder()
		s.protect(func(http.ResponseWriter, *http.Request) {
			t.Fatalf("%s: the handler ran on an expired session", name)
		}).ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: the session did not expire, the response is %d", name, w.Code)
		}
		var body struct {
			Reason   string `json:"reason"`
			AfterSec int    `json:"after_sec"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: the response is not JSON: %v", name, err)
		}
		if body.Reason != c.reason {
			t.Fatalf("%s: the reason is %q, expected %q", name, body.Reason, c.reason)
		}
		if body.AfterSec != 1 {
			t.Fatalf("%s: the limit is %d s, expected one second", name, body.AfterSec)
		}
	}
}

func TestExpiredAfterUsesConfiguredTTL(t *testing.T) {
	ttl := auth.SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour}

	if got := expiredAfter(ttl, auth.ErrIdle); got != 1800 {
		t.Errorf("the idle limit is %d s, expected 1800", got)
	}
	if got := expiredAfter(ttl, auth.ErrExpired); got != 43200 {
		t.Errorf("the absolute limit is %d s, expected 43200", got)
	}
	for _, err := range []error{auth.ErrNoSession, auth.ErrRevoked} {
		if got := expiredAfter(ttl, err); got != 0 {
			t.Errorf("%v got a limit: %d", err, got)
		}
	}
}

func TestNoSessionHasNoDeadlineField(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})

	w := httptest.NewRecorder()
	s.protect(func(http.ResponseWriter, *http.Request) {}).ServeHTTP(w, httptest.NewRequest("GET", "/api/tree", nil))

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	if _, ok := body["after_sec"]; ok {
		t.Fatalf("the no-session answer got a limit: %+v", body)
	}
}

func TestProtectKeepsTheQueryInNext(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Nanosecond, Absolute: time.Hour})
	h := s.protect(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the handler ran with an expired session")
	})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withSession(t, s, "GET", "/app?session=nightly"))
	if loc := w.Header().Get("Location"); loc != "/login?reason=idle&next=%2Fapp%3Fsession%3Dnightly" {
		t.Fatalf("the session named in the address was lost on the way to the login: %q", loc)
	}

	for _, path := range []string{"//evil.example/app", "http://evil.example/app"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if loc := w.Header().Get("Location"); loc != "/login?reason=none&next=%2Fapp" {
			t.Errorf("a visit to %q led the login elsewhere: %q", path, loc)
		}
	}
}
