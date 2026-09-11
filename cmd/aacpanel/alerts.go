package main

import (
	"aacpanel/internal/settings"
	"net/http"
	"strconv"

	"aacpanel/internal/store"
)

func (s *Server) apiAlerts(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "alerts are unavailable: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	list, err := s.db.Alerts(r.Context(), store.AlertsReq{
		Limit:  intParam(r, "limit", 50),
		Before: int64(intParam(r, "before", 0)),
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"alerts": list})
}

func (s *Server) apiAlertAck(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "alerts are unavailable: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "an alert id is required", http.StatusBadRequest)
		return
	}
	ok, err := s.db.Ack(r.Context(), id)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"acked": ok})
}

func (s *Server) apiProbes(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "probes are unavailable: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	list, err := s.db.Probes(r.Context())
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"probes": list})
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"settings": settings.Collect("")})
}
