package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/auth"
)

const testPanelSecret = "0123456789abcdef0123456789abcdef"

func testEndpoints(t *testing.T) endpoints {
	t.Helper()
	eps, err := loadEndpoints(testPanelSecret, "https://panel.example", "https://lan.example:8443", "https://host.tailnet.ts.net", ":8777", "")
	if err != nil {
		t.Fatal(err)
	}
	return eps
}

func TestLoadEndpointsOrderAndDefaults(t *testing.T) {
	eps := testEndpoints(t)
	got := make([]string, 0, 3)
	for _, e := range eps.list {
		got = append(got, e.Kind+" "+e.URL)
	}
	want := "local http://127.0.0.1:8777 | lan https://lan.example:8443 | public https://panel.example | ts https://host.tailnet.ts.net"
	if strings.Join(got, " | ") != want {
		t.Errorf("the endpoint map %q, expected %q", strings.Join(got, " | "), want)
	}
	if len(eps.panel) != 16 {
		t.Errorf("the fingerprint of this instance is %q — expected 16 hex", eps.panel)
	}
	other, _ := loadEndpoints("another-secret-0123456789", "https://panel.example", "", "", "", "")
	if other.panel == eps.panel {
		t.Error("the fingerprint does not depend on the secret — two installations are indistinguishable to the client")
	}

	only, err := loadEndpoints(testPanelSecret, "https://panel.example", "", "", "", "")
	if err != nil || len(only.list) != 1 || only.list[0].Kind != kindPublic {
		t.Errorf("the map from a single domain: %+v, %v", only.list, err)
	}
	explicit, _ := loadEndpoints(testPanelSecret, "https://panel.example", "", "", ":8777", "http://localhost:8777")
	if explicit.origin(kindLocal) != "http://localhost:8777" {
		t.Errorf("AACP_LOCAL_URL did not override the derived address: %q", explicit.origin(kindLocal))
	}
}

func TestLoadEndpointsRejectsNonOrigins(t *testing.T) {
	for _, raw := range []string{"https://panel.example/app", "panel.example", "ftp://panel.example", "https://u:p@panel.example", "https://panel.example?x=1"} {
		if _, err := loadEndpoints(testPanelSecret, raw, "", "", "", ""); err == nil {
			t.Errorf("%q was accepted as a panel address", raw)
		}
	}
	eps, err := loadEndpoints(testPanelSecret, "https://panel.example/", "", "", "", "")
	if err != nil || eps.origin(kindPublic) != "https://panel.example" {
		t.Errorf("a trailing slash: %+v, %v", eps.list, err)
	}
}

