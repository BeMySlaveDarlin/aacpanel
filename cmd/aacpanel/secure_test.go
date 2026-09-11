package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInsecureCookieIsReportedOnlyOverHTTPS(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
		proto  string
		tls    bool
		want   bool
	}{
		{"flag on, proxied https", true, "https", false, false},
		{"flag off, plain http", false, "", false, false},
		{"flag off, proxied https", false, "https", false, true},
		{"flag off, tls listener", false, "", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := &Server{host: hostWith(t, `{"at": 1}`), secure: c.secure}
			h := srv.handler(srv.localGate())

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			if c.proto != "" {
				req.Header.Set("X-Forwarded-Proto", c.proto)
			}
			if c.tls {
				req.TLS = &tls.ConnectionState{}
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			rec := httptest.NewRecorder()
			srv.apiHost(rec, httptest.NewRequest(http.MethodGet, "/api/host", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("the snapshot: %d %s", rec.Code, rec.Body.String())
			}
			var got struct {
				CookieInsecure bool `json:"cookieInsecure"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.CookieInsecure != c.want {
				t.Fatalf("cookieInsecure = %v, expected %v: %s", got.CookieInsecure, c.want, rec.Body.String())
			}
		})
	}
}

func TestInsecureCookieMarkIsSticky(t *testing.T) {
	srv := &Server{host: hostWith(t, `{"at": 1}`), secure: false}
	h := srv.handler(srv.localGate())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(httptest.NewRecorder(), req)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	rec := httptest.NewRecorder()
	srv.apiHost(rec, httptest.NewRequest(http.MethodGet, "/api/host", nil))
	var got struct {
		CookieInsecure bool `json:"cookieInsecure"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.CookieInsecure {
		t.Fatalf("the mark was cleared by a request over http: %s", rec.Body.String())
	}
}
