package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *Server) apiTree(w http.ResponseWriter, r *http.Request) {
	tree, err := s.docker.Tree(r.Context(), s.hub.SnapshotMetrics())
	if err != nil {
		log.Printf("tree: %v", err)
		http.Error(w, "docker is unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(tree)
}

func (s *Server) apiStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	sub := s.hub.Subscribe()
	defer s.hub.Unsubscribe(sub)

	for {
		select {
		case <-r.Context().Done():
			return
		case payload, ok := <-sub:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: tree\ndata: %s\n\n", payload); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

func (s *Server) apiLogs(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "an id is required", http.StatusBadRequest)
		return
	}
	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			tail = n
		}
	}

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	var mu sync.Mutex
	send := func(event, data string) bool {
		mu.Lock()
		defer mu.Unlock()
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	err := s.docker.Logs(ctx, id, tail, func(stream, line string) {
		at, text := splitLogTime(line)
		payload, _ := json.Marshal(logEvent{Stream: stream, Line: text, At: at})
		if !send("log", string(payload)) {
			cancel()
		}
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("logs of %s: %v", id, err)
		payload, _ := json.Marshal(map[string]string{"error": err.Error()})
		send("failed", string(payload))
		return
	}
	send("eof", "{}")
}

type logEvent struct {
	Stream string `json:"stream"`
	Line   string `json:"line"`
	At     string `json:"at,omitempty"`
}

func splitLogTime(line string) (at, text string) {
	head, rest, ok := strings.Cut(line, " ")
	if !ok {
		if _, err := time.Parse(time.RFC3339Nano, line); err == nil {
			return line, ""
		}
		return "", line
	}
	if _, err := time.Parse(time.RFC3339Nano, head); err != nil {
		return "", line
	}
	return head, rest
}