func TestEndpointsRoutesKnowWhereTheyAre(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)

	public := s.routes(s.publicGate())
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/endpoints", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the endpoint map without a session: %d, expected 401 — internal addresses are not for the sign-in screen", rec.Code)
	}

	for _, c := range []struct {
		name string
		mux  http.Handler
		want string
	}{
		{"main", public, kindPublic},
		{"local network", s.routes(s.lanGate()), kindLAN},
		{"tailscale", s.routes(s.tsGate()), kindTS},
		{"local panel", s.routes(s.localGate()), kindLocal},
	} {
		rec := httptest.NewRecorder()
		c.mux.ServeHTTP(rec, withSession(t, s, http.MethodGet, "/api/endpoints"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: the endpoint map %d: %s", c.name, rec.Code, rec.Body.String())
		}
		var body struct {
			Panel     string     `json:"panel"`
			Here      string     `json:"here"`
			Endpoints []endpoint `json:"endpoints"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Here != c.want || body.Panel != s.endpoints.panel || len(body.Endpoints) != 4 {
			t.Errorf("%s: here=%q panel=%q endpoints=%d", c.name, body.Here, body.Panel, len(body.Endpoints))
		}
	}
}

func TestProbeAnswersOnlyPanelOrigins(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)
	mux := s.routes(s.lanGate())

	for _, c := range []struct {
		name, method, origin string
		code                 int
		cors                 bool
	}{
		{"our own origin", http.MethodGet, "https://panel.example", http.StatusOK, true},
		{"our own origin, preflight", http.MethodOptions, "https://panel.example", http.StatusNoContent, true},
		{"the loopback from the map", http.MethodGet, "http://127.0.0.1:8777", http.StatusOK, true},
		{"a foreign origin", http.MethodGet, "https://evil.example", http.StatusForbidden, false},
		{"a subdomain of our own", http.MethodGet, "https://sub.panel.example", http.StatusForbidden, false},
		{"no origin (curl)", http.MethodGet, "", http.StatusOK, false},
	} {
		r := httptest.NewRequest(c.method, "/probe", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		if rec.Code != c.code {
			t.Errorf("%s: %d, expected %d: %s", c.name, rec.Code, c.code, rec.Body.String())
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); (got != "") != c.cors || (c.cors && got != c.origin) {
			t.Errorf("%s: Access-Control-Allow-Origin=%q", c.name, got)
		}
		if c.method == http.MethodGet && c.code == http.StatusOK {
			var body struct{ Panel, Kind string }
			json.Unmarshal(rec.Body.Bytes(), &body)
			if body.Panel != s.endpoints.panel || body.Kind != kindLAN {
				t.Errorf("%s: body %s", c.name, rec.Body.String())
			}
		}
	}
}

func TestLoginOffDomainHidesPasskeyAndPointsHome(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)
	for _, c := range []struct {
		name string
		mux  http.Handler
		home string
	}{
		{"local network", s.routes(s.lanGate()), "https://panel.example"},
		{"tailscale", s.routes(s.tsGate()), "https://panel.example"},
		{"main", s.routes(s.publicGate()), ""},
	} {
		rec := httptest.NewRecorder()
		c.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/methods", nil))
		var body struct {
			Passkey bool   `json:"passkey"`
			Home    string `json:"home"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v: %s", c.name, err, rec.Body.String())
		}
		if body.Home != c.home {
			t.Errorf("%s: home=%q, expected %q", c.name, body.Home, c.home)
		}
		if c.home != "" && body.Passkey {
			t.Errorf("%s: the passkey is shown where it will not open", c.name)
		}
	}
}

func bearerFor(t *testing.T, s *Server, mux http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withSession(t, s, http.MethodGet, "/api/session/token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: %d %s", rec.Code, rec.Body.String())
	}
	var body struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("no bearer was issued: %v %s", err, rec.Body.String())
	}
	return body.Token
}

func bearerRequest(method, path, token string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestBearerSessionOpensAPIOnEveryListener(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)
	public := s.routes(s.publicGate())
	token := bearerFor(t, s, public)

	for _, c := range []struct {
		name string
		mux  http.Handler
		want string
	}{
		{"main", public, kindPublic},
		{"local network", s.routes(s.lanGate()), kindLAN},
		{"tailscale", s.routes(s.tsGate()), kindTS},
	} {
		rec := httptest.NewRecorder()
		c.mux.ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/endpoints", token))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"here":"`+c.want+`"`) {
			t.Errorf("%s: the endpoint map by bearer: %d %s", c.name, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Set-Cookie"); got != "" {
			t.Errorf("%s: the answer to a bearer sets a cookie: %q", c.name, got)
		}
	}

	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, bearerRequest(http.MethodGet, "/app", token))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("a page by bearer: %d, expected 303 to the sign-in", rec.Code)
	}
	rec = httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/endpoints", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the endpoint map without a session: %d", rec.Code)
	}
	forged := token[:len(token)-4] + "AAAA"
	rec = httptest.NewRecorder()
	public.ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/endpoints", forged))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a bearer with a foreign signature: %d", rec.Code)
	}
	revoked := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	revoked.auth.UseDevices(deadDevice{})
	revoked.endpoints = s.endpoints
	rec = httptest.NewRecorder()
	revoked.routes(revoked.publicGate()).ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/endpoints", token))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"reason":"revoked"`) {
		t.Errorf("a bearer of a revoked device: %d %s", rec.Code, rec.Body.String())
	}
	short := testServer(t, auth.SessionTTL{Idle: time.Nanosecond, Absolute: time.Hour})
	short.endpoints = s.endpoints
	time.Sleep(2 * time.Millisecond)
	rec = httptest.NewRecorder()
	short.routes(short.publicGate()).ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/endpoints", token))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"reason":"idle"`) {
		t.Errorf("an expired bearer: %d %s", rec.Code, rec.Body.String())
	}
}

type deadDevice struct{ auth.DeviceStore }

