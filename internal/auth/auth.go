// Package auth handles panel login and session state.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cookieName = "monitor_session"

	// DefaultIdle and DefaultAbsolute are the default session lifetimes.
	DefaultIdle     = 24 * time.Hour
	DefaultAbsolute = 30 * 24 * time.Hour

	refreshFraction = 10
)

var (
	// ErrNoSession means the cookie is missing or its signature does not match.
	ErrNoSession = errors.New("there is no session: sign in again")
	// ErrIdle means the session sat too long without activity.
	ErrIdle = errors.New("the session was closed after a long spell without activity: sign in again")
	// ErrExpired means the session reached its absolute age limit.
	ErrExpired = errors.New("the session has reached its maximum age and cannot be extended: sign in again")
	// ErrRevoked means the device was revoked or is unknown.
	ErrRevoked = errors.New("this device no longer has access: it was revoked, sign in from another one")
)

// SessionTTL holds the session lifetimes.
type SessionTTL struct {
	Idle     time.Duration
	Absolute time.Duration
}

// Reason returns the short name of a rejection reason for URLs and JSON.
func Reason(err error) string {
	switch {
	case errors.Is(err, ErrIdle):
		return "idle"
	case errors.Is(err, ErrExpired):
		return "expired"
	case errors.Is(err, ErrRevoked):
		return "revoked"
	default:
		return "none"
	}
}

var b64 = base64.RawStdEncoding

// Service signs sessions and throttles login attempts.
type Service struct {
	secret []byte
	ttl    SessionTTL

	mu       sync.Mutex
	attempts map[string]*attempt

	devices  DeviceStore
	dmu      sync.Mutex
	dcache   map[int64]deviceCheck
	tokenDev int64
}

type attempt struct {
	count int
	until time.Time
}

func New(secret string, ttl SessionTTL) (*Service, error) {
	if len(secret) < 16 {
		return nil, errors.New("AACP_SECRET is required and must be at least 16 characters long")
	}
	if ttl.Idle <= 0 {
		return nil, errors.New("the session idle limit must be greater than zero (AACP_SESSION_IDLE)")
	}
	if ttl.Absolute <= 0 {
		return nil, errors.New("the absolute session limit must be greater than zero (AACP_SESSION_MAX)")
	}
	if ttl.Absolute < ttl.Idle {
		return nil, fmt.Errorf("the absolute session limit (%s) is shorter than the idle one (%s): the idle limit then means nothing",
			ttl.Absolute, ttl.Idle)
	}
	if ttl.Absolute > DefaultAbsolute {
		log.Printf("auth: the absolute session limit is %s, §5.3 of the spec expects %s", ttl.Absolute, DefaultAbsolute)
	}
	return &Service{
		secret:   []byte(secret),
		ttl:      ttl,
		attempts: map[string]*attempt{},
		dcache:   map[int64]deviceCheck{},
	}, nil
}

// TTL returns the configured session lifetimes.
func (a *Service) TTL() SessionTTL { return a.ttl }

// UseDevices turns on the device check on every request.
func (a *Service) UseDevices(d DeviceStore) {
	a.dmu.Lock()
	defer a.dmu.Unlock()
	a.devices = d
}

func (a *Service) useTokenDevice(id int64) {
	a.dmu.Lock()
	defer a.dmu.Unlock()
	a.tokenDev = id
}

func (a *Service) tokenDeviceID(hash [32]byte) int64 {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte("token-device"))
	mac.Write(hash[:])
	return -1 - int64(binary.BigEndian.Uint32(mac.Sum(nil))&0x3FFFFFFF)
}

func (a *Service) sign(payload string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	return payload + "." + b64.EncodeToString(mac.Sum(nil))
}

type sessionInfo struct {
	issued time.Time
	seen   time.Time
	device int64
}

