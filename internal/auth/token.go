package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const (
	// TokenMin is the minimum length of a login token.
	TokenMin = 24

	tokenScheme = "bearer "
)

// TokenLogin is login by a long-lived token.
type TokenLogin struct {
	hash     [32]byte
	deviceID int64

	session *Service
	secure  bool
}

// NewTokenLogin builds token login, or returns nil when the token is empty.
func NewTokenLogin(token string, session *Service, secure bool) (*TokenLogin, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	if session == nil {
		return nil, errors.New("token login: a session service is required")
	}
	if len([]rune(token)) < TokenMin {
		return nil, fmt.Errorf("AACP_TOKEN is shorter than %d characters: a short token lives for months and gets guessed", TokenMin)
	}

	t := &TokenLogin{
		hash:    sha256.Sum256([]byte(token)),
		session: session,
		secure:  secure,
	}
	t.deviceID = session.tokenDeviceID(t.hash)
	session.useTokenDevice(t.deviceID)
	return t, nil
}

// Enabled reports whether a token is configured.
func (t *TokenLogin) Enabled() bool { return t != nil }

// Device returns the device number used by token sessions.
func (t *TokenLogin) Device() int64 {
	if t == nil {
		return 0
	}
	return t.deviceID
}

type tokenRequest struct {
	Token string `json:"token"`
}

// Login exchanges a token for an ordinary session.
func (t *TokenLogin) Login(w http.ResponseWriter, r *http.Request) {
	if !t.Enabled() {
		fail(w, http.StatusNotFound, "token login is off")
		return
	}
	ip := ClientIP(r)
	if wait := t.session.Throttle(ip); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, wait %d s", int(wait.Seconds())+1))
		return
	}

	var body tokenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "the request body is malformed")
		return
	}
	if !t.match(body.Token) {
		t.session.Fail(ip)
		log.Printf("%s: token login rejected", ip)
		fail(w, http.StatusForbidden, "the token does not match")
		return
	}

	t.session.Ok(ip)
	t.session.IssueDevice(w, t.secure, t.deviceID)
	log.Printf("%s: token login", ip)
	writeJSON(w, http.StatusOK, map[string]any{"method": "token"})
}

// Bearer reports whether a valid token was presented in the Authorization header.
func (t *TokenLogin) Bearer(r *http.Request) bool {
	if !t.Enabled() || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	value := bearerValue(r)
	if value == "" {
		return false
	}
	if _, ok := t.session.parse(value); ok {
		return false
	}
	ip := ClientIP(r)
	if t.session.Throttle(ip) > 0 {
		return false
	}
	if !t.match(value) {
		t.session.Fail(ip)
		log.Printf("%s: the token header was rejected", ip)
		return false
	}
	t.session.Ok(ip)
	return true
}

func (t *TokenLogin) match(given string) bool {
	given = strings.TrimSpace(given)
	if given == "" {
		return false
	}
	sum := sha256.Sum256([]byte(given))
	return subtle.ConstantTimeCompare(sum[:], t.hash[:]) == 1
}

// Methods reports which doors are open for login.
func Methods(passkey *Passkey, token *TokenLogin, home string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"passkey": passkey.Enabled() && home == "",
			"token":   token.Enabled(),
		}
		if home != "" {
			body["home"] = home
		}
		writeJSON(w, http.StatusOK, body)
	}
}
