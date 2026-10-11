package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

// The startup import retries while the database migrates or the collector is
// down, then stops for good. Requests share its lock so a first editor read
// does not read the host file a second time.
func (s *Server) runBriefGuideImport(ctx context.Context) {
	failed := ""
	for {
		_, err := s.briefGuide(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		if err.Error() != failed {
			log.Printf("the brief guide was not imported: %v", err)
			failed = err.Error()
		}
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Server) briefGuide(ctx context.Context) (string, error) {
	if s.db == nil {
		return "", errors.New("the brief guide is unavailable: the database is not configured")
	}
	s.briefGuideMu.Lock()
	defer s.briefGuideMu.Unlock()
	text, found, err := s.db.BriefGuide(ctx)
	if err != nil || found {
		return text, err
	}
	text, err = s.chat.LegacyBriefGuide(ctx)
	if err != nil {
		return "", err
	}
	if err := s.db.ImportBriefGuide(ctx, text); err != nil {
		return "", err
	}
	// A save may have won the insert while the host was being read.
	text, _, err = s.db.BriefGuide(ctx)
	return text, err
}

func (s *Server) apiBriefGuide(w http.ResponseWriter, r *http.Request) {
	text, err := s.briefGuide(r.Context())
	if err != nil {
		http.Error(w, "the brief guide was not read: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{"text": text})
}

func (s *Server) apiBriefGuideSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text *string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil || body.Text == nil {
		http.Error(w, "the brief guide must contain text and fit in 4 MB", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "the brief guide cannot be saved: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.db.SaveBriefGuide(r.Context(), *body.Text); err != nil {
		http.Error(w, "the brief guide was not saved: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]string{"text": *body.Text})
}
