package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"aacpanel/internal/store"
	"aacpanel/internal/termlink"
)

type terminals struct {
	client *termlink.Client

	mu   sync.Mutex
	live map[string]*termlink.Stream
}

func newTerminals(client *termlink.Client) *terminals {
	return &terminals{client: client, live: map[string]*termlink.Stream{}}
}

func (t *terminals) enabled() bool { return t != nil && t.client.Enabled() }

func (t *terminals) add(stream *termlink.Stream) string {
	buf := make([]byte, 16)
	rand.Read(buf)
	id := hex.EncodeToString(buf)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.live[id] = stream
	return id
}

func (t *terminals) get(id string) (*termlink.Stream, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.live[id]
	return s, ok
}

func (t *terminals) drop(id string) {
	t.mu.Lock()
	stream := t.live[id]
	delete(t.live, id)
	t.mu.Unlock()
	if stream != nil {
		stream.Close()
	}
}

func (s *Server) apiTermStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"available": false}
	switch {
	case !s.terms.enabled():
		out["reason"] = "the executor is not configured: there is nobody to open a terminal"
	case !s.terms.client.Available(r.Context()):
		out["reason"] = "the executor does not answer on the terminal socket — was aacpanel-exec built without the " +
			"terminal? rebuild it"
		if s.exec != nil {
			if err := s.exec.Reach(r.Context()); err != nil {
				out["reason"] = err.Error()
			}
		}
	default:
		out["available"] = true
	}
	writeJSON(w, out)
}

func (s *Server) apiTermStream(w http.ResponseWriter, r *http.Request) {
	if !s.terms.enabled() {
		http.Error(w, "the terminal is not configured", http.StatusServiceUnavailable)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said which session to attach to", http.StatusBadRequest)
		return
	}
	cols, rows, err := sizeFrom(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	journal := s.termJournal(r, name)

	stream, err := s.terms.client.Open(r.Context(), name, cols, rows)
	if err != nil {
		if !wantsSSE(r) {
			journal.done(r.Context(), store.ActionFailed, err.Error())
			code := http.StatusBadGateway
			if errors.Is(err, termlink.ErrUnavailable) {
				code = http.StatusServiceUnavailable
			}
			http.Error(w, err.Error(), code)
			return
		}
		journal.done(r.Context(), store.ActionFailed, err.Error())
		sseHeaders(w)
		w.WriteHeader(http.StatusOK)
		done, _ := json.Marshal(map[string]string{"reason": err.Error()})
		fmt.Fprintf(w, "event: end\ndata: %s\n\n", done)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	id := s.terms.add(stream)
	defer s.terms.drop(id)
	defer journal.done(context.WithoutCancel(r.Context()), store.ActionOK, "the bridge is closed")

	stop := context.AfterFunc(r.Context(), func() { stream.Close() })
	defer stop()

	sseHeaders(w)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	send := func(event, data string) bool {
		if event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
				return false
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}

	ready, _ := json.Marshal(map[string]string{"id": id, "kind": stream.Kind(), "detail": stream.Detail()})
	if !send("ready", string(ready)) {
		return
	}

	buf := make([]byte, termlink.MaxChunk)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if !send("", base64.StdEncoding.EncodeToString(buf[:n])) {
				return
			}
		}
		if err != nil {
			reason := stream.End()
			if reason != "" {
				log.Printf("terminal %s: %s", name, reason)
			}
			done, _ := json.Marshal(map[string]string{"reason": reason})
			send("end", string(done))
			return
		}
	}
}

func (s *Server) apiTermInput(w http.ResponseWriter, r *http.Request) {
	stream, ok := s.terms.get(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, "the terminal is already closed", http.StatusGone)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, termlink.MaxChunk+1))
	if err != nil {
		http.Error(w, "the input was not read", http.StatusBadRequest)
		return
	}
	if len(body) > termlink.MaxChunk {
		http.Error(w, "the input is longer than "+strconv.Itoa(termlink.MaxChunk)+" bytes", http.StatusRequestEntityTooLarge)
		return
	}
	if _, err := stream.Write(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiTermSize(w http.ResponseWriter, r *http.Request) {
	stream, ok := s.terms.get(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, "the terminal is already closed", http.StatusGone)
		return
	}
	cols, rows, err := sizeFrom(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := stream.Resize(cols, rows); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sizeFrom(r *http.Request) (cols, rows uint16, err error) {
	q := r.URL.Query()
	c, cerr := strconv.Atoi(q.Get("cols"))
	rw, rerr := strconv.Atoi(q.Get("rows"))
	if cerr != nil || rerr != nil {
		return 0, 0, errors.New("the window size is not given: cols and rows are required")
	}
	if c <= 0 || rw <= 0 || c > termlink.MaxCols || rw > termlink.MaxRows {
		return 0, 0, errors.New("the window size is outside sane limits")
	}
	return uint16(c), uint16(rw), nil
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
}

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

type termEntry struct {
	db   *store.Store
	id   int64
	done func(ctx context.Context, result, detail string)
}

func (s *Server) termJournal(r *http.Request, name string) termEntry {
	quiet := termEntry{done: func(context.Context, string, string) {}}
	if s.db == nil || s.auth == nil {
		return quiet
	}
	deviceID := s.auth.CurrentDevice(r)
	if deviceID == 0 {
		return quiet
	}
	deviceName := s.passkey.DeviceName(r.Context(), deviceID)
	if deviceName == "" {
		deviceName = "unknown device"
	}
	id, err := s.db.LogAttempt(r.Context(), store.Action{
		DeviceID:   deviceRef(deviceID),
		DeviceName: deviceName,
		Kind:       "term.attach",
		Target:     name,
	})
	if err != nil {
		log.Printf("terminal log: %v", err)
		return quiet
	}
	var once sync.Once
	return termEntry{db: s.db, id: id, done: func(ctx context.Context, result, detail string) {
		once.Do(func() {
			if err := s.db.LogResult(ctx, id, store.ActionOutcome{Result: result, Detail: detail}); err != nil {
				log.Printf("terminal log: %v", err)
			}
		})
	}}
}