func (deadDevice) Exists(context.Context, int64) (bool, error) { return false, nil }

func TestSessionTokenIssuedOnlyByCookie(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)
	public := s.routes(s.publicGate())
	token := bearerFor(t, s, public)

	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/session/token", token))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a bearer issued itself a new bearer: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.routes(s.localGate()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session/token", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the local panel issued a bearer without a session: %d %s", rec.Code, rec.Body.String())
	}
}

func TestQueryTokenOpensOnlyStreams(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)
	token := bearerFor(t, s, s.routes(s.publicGate()))

	served := false
	rec := httptest.NewRecorder()
	s.protectStream(func(http.ResponseWriter, *http.Request) { served = true }).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/stream?token="+url.QueryEscape(token), nil))
	if !served || rec.Code != http.StatusOK {
		t.Errorf("the stream did not open by ?token=: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.protect(func(http.ResponseWriter, *http.Request) {
		t.Error("an ordinary handler opened by ?token=")
	}).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/tree?token="+url.QueryEscape(token), nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an ordinary handler with ?token=: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.protectStream(func(http.ResponseWriter, *http.Request) {
		t.Error("the stream opened on a foreign signature")
	}).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/stream?token="+url.QueryEscape(token[:len(token)-4]+"AAAA"), nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a stream with a foreign signature: %d", rec.Code)
	}
}

func TestCORSAnswersOnlyPanelOrigins(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	s.endpoints = testEndpoints(t)
	token := bearerFor(t, s, s.routes(s.publicGate()))

	for _, l := range []struct {
		name string
		mux  http.Handler
	}{
		{"main", s.handler(s.publicGate())},
		{"local network", s.handler(s.lanGate())},
		{"tailscale", s.handler(s.tsGate())},
		{"local panel", s.handler(s.localGate())},
	} {
		r := httptest.NewRequest(http.MethodOptions, "/api/actions", nil)
		r.Header.Set("Origin", "https://panel.example")
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
		rec := httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s: preflight %d %s", l.name, rec.Code, rec.Body.String())
		}
		h := rec.Header()
		if h.Get("Access-Control-Allow-Origin") != "https://panel.example" || h.Get("Vary") != "Origin" ||
			!strings.Contains(h.Get("Access-Control-Allow-Headers"), "Authorization") ||
			!strings.Contains(h.Get("Access-Control-Allow-Methods"), "POST") ||
			h.Get("Access-Control-Allow-Private-Network") != "true" {
			t.Errorf("%s: preflight headers: %v", l.name, h)
		}
		r = bearerRequest(http.MethodGet, "/api/endpoints", token)
		r.Header.Set("Origin", "https://panel.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "https://panel.example" {
			t.Errorf("%s: a request with an Origin from the map: %d, ACAO=%q", l.name, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		r = httptest.NewRequest(http.MethodOptions, "/api/actions", nil)
		r.Header.Set("Origin", "https://evil.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Code == http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: a preflight to a foreign origin: %d, ACAO=%q", l.name, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		r = bearerRequest(http.MethodGet, "/api/endpoints", token)
		r.Header.Set("Origin", "https://sub.panel.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: CORS was given to a subdomain of our own", l.name)
		}
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, bearerRequest(http.MethodGet, "/api/endpoints", token))
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: a request without an Origin got CORS", l.name)
		}
		r = httptest.NewRequest(http.MethodGet, "/dist/bundle.js", nil)
		r.Header.Set("Origin", "https://panel.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Header().Get("Access-Control-Allow-Origin") != "https://panel.example" {
			t.Errorf("%s: the panel code was not given CORS", l.name)
		}
		r = httptest.NewRequest(http.MethodGet, "/dist/bundle.js", nil)
		r.Header.Set("Origin", "https://evil.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: a foreign origin was given CORS for the code", l.name)
		}
		r = httptest.NewRequest(http.MethodGet, "/healthz", nil)
		r.Header.Set("Origin", "https://panel.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: CORS outside /api/ and /dist/", l.name)
		}
		r = httptest.NewRequest(http.MethodGet, "/static/icons/icon.svg", nil)
		r.Header.Set("Origin", "https://panel.example")
		rec = httptest.NewRecorder()
		l.mux.ServeHTTP(rec, r)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: CORS on the static files of the shell", l.name)
		}
	}
}
