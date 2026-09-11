package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sessionRequest(t *testing.T, a *Service, issued, seen time.Time, device int64) *http.Request {
	t.Helper()
	w := httptest.NewRecorder()
	a.setCookie(w, false, sessionInfo{issued: issued, seen: seen, device: device})

	r := httptest.NewRequest("GET", "/api/tree", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}

func testService(t *testing.T, ttl SessionTTL) (*Service, *memDevices) {
	t.Helper()
	a, err := New(testSecret, ttl)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	devices := &memDevices{}
	if _, err := devices.Add(context.Background(), &Device{Name: "phone", CredentialID: []byte("cred"), PublicKey: []byte("pk")}); err != nil {
		t.Fatalf("device: %v", err)
	}
	a.UseDevices(devices)
	return a, devices
}

func TestSessionTTLValidation(t *testing.T) {
	cases := map[string]SessionTTL{
		"no idle limit":                         {Idle: 0, Absolute: time.Hour},
		"no absolute limit":                     {Idle: time.Minute, Absolute: 0},
		"negative idle":                         {Idle: -time.Minute, Absolute: time.Hour},
		"absolute shorter than idle":            {Idle: time.Hour, Absolute: time.Minute},
		"both zero (a session that never ends)": {},
	}
	for name, ttl := range cases {
		if _, err := New(testSecret, ttl); err == nil {
			t.Errorf("%s: the configuration was accepted, and it must not be", name)
		}
	}

	if _, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute}); err != nil {
		t.Errorf("a sound configuration was rejected: %v", err)
	}
}

func TestSessionExpiresByIdle(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	now := time.Now()

	r := sessionRequest(t, a, now.Add(-time.Hour), now.Add(-29*time.Minute), 1)
	if err := a.Guard(httptest.NewRecorder(), r, false); err != nil {
		t.Fatalf("a live session was rejected: %v", err)
	}

	r = sessionRequest(t, a, now.Add(-time.Hour), now.Add(-31*time.Minute), 1)
	err := a.Guard(httptest.NewRecorder(), r, false)
	if !errors.Is(err, ErrIdle) {
		t.Fatalf("idle time did not close the session: %v", err)
	}
	if Reason(err) != "idle" {
		t.Fatalf("the reason is reported as %q", Reason(err))
	}
	if a.Authorized(r) {
		t.Fatal("Authorized lets through a session that expired on idle time")
	}
}

func TestSessionExpiresByAbsoluteDespiteActivity(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	now := time.Now()

	r := sessionRequest(t, a, now.Add(-13*time.Hour), now.Add(-time.Second), 1)
	err := a.Guard(httptest.NewRecorder(), r, false)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("the absolute limit did not fire: %v", err)
	}
	if Reason(err) != "expired" {
		t.Fatalf("the reason is reported as %q", Reason(err))
	}
}

func TestGuardRefreshesSeenNotIssued(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	issued := time.Now().Add(-11*time.Hour - 55*time.Minute)

	r := sessionRequest(t, a, issued, time.Now().Add(-20*time.Minute), 1)
	w := httptest.NewRecorder()
	if err := a.Guard(w, r, false); err != nil {
		t.Fatalf("the session was rejected: %v", err)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("the session was not renewed: %d cookies", len(cookies))
	}
	info, ok := a.parse(cookies[0].Value)
	if !ok {
		t.Fatal("the renewed cookie does not parse")
	}
	if info.issued.Unix() != issued.Unix() {
		t.Fatalf("the moment of login moved: was %s, became %s", issued, info.issued)
	}
	if time.Since(info.seen) > time.Minute {
		t.Fatalf("the activity mark was not refreshed: %s", info.seen)
	}

	stale := sessionRequest(t, a, time.Now().Add(-12*time.Hour-time.Second), time.Now(), 1)
	if err := a.Guard(httptest.NewRecorder(), stale, false); !errors.Is(err, ErrExpired) {
		t.Fatalf("the absolute limit was walked around by renewal: %v", err)
	}
}

func TestGuardDoesNotRefreshOnEveryRequest(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})

	fresh := sessionRequest(t, a, time.Now(), time.Now(), 1)
	w := httptest.NewRecorder()
	if err := a.Guard(w, fresh, false); err != nil {
		t.Fatalf("the session was rejected: %v", err)
	}
	if got := len(w.Result().Cookies()); got != 0 {
		t.Fatalf("a fresh session was re-set for nothing: %d cookies", got)
	}
}

func TestDeadlineIsNearestOfTwoLimits(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	now := time.Now()

	r := sessionRequest(t, a, now.Add(-time.Hour), now, 1)
	deadline, ok := a.Deadline(r, false)
	if !ok {
		t.Fatal("a live session has no deadline")
	}
	if d := time.Until(deadline); d > 30*time.Minute+time.Second || d < 29*time.Minute {
		t.Fatalf("the idle deadline was computed as %s", d)
	}

	r = sessionRequest(t, a, now.Add(-12*time.Hour+5*time.Minute), now, 1)
	deadline, ok = a.Deadline(r, false)
	if !ok {
		t.Fatal("a live session has no deadline")
	}
	if d := time.Until(deadline); d > 5*time.Minute+time.Second || d < 4*time.Minute {
		t.Fatalf("the absolute deadline was computed as %s", d)
	}

	dead := sessionRequest(t, a, now.Add(-13*time.Hour), now, 1)
	if _, ok := a.Deadline(dead, false); ok {
		t.Fatal("an expired session turned out to have a deadline")
	}
}

func TestRevokedDeviceReportedSeparately(t *testing.T) {
	a, devices := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	r := sessionRequest(t, a, time.Now(), time.Now(), 1)

	if err := a.Guard(httptest.NewRecorder(), r, false); err != nil {
		t.Fatalf("a live session was rejected: %v", err)
	}
	if err := devices.Revoke(context.Background(), 1); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	a.ForgetDevice(1)

	err := a.Guard(httptest.NewRecorder(), r, false)
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("the revocation is not named as the reason: %v", err)
	}
	if Reason(err) != "revoked" {
		t.Fatalf("the reason is reported as %q", Reason(err))
	}
}

func TestForgedSessionRejected(t *testing.T) {
	a, _ := testService(t, SessionTTL{Idle: 30 * time.Minute, Absolute: 12 * time.Hour})
	now := time.Now().Unix()

	forged := []string{
		"",
		"garbage",
		"1|2|3",
		a.sign("0|0|1"),
	}
	for _, token := range forged {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		if err := a.Guard(httptest.NewRecorder(), r, false); err == nil {
			t.Fatalf("a forged token %q was accepted", token)
		}
	}

	other, err := New("fedcba9876543210fedcba9876543210", SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: other.sign(
		time.Unix(now, 0).Format("2006") + "|0|1")})
	if err := a.Guard(httptest.NewRecorder(), r, false); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a signature from another service was accepted: %v", err)
	}
}
