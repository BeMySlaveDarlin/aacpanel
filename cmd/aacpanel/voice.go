package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/action"
)

type voiceKey struct{}

// The voice listener has no panel routes: possession of its token permits one
// action only, even when the panel's registry grows. The actor is attached here,
// never taken from a header or included in the text the session receives.
func (s *Server) voiceHandler(tokenPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := voiceToken(tokenPath)
		if err != nil {
			http.Error(w, "the voice token is unavailable", http.StatusServiceUnavailable)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "a voice token is required", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/session.send" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "POST is required", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			To   string `json:"to"`
			Text string `json:"text"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, actionBodyMax))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "the request must contain only to and text", http.StatusBadRequest)
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			http.Error(w, "one request is required", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(map[string]any{
			"kind": action.SessionSend, "target": body.To,
			"params": map[string]any{"text": body.Text},
		})
		r = r.WithContext(context.WithValue(r.Context(), voiceKey{}, true))
		r.Body = io.NopCloser(bytes.NewReader(payload))
		s.runAction(w, r, false)
	})
}

// Read on each request so replacing the file revokes the old token even on an
// existing connection. Mount the directory, not the token's inode, in consumers.
func voiceToken(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("the voice token must be a regular file with mode 0600")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 || len(raw) > 4096 || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("the voice token must have 32 to 4096 characters without whitespace")
	}
	return token, nil
}

func (s *Server) voiceServer(dir string) (*http.Server, net.Listener, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("voice directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return nil, nil, fmt.Errorf("the voice directory must have mode 0700 and belong to the service uid")
	}
	tokenPath := filepath.Join(dir, "token")
	if _, err := voiceToken(tokenPath); err != nil {
		return nil, nil, fmt.Errorf("voice token: %w", err)
	}
	path := filepath.Join(dir, "sock")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, nil, fmt.Errorf("the voice socket path is occupied by a non-socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, nil, fmt.Errorf("stale voice socket: %w", err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("voice socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, nil, fmt.Errorf("voice socket permissions: %w", err)
	}
	return &http.Server{
		Handler:           s.voiceHandler(tokenPath),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}, ln, nil
}
