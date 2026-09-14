package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"aacpanel/internal/auth"
)

func (s *Server) protect(h http.HandlerFunc) http.Handler {
	return s.gate(h, true)
}

func (s *Server) protectStream(h http.HandlerFunc) http.Handler {
	return s.gate(h, false)
}

func (s *Server) gate(h http.HandlerFunc, refresh bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		check := s.auth.VerifyStream
		if refresh {
			check = func(r *http.Request) error { return s.auth.Guard(w, r, s.secure) }
		}
		if err := check(r); err != nil && !s.tokens.Bearer(r) {
			reason := auth.Reason(err)
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				body := map[string]any{"error": err.Error(), "reason": reason}
				if sec := expiredAfter(s.auth.TTL(), err); sec > 0 {
					body["after_sec"] = sec
				}
				json.NewEncoder(w).Encode(body)
				return
			}
			// The query is part of the address to come back to: a push about a
			// question leads to a named session, not to the panel at large.
			back := safeNext(r.URL.RequestURI())
			http.Redirect(w, r, "/login?reason="+reason+"&next="+url.QueryEscape(back), http.StatusSeeOther)
			return
		}

		if deadline, ok := s.auth.Deadline(r, !refresh); ok {
			ctx, cancel := context.WithDeadline(r.Context(), deadline)
			defer cancel()
			r = r.WithContext(ctx)
		}
		h(w, r)
	})
}

func expiredAfter(ttl auth.SessionTTL, err error) int {
	switch {
	case errors.Is(err, auth.ErrIdle):
		return int(ttl.Idle.Seconds())
	case errors.Is(err, auth.ErrExpired):
		return int(ttl.Absolute.Seconds())
	default:
		return 0
	}
}
