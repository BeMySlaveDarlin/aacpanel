package main

import (
	"errors"
	"net/http"

	"aacpanel/internal/action"
)

// apiSecrets lists the secrets the host keeps: the directory, and the name,
// size and time of every file in it. What a file holds never leaves the host
// this way — the list is a GET the service worker keeps a copy of.
func (s *Server) apiSecrets(w http.ResponseWriter, r *http.Request) {
	if s.exec == nil {
		http.Error(w, "the executor is not configured: there is nobody to keep secrets", http.StatusServiceUnavailable)
		return
	}
	list, err := s.exec.Secrets(r.Context())
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, action.ErrUnavailable) {
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	if list.Secrets == nil {
		list.Secrets = []action.SecretFile{}
	}
	writeJSON(w, list)
}
