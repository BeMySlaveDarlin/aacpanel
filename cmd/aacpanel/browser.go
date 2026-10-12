package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"aacpanel/internal/browserlink"
)

type browsers struct {
	path  string
	mu    sync.Mutex
	views map[string]*browserView
}

type browserView struct {
	link    *browserlink.Link
	device  int64
	local   bool
	mu      sync.Mutex
	frame   browserlink.Frame
	pulling bool
}

func newBrowsers(path string) *browsers {
	return &browsers{path: path, views: make(map[string]*browserView)}
}

func (s *Server) browserEnabled(ctx context.Context) bool {
	if s.exec == nil {
		return false
	}
	st, err := s.exec.Browser(ctx)
	return err == nil && st.Enabled
}

func (s *Server) apiBrowser(w http.ResponseWriter, r *http.Request) {
	if s.exec == nil {
		writeJSON(w, browserlink.Status{Reason: "the executor is unavailable", Profiles: []browserlink.Profile{}})
		return
	}
	st, err := s.exec.Browser(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, st)
}

func (s *Server) browserDevice(r *http.Request, stream bool) (int64, bool) {
	var id int64
	if s.auth != nil {
		id = s.auth.CurrentDevice(r)
		if stream {
			id = s.auth.CurrentStreamDevice(r)
		}
	}
	local, _ := r.Context().Value(localKey{}).(bool)
	return id, local
}

func (s *Server) browserView(r *http.Request) (*browserView, bool) {
	if s.browsers == nil {
		return nil, false
	}
	s.browsers.mu.Lock()
	v := s.browsers.views[r.URL.Query().Get("id")]
	s.browsers.mu.Unlock()
	if v == nil {
		return nil, false
	}
	id, local := s.browserDevice(r, false)
	return v, v.device == id && v.local == local
}

func browserSize(r *http.Request) (browserlink.Size, error) {
	q := r.URL.Query()
	w, ew := strconv.Atoi(q.Get("width"))
	h, eh := strconv.Atoi(q.Get("height"))
	d, ed := strconv.ParseFloat(q.Get("dpr"), 64)
	if ew != nil || eh != nil || ed != nil {
		return browserlink.Size{}, errors.New("width, height and dpr are required")
	}
	size := browserlink.Size{Width: w, Height: h, DPR: d, Mobile: q.Get("mobile") == "1"}
	return size, size.Check()
}

func (s *Server) apiBrowserStream(w http.ResponseWriter, r *http.Request) {
	if s.browsers == nil || !s.browserEnabled(r.Context()) {
		http.Error(w, "the browser is disabled on the host", http.StatusNotFound)
		return
	}
	size, err := browserSize(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	link, err := browserlink.Attach(r.Context(), s.browsers.path, r.URL.Query().Get("profile"), size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	defer link.Close()
	raw := make([]byte, 16)
	rand.Read(raw)
	id := hex.EncodeToString(raw)
	device, local := s.browserDevice(r, true)
	v := &browserView{link: link, device: device, local: local}
	s.browsers.mu.Lock()
	s.browsers.views[id] = v
	s.browsers.mu.Unlock()
	defer func() { s.browsers.mu.Lock(); delete(s.browsers.views, id); s.browsers.mu.Unlock() }()
	stop := context.AfterFunc(r.Context(), func() { link.Close() })
	defer stop()
	sseHeaders(w)
	send := func(event string, body interface{}) error {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		controller := http.NewResponseController(w)
		controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return err
		}
		return controller.Flush()
	}
	if err = send("ready", struct {
		ID string `json:"id"`
	}{id}); err != nil {
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	frames := make(chan browserlink.Frame)
	ended := make(chan error, 1)
	readCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			f, err := link.Read()
			if err != nil {
				ended <- err
				return
			}
			select {
			case frames <- f:
			case <-readCtx.Done():
				return
			}
		}
	}()
	lastPull := time.Now()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ended:
			send("end", map[string]string{"reason": "The browser connection ended"})
			return
		case <-heartbeat.C:
			v.mu.Lock()
			waiting := v.frame.Sequence != 0
			v.mu.Unlock()
			if waiting && time.Since(lastPull) > 45*time.Second {
				return
			}
			if err = link.Write(browserlink.Frame{Type: "ping"}); err != nil {
				return
			}
			if err = send("ping", struct{}{}); err != nil {
				return
			}
		case f := <-frames:
			switch f.Type {
			case "frame":
				v.mu.Lock()
				v.frame = f
				v.mu.Unlock()
				lastPull = time.Now()
				if err = send("frame", struct {
					Sequence   uint64            `json:"sequence"`
					Generation uint64            `json:"generation"`
					Size       *browserlink.Size `json:"size"`
				}{f.Sequence, f.Generation, f.Size}); err != nil {
					return
				}
			case "state":
				if err = send("state", f.State); err != nil {
					return
				}
			case "refused":
				if err = send("refused", map[string]string{"reason": f.Error}); err != nil {
					return
				}
			case "end":
				send("end", map[string]string{"reason": f.Error})
				return
			}
		}
	}
}

func (s *Server) apiBrowserFrame(w http.ResponseWriter, r *http.Request) {
	v, ok := s.browserView(r)
	if !ok {
		http.Error(w, "the browser view is closed", http.StatusGone)
		return
	}
	seq, err := strconv.ParseUint(r.URL.Query().Get("sequence"), 10, 64)
	if err != nil || seq == 0 {
		http.Error(w, "invalid browser frame", http.StatusBadRequest)
		return
	}
	v.mu.Lock()
	if v.frame.Sequence != seq || v.pulling {
		v.mu.Unlock()
		http.Error(w, "the browser frame is no longer available", http.StatusGone)
		return
	}
	f := v.frame
	v.pulling = true
	v.mu.Unlock()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(f.Data)))
	_, err = w.Write(f.Data)
	v.mu.Lock()
	v.pulling = false
	if v.frame.Sequence == seq {
		v.frame.Data = nil
		v.frame.Sequence = 0
	}
	v.mu.Unlock()
	if err == nil {
		v.link.Write(browserlink.Frame{Type: "ack", Sequence: seq})
	} else {
		v.link.Close()
	}
}

func (s *Server) apiBrowserInput(w http.ResponseWriter, r *http.Request) {
	v, ok := s.browserView(r)
	if !ok {
		http.Error(w, "the browser view is closed", http.StatusGone)
		return
	}
	var batch struct {
		Events []browserlink.Input `json:"events"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 32*browserlink.MaxText+4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil || len(batch.Events) == 0 || len(batch.Events) > 32 {
		http.Error(w, "the browser input batch did not parse", http.StatusBadRequest)
		return
	}
	for _, input := range batch.Events {
		if err := input.Check(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	for _, input := range batch.Events {
		if err := v.link.Write(browserlink.Frame{Type: "input", Input: &input}); err != nil {
			http.Error(w, "the browser input could not be sent", http.StatusBadGateway)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiBrowserSize(w http.ResponseWriter, r *http.Request) {
	v, ok := s.browserView(r)
	if !ok {
		http.Error(w, "the browser view is closed", http.StatusGone)
		return
	}
	size, err := browserSize(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = v.link.Write(browserlink.Frame{Type: "size", Size: &size}); err != nil {
		http.Error(w, "the browser size could not be sent", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
