package main

import (
	"encoding/json"
	"log"
	"net/http"

	"aacpanel/internal/auth"
	"aacpanel/internal/notify"
)

func (s *Server) apiPushKey(w http.ResponseWriter, r *http.Request) {
	key := s.push.PublicKey()
	if key == "" {
		http.Error(w, "push notifications are not ready yet", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"key": key})
}

func (s *Server) apiPushTest(w http.ResponseWriter, r *http.Request) {
	if !s.push.Ready() {
		http.Error(w, "push notifications are not ready yet", http.StatusServiceUnavailable)
		return
	}
	s.push.Send(notify.Message{
		Title:    "aacpanel is on the line",
		Body:     "Test notification: delivery works",
		Tag:      "test",
		Severity: notify.Info,
	})
	log.Printf("%s: test notification sent", auth.ClientIP(r))
	w.WriteHeader(http.StatusAccepted)
}