func (a *Service) parse(token string) (sessionInfo, bool) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok {
		return sessionInfo{}, false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	want := mac.Sum(nil)
	got, err := b64.DecodeString(sig)
	if err != nil {
		return sessionInfo{}, false
	}
	if !hmac.Equal(got, want) {
		return sessionInfo{}, false
	}

	parts := strings.Split(payload, "|")
	if len(parts) != 3 {
		return sessionInfo{}, false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return sessionInfo{}, false
	}
	seen, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return sessionInfo{}, false
	}
	device, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || device == 0 {
		return sessionInfo{}, false
	}
	if seen < issued {
		return sessionInfo{}, false
	}
	return sessionInfo{issued: time.Unix(issued, 0), seen: time.Unix(seen, 0), device: device}, true
}

// IssueDevice issues a session cookie bound to a device.
func (a *Service) IssueDevice(w http.ResponseWriter, secure bool, device int64) {
	now := time.Now()
	a.setCookie(w, secure, sessionInfo{issued: now, seen: now, device: device})
}

func (a *Service) setCookie(w http.ResponseWriter, secure bool, info sessionInfo) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    a.token(info),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  info.seen.Add(a.ttl.Idle),
		MaxAge:   int(a.ttl.Idle / time.Second),
	})
}

func (a *Service) token(info sessionInfo) string {
	return a.sign(fmt.Sprintf("%d|%d|%d", info.issued.Unix(), info.seen.Unix(), info.device))
}

// IssueBearer issues a session as a string for cross-origin requests.
func (a *Service) IssueBearer(r *http.Request) (string, error) {
	info, src := a.sessionFrom(r, false)
	if src != srcCookie {
		return "", fmt.Errorf("%w: a bearer is issued from a cookie only", ErrNoSession)
	}
	if err := a.validate(r.Context(), info); err != nil {
		return "", err
	}
	info.seen = time.Now()
	return a.token(info), nil
}

// Clear drops the session cookie.
func (a *Service) Clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Authorized reports whether the request carries a valid session, without refreshing it.
func (a *Service) Authorized(r *http.Request) bool {
	_, err := a.check(r)
	return err == nil
}

// Guard checks the session, refreshes a stale activity mark and reports the rejection reason.
func (a *Service) Guard(w http.ResponseWriter, r *http.Request, secure bool) error {
	info, src := a.sessionFrom(r, false)
	if src == srcNone {
		return ErrNoSession
	}
	if err := a.validate(r.Context(), info); err != nil {
		return err
	}
	if src == srcCookie && time.Since(info.seen) > a.ttl.Idle/refreshFraction {
		info.seen = time.Now()
		a.setCookie(w, secure, info)
	}
	return nil
}

// Verify checks the session without refreshing it.
func (a *Service) Verify(r *http.Request) error {
	_, err := a.check(r)
	return err
}

// VerifyStream checks the session and also accepts it in the token query parameter.
func (a *Service) VerifyStream(r *http.Request) error {
	_, err := a.checkFrom(r, true)
	return err
}

func (a *Service) check(r *http.Request) (sessionInfo, error) {
	return a.checkFrom(r, false)
}

func (a *Service) checkFrom(r *http.Request, stream bool) (sessionInfo, error) {
	info, src := a.sessionFrom(r, stream)
	if src == srcNone {
		return sessionInfo{}, ErrNoSession
	}
	if err := a.validate(r.Context(), info); err != nil {
		return sessionInfo{}, err
	}
	return info, nil
}

func (a *Service) validate(ctx context.Context, info sessionInfo) error {
	now := time.Now()
	if now.Sub(info.issued) > a.ttl.Absolute {
		return ErrExpired
	}
	if now.Sub(info.seen) > a.ttl.Idle {
		return ErrIdle
	}
	if !a.deviceActive(ctx, info.device) {
		return ErrRevoked
	}
	return nil
}

