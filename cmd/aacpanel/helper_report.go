package main

import (
	"context"
	"encoding/json"
	"net/http"

	"aacpanel/internal/action"
)

type helperReportKey struct{}

// The host has the opening proof; the panel has the sleep record. Resolve
// the recipient by conversation here, then use the ordinary letter path so
// its wake, pause removal and journal cannot diverge from an agent's letter.
func (s *Server) helperReport(w http.ResponseWriter, r *http.Request, params map[string]any) {
	if local, _ := r.Context().Value(localKey{}).(bool); !local {
		http.Error(w, "helper reports use the local listener", http.StatusForbidden)
		return
	}
	if s.db == nil || s.host == nil || s.exec == nil {
		http.Error(w, "helper reports need the database and a connected host", http.StatusServiceUnavailable)
		return
	}
	var report action.HelperReport
	raw, err := json.Marshal(params["report"])
	if err == nil {
		err = json.Unmarshal(raw, &report)
	}
	text, _ := params["text"].(string)
	if err == nil {
		err = (action.Request{ID: "report", Kind: action.SessionLetter, Target: "parent", Report: &report, Conversation: report.Parent, Text: text}).Validate()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pauses, err := s.sessionPauses(r.Context())
	if err != nil {
		historyError(w, err)
		return
	}
	name := ""
	if paused, ok := pauseConversation(pauses, report.Parent); ok && paused.Sleeping {
		name = paused.Session
	}
	if name == "" {
		if live, ok := s.host.LiveSessionOf(report.Parent); ok {
			name = live.Name
		}
	}
	if name == "" {
		http.Error(w, "the report recipient is closed and has no sleep to wake", http.StatusNotFound)
		return
	}
	ctx := context.WithValue(r.Context(), helperReportKey{}, report)
	ctx = context.WithValue(ctx, automaticActionKey{}, "helper reports")
	if err := s.automaticAction(ctx, string(action.SessionLetter), name, map[string]any{"text": text, "conversation": report.Parent}); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "logged": true, "detail": "the helper's report was delivered"})
}
