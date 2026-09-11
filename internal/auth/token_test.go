package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	testToken  = "aacpanel-local-token-0123456789"
	otherToken = "aacpanel-local-token-9876543210"
)

type tokenStand struct {
	srv    *httptest.Server
	guard  *Service
	tokens *TokenLogin
	logs   *bytes.Buffer
}

func newTokenStand(t *testing.T, token string) *tokenStand {
	t.Helper()

	logs := new(bytes.Buffer)
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	guard, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("session service: %v", err)
	}
	devices := &memDevices{}
	guard.UseDevices(devices)

	pk, err := NewPasskey(PasskeyConfig{
		RPID:    testRPID,
		RPName:  "aacpanel",
		Origins: []string{testOrigin},
		Secure:  false,
	}, guard, devices)
	if err != nil {
		t.Fatalf("passkey: %v", err)
	}

	tokens, err := NewTokenLogin(token, guard, false)
	if err != nil {
		t.Fatalf("token login: %v", err)
	}

	gate := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := guard.Verify(r); err != nil && !tokens.Bearer(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		})
	}
	ok := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/token", tokens.Login)
	mux.HandleFunc("GET /auth/methods", Methods(pk, tokens, ""))
	mux.Handle("GET /api/tree", gate(ok))
	mux.Handle("GET /app", gate(ok))
	mux.Handle("POST /api/push/subscription", gate(pk.Subscribe))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &tokenStand{srv: srv, guard: guard, tokens: tokens, logs: logs}
}

func (s *tokenStand) login(t *testing.T, c *http.Client, token string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": token})
	return s.do(t, c, "POST", "/auth/token", body, "")
}

func (s *tokenStand) do(t *testing.T, c *http.Client, method, path string, body []byte, bearer string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, s.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := new(bytes.Buffer)
	out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

func newJar(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("stand address: %v", err)
	}
	return u
}

func TestTokenLoginIssuesSession(t *testing.T) {
	s := newTokenStand(t, testToken)
	c := newJar(t)

	if code, body := s.do(t, c, "GET", "/api/tree", nil, ""); code != http.StatusUnauthorized {
		t.Fatalf("let through before login: %d %s", code, body)
	}
	if code, body := s.login(t, c, testToken); code != http.StatusOK {
		t.Fatalf("token login: %d %s", code, body)
	}
	if code, body := s.do(t, c, "GET", "/api/tree", nil, ""); code != http.StatusOK {
		t.Fatalf("not let through after login: %d %s", code, body)
	}
	if code, body := s.do(t, c, "GET", "/app", nil, ""); code != http.StatusOK {
		t.Fatalf("the page was not served: %d %s", code, body)
	}
}

func TestTokenLoginRejectsWrongToken(t *testing.T) {
	s := newTokenStand(t, testToken)
	c := newJar(t)

	wrong := testToken[:len(testToken)-1] + "X"
	code, body := s.login(t, c, wrong)
	if code != http.StatusForbidden {
		t.Fatalf("a wrong token was accepted: %d %s", code, body)
	}
	if len(c.Jar.Cookies(mustURL(t, s.srv.URL))) != 0 {
		t.Fatal("a cookie was issued after the refusal")
	}
	if code, _ := s.do(t, c, "GET", "/api/tree", nil, ""); code != http.StatusUnauthorized {
		t.Fatalf("let through after the refusal: %d", code)
	}
	if code, _ := s.login(t, newJar(t), ""); code != http.StatusForbidden {
		t.Fatalf("an empty token: %d", code)
	}
}

func TestWithoutTokenNothingChanges(t *testing.T) {
	s := newTokenStand(t, "")
	c := newJar(t)

	if s.tokens.Enabled() {
		t.Fatal("token login switched itself on without a token")
	}
	if code, body := s.login(t, c, testToken); code != http.StatusNotFound {
		t.Fatalf("a switched-off login answered: %d %s", code, body)
	}
	if code, _ := s.do(t, c, "GET", "/api/tree", nil, testToken); code != http.StatusUnauthorized {
		t.Fatal("a header with the token let through while login is switched off")
	}

	var methods struct {
		Passkey bool `json:"passkey"`
		Token   bool `json:"token"`
	}
	_, body := s.do(t, c, "GET", "/auth/methods", nil, "")
	if err := json.Unmarshal(body, &methods); err != nil {
		t.Fatalf("login methods: %v (%s)", err, body)
	}
	if methods.Token || !methods.Passkey {
		t.Fatalf("login methods with the token switched off: %+v", methods)
	}

	withToken, tokens := serviceWithToken(t, testToken)
	plain, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("session service: %v", err)
	}
	now := time.Now()
	r := sessionRequest(t, withToken, now, now, tokens.Device())
	if err := plain.Verify(r); !errors.Is(err, ErrRevoked) {
		t.Fatalf("a session on a token that was taken away: %v", err)
	}
}

