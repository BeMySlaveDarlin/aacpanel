package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func localOnly(port string, panel func(string) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reason := crossSite(r, port, panel); reason != "" {
			http.Error(w, "the local panel takes only its own requests: "+reason, http.StatusForbidden)
			log.Printf("the local panel rejected %s %s: %s", r.Method, r.URL.Path, reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func crossSite(r *http.Request, port string, panel func(string) bool) string {
	if panel != nil && panel(r.Header.Get("Origin")) && (r.URL.Path == "/probe" || corsPath(r.URL.Path)) {
		if !loopbackHost(r.Host, port) {
			return "foreign host name " + r.Host
		}
		return ""
	}
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "", "same-origin", "none":
	default:
		if !navigation(r) {
			return "a request from outside (Sec-Fetch-Site: " + site + ")"
		}
	}

	if origin := r.Header.Get("Origin"); origin != "" && !loopbackURL(origin, port) {
		return "foreign origin " + origin
	}

	if !loopbackHost(r.Host, port) {
		return "foreign host name " + r.Host
	}
	return ""
}

func navigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("Sec-Fetch-Mode") != "navigate" {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "document", "empty":
		return true
	}
	return false
}

func frameDeny(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func loopbackURL(raw, port string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	return loopbackHost(u.Host, port)
}

func loopbackHost(host, port string) bool {
	name, p, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	if p != port {
		return false
	}
	switch strings.ToLower(name) {
	case "localhost", "127.0.0.1", "[::1]", "::1":
		return true
	}
	return false
}

func portOf(addr string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("the local panel address %q: %w (a form like :8777 is expected)", addr, err)
	}
	if port == "" {
		return "", fmt.Errorf("the local panel address %q has no port", addr)
	}
	return port, nil
}

func (s *Server) localServer(addr string) (*http.Server, error) {
	port, err := portOf(addr)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              addr,
		Handler:           logRequests(frameDeny(localOnly(port, s.endpoints.allowedOrigin, s.handler(s.localGate())))),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}, nil
}
