package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"aacpanel/internal/chat"
)

// A page is named by its published address or by a hash of where it came from,
// and the name travels in a path. Anything outside this shape never reaches
// the collector.
var pageIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func pageFail(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrUnavailable) {
		http.Error(w, "the published pages are unavailable: the session collector on the host does not answer. Is aacpanel-agent running?",
			http.StatusServiceUnavailable)
		return
	}
	if errors.Is(err, chat.ErrNoPages) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusNotFound)
}

// apiPages lists the pages kept for a session, or for the whole machine.
func (s *Server) apiPages(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "the published pages are unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	session := r.URL.Query().Get("session")
	if session != "" && !uuidRE.MatchString(session) {
		http.Error(w, "the conversation id does not look like an id", http.StatusBadRequest)
		return
	}
	cards, err := s.chat.Pages(r.Context(), session)
	if err != nil {
		pageFail(w, err)
		return
	}
	writeJSON(w, map[string]any{"pages": cards})
}

// apiPage hands back the copy of one page as the document itself.
//
// It is served as html and not as json because that is what the reader loads
// it into: a frame with a src costs nothing on a phone, while the same page
// inlined into a document attribute is copied through the whole screen.
//
// The page is somebody else's code, so it is served sandboxed by the response
// itself: the header holds even when the address is opened on its own, outside
// any frame. Without allow-same-origin the page gets an origin of its own and
// the session cookie of the panel is not its to read.
func (s *Server) apiPage(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "the published pages are unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if !pageIDRE.MatchString(id) {
		http.Error(w, "this is not the name of a page", http.StatusBadRequest)
		return
	}
	page, err := s.chat.PageOf(r.Context(), id)
	if err != nil {
		pageFail(w, err)
		return
	}
	body := []byte(page.HTML)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-popups allow-forms")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(body); err != nil {
		return
	}
}

// apiPageCard returns what the shelf knows about one page, without its html.
func (s *Server) apiPageCard(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "the published pages are unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if !pageIDRE.MatchString(id) {
		http.Error(w, "this is not the name of a page", http.StatusBadRequest)
		return
	}
	page, err := s.chat.PageOf(r.Context(), id)
	if err != nil {
		pageFail(w, err)
		return
	}
	card := page.PageCard
	card.Bytes = int64(len(page.HTML))
	out, err := json.Marshal(map[string]any{"page": card, "firstAt": page.FirstAt})
	if err != nil {
		http.Error(w, "the page was not packed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}