// Deadline returns when the request session stops being valid if nothing else happens.
func (a *Service) Deadline(r *http.Request, stream bool) (time.Time, bool) {
	info, err := a.checkFrom(r, stream)
	if err != nil {
		return time.Time{}, false
	}
	absolute := info.issued.Add(a.ttl.Absolute)
	idle := info.seen.Add(a.ttl.Idle)
	if idle.Before(absolute) {
		return idle, true
	}
	return absolute, true
}

// CurrentDevice returns the device number of the current session, or 0.
func (a *Service) CurrentDevice(r *http.Request) int64 {
	info, err := a.check(r)
	if err != nil {
		return 0
	}
	return info.device
}

type source int

const (
	srcNone source = iota
	srcCookie
	srcBearer
	srcQuery
)

func (a *Service) sessionFrom(r *http.Request, stream bool) (sessionInfo, source) {
	if c, err := r.Cookie(cookieName); err == nil {
		if info, ok := a.parse(c.Value); ok {
			return info, srcCookie
		}
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return sessionInfo{}, srcNone
	}
	if raw := bearerValue(r); raw != "" {
		if info, ok := a.parse(raw); ok {
			return info, srcBearer
		}
	}
	if stream {
		if raw := r.URL.Query().Get("token"); raw != "" {
			if info, ok := a.parse(raw); ok {
				return info, srcQuery
			}
		}
	}
	return sessionInfo{}, srcNone
}

func bearerValue(r *http.Request) string {
	head := r.Header.Get("Authorization")
	if len(head) <= len(tokenScheme) || !strings.EqualFold(head[:len(tokenScheme)], tokenScheme) {
		return ""
	}
	return strings.TrimSpace(head[len(tokenScheme):])
}

type deviceCheck struct {
	ok bool
	at time.Time
}

const (
	deviceCheckTTL   = 30 * time.Second
	deviceGrace      = 15 * time.Minute
	deviceCacheLimit = 256
)

func (a *Service) deviceActive(ctx context.Context, id int64) bool {
	a.dmu.Lock()
	devices := a.devices
	tokenDev := a.tokenDev
	prev, known := a.dcache[id]
	a.dmu.Unlock()

	if id < 0 {
		return tokenDev != 0 && id == tokenDev
	}

	if devices == nil {
		return false
	}
	if known && time.Since(prev.at) < deviceCheckTTL {
		return prev.ok
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ok, err := devices.Exists(ctx, id)
	if err != nil {
		if known && prev.ok && time.Since(prev.at) < deviceGrace {
			return true
		}
		log.Printf("auth: checking device %d: %v", id, err)
		return false
	}

	a.dmu.Lock()
	if len(a.dcache) >= deviceCacheLimit {
		clear(a.dcache)
	}
	a.dcache[id] = deviceCheck{ok: ok, at: time.Now()}
	a.dmu.Unlock()
	return ok
}

// ForgetDevice drops a device from the check cache so a revocation takes effect at once.
func (a *Service) ForgetDevice(id int64) {
	a.dmu.Lock()
	defer a.dmu.Unlock()
	delete(a.dcache, id)
}

// Throttle returns how long to wait before the next login attempt from an address.
func (a *Service) Throttle(remote string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	at, ok := a.attempts[remote]
	if !ok {
		return 0
	}
	if time.Now().Before(at.until) {
		return time.Until(at.until)
	}
	return 0
}

// Fail records a failed login attempt.
func (a *Service) Fail(remote string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	at, ok := a.attempts[remote]
	if !ok || time.Now().After(at.until) && at.count >= 5 {
		at = &attempt{}
		a.attempts[remote] = at
	}
	at.count++
	if at.count >= 5 {
		at.until = time.Now().Add(time.Minute)
	}
	if len(a.attempts) > 1000 {
		for k, v := range a.attempts {
			if time.Now().After(v.until) {
				delete(a.attempts, k)
			}
		}
	}
}

// Ok resets the attempt counter after a successful login.
func (a *Service) Ok(remote string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.attempts, remote)
}

// ClientIP returns the client address, accounting for the reverse proxy in front.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