func TestTokenChangeKillsOldSessions(t *testing.T) {
	old, oldTokens := serviceWithToken(t, testToken)
	fresh, freshTokens := serviceWithToken(t, otherToken)

	if oldTokens.Device() == freshTokens.Device() {
		t.Fatal("two different tokens got one device number")
	}
	if oldTokens.Device() >= 0 || freshTokens.Device() >= 0 {
		t.Fatalf("the number of a token device is not negative: %d, %d", oldTokens.Device(), freshTokens.Device())
	}

	now := time.Now()
	r := sessionRequest(t, old, now, now, oldTokens.Device())
	if err := fresh.Verify(r); !errors.Is(err, ErrRevoked) {
		t.Fatalf("a session of the previous token survived the change: %v", err)
	}
	if err := fresh.Verify(sessionRequest(t, fresh, now, now, freshTokens.Device())); err != nil {
		t.Fatalf("its own session was rejected: %v", err)
	}
}

func serviceWithToken(t *testing.T, token string) (*Service, *TokenLogin) {
	t.Helper()
	guard, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("session service: %v", err)
	}
	tokens, err := NewTokenLogin(token, guard, false)
	if err != nil {
		t.Fatalf("token login: %v", err)
	}
	return guard, tokens
}

func TestTokenNeverLeaksToLogsOrAnswers(t *testing.T) {
	s := newTokenStand(t, testToken)
	c := newJar(t)

	var seen []string
	collect := func(code int, body []byte) { seen = append(seen, string(body)) }

	collect(s.login(t, c, testToken))
	collect(s.login(t, newJar(t), "the wrong token entirely, but long enough"))
	collect(s.do(t, c, "GET", "/api/tree", nil, testToken))
	collect(s.do(t, newJar(t), "GET", "/api/tree", nil, "a miss, but the header is there"))
	collect(s.do(t, c, "GET", "/auth/methods", nil, ""))

	for _, body := range seen {
		if strings.Contains(body, testToken) {
			t.Fatalf("the token is in the server answer: %s", body)
		}
	}
	if strings.Contains(s.logs.String(), testToken) {
		t.Fatalf("the token is in the log: %s", s.logs.String())
	}
	if !strings.Contains(s.logs.String(), "token login") {
		t.Fatalf("token login is not recorded in the log: %s", s.logs.String())
	}
	for _, cookie := range c.Jar.Cookies(mustURL(t, s.srv.URL)) {
		if strings.Contains(cookie.Value, testToken) {
			t.Fatal("the token went into a cookie")
		}
	}
}

func TestBearerWorksOnlyOnAPI(t *testing.T) {
	s := newTokenStand(t, testToken)
	c := newJar(t)

	if code, body := s.do(t, c, "GET", "/api/tree", nil, testToken); code != http.StatusOK {
		t.Fatalf("a header with the token did not let through to the API: %d %s", code, body)
	}
	if code, _ := s.do(t, c, "GET", "/app", nil, testToken); code != http.StatusUnauthorized {
		t.Fatal("a header with the token let through to a page")
	}
	if code, _ := s.do(t, c, "GET", "/api/tree", nil, "someone-elses"); code != http.StatusUnauthorized {
		t.Fatal("a header with a foreign token let through to the API")
	}
	if code, _ := s.do(t, c, "GET", "/api/tree", nil, ""); code != http.StatusUnauthorized {
		t.Fatal("a session was left behind by the header")
	}

	req, _ := http.NewRequest("GET", s.srv.URL+"/api/tree", nil)
	req.Header.Set("Authorization", "bearer "+testToken)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the scheme in lower case was not accepted: %d", resp.StatusCode)
	}
}

