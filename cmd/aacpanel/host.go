package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/store"
)

func (s *Server) apiHost(w http.ResponseWriter, r *http.Request) {
	s.noteViewing(r)

	payload, err := s.hostSnapshot(r.Context())
	if err != nil {
		log.Printf("host state: %v", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(payload)
}

func (s *Server) noteViewing(r *http.Request) {
	if s.db == nil {
		return
	}
	device := s.auth.CurrentDevice(r)
	if device <= 0 {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), viewingTimeout)
	defer cancel()

	views := s.db.SessionViews()
	name := strings.TrimSpace(r.URL.Query().Get("viewing"))
	var err error
	if name == "" {
		err = views.Forget(ctx, device)
	} else {
		err = views.Mark(ctx, device, name)
	}
	if err != nil {
		log.Printf("viewing mark of device %d: %v", device, err)
	}
}

const viewingTimeout = 3 * time.Second

const (
	procsOK      = "ok"
	procsUnknown = "unknown"
)

type procRow struct {
	PID    int     `json:"pid"`
	Cmd    string  `json:"cmd"`
	User   string  `json:"user,omitempty"`
	CPUPct float64 `json:"cpuPct"`
	RSS    int64   `json:"rss"`
	MemPct float64 `json:"memPct"`
}

type procsReply struct {
	State string    `json:"state"`
	At    int64     `json:"at,omitempty"`
	Total int       `json:"total,omitempty"`
	Items []procRow `json:"items,omitempty"`
}

func procsReport(raw []byte) procsReply {
	var snapshot struct {
		Procs *struct {
			At    int64     `json:"at"`
			Total int       `json:"total"`
			Items []procRow `json:"items"`
		} `json:"procs"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Procs == nil {
		return procsReply{State: procsUnknown}
	}
	p := snapshot.Procs
	out := procsReply{State: procsOK, At: p.At, Total: p.Total, Items: []procRow{}}
	for _, row := range p.Items {
		if row.PID <= 0 || row.Cmd == "" {
			continue
		}
		out.Items = append(out.Items, row)
	}
	return out
}

func (s *Server) apiProcs(w http.ResponseWriter, r *http.Request) {
	if s.host == nil {
		writeJSON(w, procsReply{State: procsUnknown})
		return
	}
	raw, err := s.host.Raw()
	if err != nil {
		log.Printf("host processes: %v", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, procsReport(raw))
}

func (s *Server) apiFaults(w http.ResponseWriter, r *http.Request) {
	if s.writer == nil {
		writeJSON(w, map[string]any{"recording": false, "reason": "history is off: AACP_DB_DSN is not set"})
		return
	}
	faults := s.writer.SnapshotFaults()
	if faults == nil {
		faults = []store.SnapshotFault{}
	}
	writeJSON(w, map[string]any{
		"recording": true,
		"queued":    s.writer.Queued(),
		"faults":    faults,
	})
}
