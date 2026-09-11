package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type endpoint struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type endpoints struct {
	list  []endpoint
	panel string
}

const (
	kindLocal  = "local"
	kindLAN    = "lan"
	kindPublic = "public"
	kindTS     = "ts"
)

func loadEndpoints(secret, public, lan, ts, localAddr, localURL string) (endpoints, error) {
	var out endpoints
	add := func(kind, raw string) error {
		if raw == "" {
			return nil
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("the panel address (%s) %q: an origin like https://panel.example without a path is expected", kind, raw)
		}
		out.list = append(out.list, endpoint{Kind: kind, URL: u.Scheme + "://" + u.Host})
		return nil
	}
	if localURL == "" && localAddr != "" {
		port, err := portOf(localAddr)
		if err != nil {
			return endpoints{}, err
		}
		localURL = "http://127.0.0.1:" + port
	}
	for _, e := range []struct{ kind, raw string }{{kindLocal, localURL}, {kindLAN, lan}, {kindPublic, public}, {kindTS, ts}} {
		if err := add(e.kind, e.raw); err != nil {
			return endpoints{}, err
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("aacpanel.instance"))
	out.panel = hex.EncodeToString(mac.Sum(nil))[:16]
	return out, nil
}

func (e endpoints) origin(kind string) string {
	for _, it := range e.list {
		if it.Kind == kind {
			return it.URL
		}
	}
	return ""
}

func (e endpoints) allowedOrigin(origin string) bool {
	for _, it := range e.list {
		if it.URL == origin {
			return true
		}
	}
	return false
}

func (s *Server) apiEndpoints(here string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list := s.endpoints.list
		if list == nil {
			list = []endpoint{}
		}
		writeJSON(w, map[string]any{"panel": s.endpoints.panel, "here": here, "endpoints": list})
	}
}

func (s *Server) apiSessionToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.auth.IssueBearer(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"token": token})
}

func corsPath(path string) bool {
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/dist/")
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !corsPath(r.URL.Path) || !s.endpoints.allowedOrigin(origin) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
		h.Set("Access-Control-Allow-Private-Network", "true")
		h.Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) probe(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.endpoints.allowedOrigin(origin) {
				http.Error(w, "the probe answers only its own addresses", http.StatusForbidden)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET")
			h.Set("Access-Control-Allow-Private-Network", "true")
			h.Set("Access-Control-Max-Age", "600")
			h.Add("Vary", "Origin")
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, map[string]any{"panel": s.endpoints.panel, "kind": kind})
	}
}

func (s *Server) lanGate() gate {
	g := s.publicGate()
	g.kind = kindLAN
	return g
}

func (s *Server) tsGate() gate {
	g := s.publicGate()
	g.kind = kindTS
	return g
}

func (s *Server) loginHome(g gate) string {
	if g.kind == kindLAN || g.kind == kindTS {
		return s.endpoints.origin(kindPublic)
	}
	return ""
}
