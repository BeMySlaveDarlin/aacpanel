package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/store"
	"aacpanel/internal/usage"
)

var usageAll = time.Unix(0, 0)

func (s *Server) apiUsageScan(w http.ResponseWriter, r *http.Request) {
	if s.usage == nil || !s.usage.Available() {
		writeJSON(w, map[string]any{"available": false,
			"reason": "usage collection is not configured: the agent does not answer or the database is down"})
		return
	}
	out := map[string]any{"available": true, "state": s.usage.State()}
	if pong, err := s.usage.Ping(r.Context()); err != nil {
		out["agentError"] = err.Error()
	} else {
		out["workers"] = pong.Workers
	}
	if at, files, err := s.db.UsageScanAt(r.Context()); err == nil {
		out["files"] = files
		if !at.IsZero() {
			out["scannedAt"] = at.Unix()
		}
	}
	if !s.usage.State().Running && r.URL.Query().Get("estimate") != "0" {
		est, err := s.usage.Estimate(r.Context())
		if err != nil {
			out["estimateError"] = err.Error()
		} else {
			out["estimate"] = est
		}
	}
	writeJSON(w, out)
}

func (s *Server) apiUsageScanStart(w http.ResponseWriter, r *http.Request) {
	if s.usage == nil || !s.usage.Available() {
		http.Error(w, "usage collection is not configured", http.StatusServiceUnavailable)
		return
	}
	if s.usage.State().Running {
		writeStatusJSON(w, http.StatusConflict, map[string]any{"error": usage.ErrBusy.Error()})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), usageScanTimeout)
		defer cancel()
		if err := s.usage.Run(ctx); err != nil && !errors.Is(err, usage.ErrBusy) {
			log.Printf("usage collection failed: %v", err)
		}
	}()
	writeStatusJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

const usageScanTimeout = time.Hour

func (s *Server) apiUsageSummary(w http.ResponseWriter, r *http.Request) {
	f, err := usageFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sum, err := s.db.UsageSummaryFor(r.Context(), f)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, usageSummaryBody(sum, f))
}

func usageSummaryBody(sum store.UsageSummary, f store.UsageFilter) map[string]any {
	return map[string]any{
		"summary":     sum,
		"hit":         sum.Hit,
		"inbound":     sum.Inbound,
		"subShareIn":  sum.SubShareIn(),
		"subShareOut": sum.SubShareOut(),
		"from":        f.From.Unix(),
		"to":          f.To.Unix(),
	}
}

func (s *Server) apiUsageSeries(w http.ResponseWriter, r *http.Request) {
	f, err := usageFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	step := r.URL.Query().Get("step")
	points, err := s.db.UsageSeriesFor(r.Context(), f, step)
	if err != nil {
		historyError(w, err)
		return
	}
	if step == "" {
		step = "hour"
	}
	writeJSON(w, map[string]any{"step": step, "points": points})
}

func (s *Server) apiUsageBreakdown(w http.ResponseWriter, r *http.Request) {
	f, err := usageFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	by := r.URL.Query().Get("by")
	if by == "" {
		by = store.UsageByProject
	}
	rows, err := s.db.UsageBreakdownFor(r.Context(), f, by, intParam(r, "limit", 0))
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"by": by, "rows": rows})
}

func (s *Server) apiUsageModels(w http.ResponseWriter, r *http.Request) {
	f, err := usageFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	models, err := s.db.UsageModelsFor(r.Context(), f)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"models": models})
}

func (s *Server) apiUsageTools(w http.ResponseWriter, r *http.Request) {
	f, err := usageFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tools, err := s.db.UsageToolsFor(r.Context(), f, intParam(r, "limit", 0))
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, tools)
}

func (s *Server) apiUsageContours(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.UsageContours(r.Context())
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"contours": list})
}

func usageFilter(r *http.Request) (store.UsageFilter, error) {
	q := r.URL.Query()
	f := store.UsageFilter{
		To:      time.Now(),
		Group:   q.Get("group"),
		Project: q.Get("project"),
		Session: q.Get("session"),
		Zone:    q.Get("tz"),
	}
	if v := q.Get("to"); v != "" {
		sec, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, errors.New("to: a unix timestamp is required")
		}
		f.To = time.Unix(sec, 0)
	}
	switch v := q.Get("from"); {
	case v != "":
		sec, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, errors.New("from: a unix timestamp is required")
		}
		f.From = time.Unix(sec, 0)
	case q.Get("period") == "all":
		f.From = usageAll
	default:
		d, err := parseSpan(orDefault(q.Get("period"), "30d"))
		if err != nil {
			return f, err
		}
		f.From = f.To.Add(-d)
	}
	if !f.From.Before(f.To) {
		return f, errors.New("the period is empty: from is not earlier than to")
	}
	if c := q["contour"]; len(c) > 0 {
		f.Contours = splitContours(c)
	}
	f.Outside = q.Get("outside") == "1"
	return f, nil
}

func splitContours(values []string) []string {
	out := []string{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