func TestTokenLoginThrottles(t *testing.T) {
	s := newTokenStand(t, testToken)
	c := newJar(t)

	for i := range 5 {
		if code, _ := s.login(t, c, "miss"); code != http.StatusForbidden {
			t.Fatalf("attempt %d: %d", i+1, code)
		}
	}
	if code, _ := s.login(t, c, "miss"); code != http.StatusTooManyRequests {
		t.Fatalf("the sixth attempt went through without a pause: %d", code)
	}
	if code, _ := s.login(t, c, testToken); code != http.StatusTooManyRequests {
		t.Fatalf("the right token during the pause: %d", code)
	}
	if code, _ := s.do(t, c, "GET", "/api/tree", nil, testToken); code != http.StatusUnauthorized {
		t.Fatal("a header with the token went through during the pause")
	}
}

func TestShortTokenRefused(t *testing.T) {
	guard, err := New(testSecret, SessionTTL{Idle: DefaultIdle, Absolute: DefaultAbsolute})
	if err != nil {
		t.Fatalf("session service: %v", err)
	}
	short := strings.Repeat("a", TokenMin-1)
	tokens, err := NewTokenLogin(short, guard, false)
	if err == nil {
		t.Fatal("a short token was accepted")
	}
	if tokens != nil {
		t.Fatal("a working login came back together with the refusal")
	}
	if strings.Contains(err.Error(), short) {
		t.Fatalf("the token is in the error text: %v", err)
	}
	padded, err := NewTokenLogin("  "+testToken+"\n", guard, false)
	if err != nil {
		t.Fatalf("a token with spaces: %v", err)
	}
	if !padded.match(testToken) {
		t.Fatal("a token with spaces around it did not match itself")
	}
}

func TestTokenSessionHasNoDevice(t *testing.T) {
	s := newTokenStand(t, testToken)
	c := newJar(t)
	if code, body := s.login(t, c, testToken); code != http.StatusOK {
		t.Fatalf("token login: %d %s", code, body)
	}

	code, body := s.do(t, c, "POST", "/api/push/subscription", pushBody(t, "https://push.example.net/abc"), "")
	if code != http.StatusForbidden {
		t.Fatalf("subscribing from a token session: %d %s", code, body)
	}
	if !strings.Contains(string(body), "token") {
		t.Fatalf("the refusal does not explain the reason: %s", body)
	}
}

func TestTokenSessionNamedInJournal(t *testing.T) {
	_, tokens := serviceWithToken(t, testToken)
	pk := &Passkey{}
	if name := pk.DeviceName(t.Context(), tokens.Device()); name != "token login" {
		t.Fatalf("the name of a token session in the action log: %q", name)
	}
}

func TestMethodsTellsWhichDoorsExist(t *testing.T) {
	s := newTokenStand(t, testToken)
	var methods struct {
		Passkey bool `json:"passkey"`
		Token   bool `json:"token"`
	}
	_, body := s.do(t, newJar(t), "GET", "/auth/methods", nil, "")
	if err := json.Unmarshal(body, &methods); err != nil {
		t.Fatalf("login methods: %v (%s)", err, body)
	}
	if !methods.Token || !methods.Passkey {
		t.Fatalf("login methods with a token set: %+v", methods)
	}
	if strings.Contains(string(body), "\"methods\"") {
		t.Fatalf("the answer is wrapped, the screen will read it as empty: %s", body)
	}
}

func TestTokenComparedInConstantTime(t *testing.T) {
	src, err := os.ReadFile("token.go")
	if err != nil {
		t.Fatalf("source file: %v", err)
	}
	if !strings.Contains(string(src), "subtle.ConstantTimeCompare") {
		t.Fatal("the token is compared outside crypto/subtle")
	}
	if !strings.Contains(string(src), "sha256.Sum256") || !strings.Contains(string(src), "t.hash[:]") {
		t.Fatal("it is not hashes that are compared — the token length leaks through timing")
	}

	_, tokens := serviceWithToken(t, testToken)
	for _, given := range []string{
		testToken[:len(testToken)-1],
		testToken + "x",
		strings.ToUpper(testToken),
		testToken[:5] + "X" + testToken[6:],
	} {
		if tokens.match(given) {
			t.Fatalf("a foreign token was accepted: %q", given)
		}
	}
	if !tokens.match(testToken) {
		t.Fatal("its own token was not accepted")
	}
}
