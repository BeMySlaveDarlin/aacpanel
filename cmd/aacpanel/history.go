package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/store"
)

func (s *Server) apiHistory(w http.ResponseWriter, r *http.Request) {
	hostID, ok := s.historyHost(w, r)
	if !ok {
		return
	}
	from, to, err := parsePeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	series, err := s.db.SeriesFor(r.Context(), store.SeriesReq{
		HostID:     hostID,
		Subject:    r.URL.Query().Get("subject"),
		Metric:     r.URL.Query().Get("metric"),
		From:       from,
		To:         to,
		MaxPoints:  intParam(r, "points", 0),
		Resolution: r.URL.Query().Get("resolution"),
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, series)
}

func (s *Server) apiHistoryTop(w http.ResponseWriter, r *http.Request) {
	hostID, ok := s.historyHost(w, r)
	if !ok {
		return
	}
	from, to, err := parsePeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	top, err := s.db.TopFor(r.Context(), store.TopReq{
		HostID:     hostID,
		Metric:     r.URL.Query().Get("metric"),
		From:       from,
		To:         to,
		Limit:      intParam(r, "limit", 10),
		Resolution: r.URL.Query().Get("resolution"),
		SortBy:     r.URL.Query().Get("by"),
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, top)
}

func (s *Server) apiHistorySessions(w http.ResponseWriter, r *http.Request) {
	hostID, ok := s.historyHost(w, r)
	if !ok {
		return
	}
	from, to, err := parsePeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	list, err := s.db.SessionsFor(r.Context(), store.SessionsReq{
		HostID:     hostID,
		From:       from,
		To:         to,
		MaxPoints:  intParam(r, "points", 0),
		Limit:      intParam(r, "limit", 0),
		Offset:     intParam(r, "offset", 0),
		Resolution: r.URL.Query().Get("resolution"),
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, list)
}

func (s *Server) apiHistoryNet(w http.ResponseWriter, r *http.Request) {
	hostID, ok := s.historyHost(w, r)
	if !ok {
		return
	}
	from, to, err := parsePeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	traffic, err := s.db.NetFor(r.Context(), store.NetReq{
		HostID:     hostID,
		From:       from,
		To:         to,
		MaxPoints:  intParam(r, "points", 0),
		Resolution: r.URL.Query().Get("resolution"),
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, traffic)
}

func (s *Server) apiDegradations(w http.ResponseWriter, r *http.Request) {
	hostID, ok := s.historyHost(w, r)
	if !ok {
		return
	}
	list, err := s.db.Degradations(r.Context(), store.DegradationReq{
		HostID: hostID,
		Ratio:  floatParam(r, "ratio", 0),
		Window: time.Duration(intParam(r, "window", 0)) * time.Minute,
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"degradations": list})
}

func (s *Server) historyHost(w http.ResponseWriter, r *http.Request) (int, bool) {
	if s.db == nil {
		http.Error(w, "history is off: the database is not configured", http.StatusServiceUnavailable)
		return 0, false
	}
	hostID, err := s.db.HostID(r.Context(), s.hostName)
	if err != nil {
		historyError(w, err)
		return 0, false
	}
	return hostID, true
}

func historyError(w http.ResponseWriter, err error) {
	var bad store.ErrBadRequest
	switch {
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), http.StatusBadRequest)
	case errors.Is(err, store.ErrUnavailable), errors.Is(err, store.ErrClosed):
		http.Error(w, "history is unavailable: the database does not answer", http.StatusServiceUnavailable)
	default:
		log.Printf("history: %v", err)
		http.Error(w, "history is unavailable", http.StatusInternalServerError)
	}
}

func parsePeriod(r *http.Request) (from, to time.Time, err error) {
	q := r.URL.Query()
	to = time.Now()

	if v := q.Get("to"); v != "" {
		sec, e := strconv.ParseInt(v, 10, 64)
		if e != nil {
			return from, to, fmt.Errorf("to: a unix timestamp is required, got %q", v)
		}
		to = time.Unix(sec, 0)
	}
	if v := q.Get("from"); v != "" {
		sec, e := strconv.ParseInt(v, 10, 64)
		if e != nil {
			return from, to, fmt.Errorf("from: a unix timestamp is required, got %q", v)
		}
		from = time.Unix(sec, 0)
	} else {
		d, e := parseSpan(q.Get("period"))
		if e != nil {
			return from, to, e
		}
		from = to.Add(-d)
	}

	if !from.Before(to) {
		return from, to, fmt.Errorf("the period is empty: from is not earlier than to")
	}
	if to.Sub(from) > 400*24*time.Hour {
		return from, to, fmt.Errorf("the period is longer than a year: there is no such history anyway")
	}
	return from, to, nil
}

func parseSpan(v string) (time.Duration, error) {
	if v == "" {
		return time.Hour, nil
	}
	if days, ok := strings.CutSuffix(v, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("period: %q is not understood", v)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("period: %q is not understood", v)
	}
	return d, nil
}
