package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/store"
)

// What a reading is allowed to hold. The ceilings are here rather than in the
// database because a request is where a number too large arrives: the store
// would take it and the screen would then have to draw it.
const (
	maxNotes     = 200
	maxNoteText  = 4000
	maxNoteQuote = 2000
)

// apiReviews answers with the index: every reading with its status, newest
// first. A screen showing what is in hand reads this one answer instead of
// opening the readings one at a time.
func (s *Server) apiReviews(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "readings are not kept: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.db.Reviews(r.Context(), strings.TrimSpace(r.URL.Query().Get("session")), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"reviews": list})
}

// apiReviewNew starts a reading and names it. The name is made here rather
// than in the browser: it becomes the name of a file on a shelf, and a name
// that arrives from outside is a path somebody else chose.
func (s *Server) apiReviewNew(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
		Cwd     string `json:"cwd"`
		Base    string `json:"base"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		http.Error(w, "the reading did not parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	session := strings.TrimSpace(body.Session)
	if session == "" {
		http.Error(w, "a reading belongs to a conversation, and this one names none", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "readings are not kept: the database is not configured", http.StatusServiceUnavailable)
		return
	}

	id := reviewName(session, time.Now().UTC())
	review := store.Review{
		ID: id, Session: session,
		Cwd:  strings.TrimSpace(body.Cwd),
		Base: strings.TrimSpace(body.Base),
	}
	if err := s.db.SaveReview(r.Context(), review); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	made, err := s.db.ReviewOf(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, made)
}

// reviewName is the name of a reading: the conversation it belongs to and when
// it started, so a shelf of them reads in order and says whose they are.
func reviewName(session string, at time.Time) string {
	safe := make([]rune, 0, len(session))
	for _, c := range strings.ToLower(session) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			safe = append(safe, c)
		case c == '-' || c == '_':
			safe = append(safe, '-')
		}
	}
	name := strings.Trim(string(safe), "-")
	if name == "" {
		name = "reading"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return name + "-" + at.Format("20060102-150405")
}

// apiReview answers with one reading. A reading nobody has started comes back
// empty rather than missing: the screen opens it the same way either way.
func (s *Server) apiReview(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewID(w, r)
	if !ok {
		return
	}
	if s.db == nil {
		http.Error(w, "readings are not kept: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	review, err := s.db.ReviewOf(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, review)
}

// apiReviewDraft writes down where a reading has got to.
func (s *Server) apiReviewDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewID(w, r)
	if !ok {
		return
	}
	var body struct {
		Session string             `json:"session"`
		Cwd     string             `json:"cwd"`
		Base    string             `json:"base"`
		Notes   []store.ReviewNote `json:"notes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "the reading did not parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	notes, err := cleanNotes(body.Notes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Session) == "" {
		http.Error(w, "a reading belongs to a conversation, and this one names none", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "the reading cannot be saved: the database is not configured", http.StatusServiceUnavailable)
		return
	}

	err = s.db.SaveReview(r.Context(), store.Review{
		ID:      id,
		Session: strings.TrimSpace(body.Session),
		Cwd:     strings.TrimSpace(body.Cwd),
		Base:    strings.TrimSpace(body.Base),
		Notes:   notes,
	})
	if errors.Is(err, store.ErrReviewSent) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	review, err := s.db.ReviewOf(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, review)
}

// apiReviewFile writes the reading to the shelf and answers with the path of
// the file. Writing is a step of its own, before the signal: the file has to be
// there when the session comes to read it, and the session is told about it the
// way it is told anything else — through the queue of its composer.
func (s *Server) apiReviewFile(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewID(w, r)
	if !ok {
		return
	}
	if s.db == nil {
		http.Error(w, "readings are not kept: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	if !s.shelf.Available() {
		http.Error(w, chat.ErrNoShelf.Error(), http.StatusServiceUnavailable)
		return
	}

	review, err := s.db.ReviewOf(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if review.Session == "" {
		http.Error(w, "there is no such reading", http.StatusNotFound)
		return
	}
	if review.SentAt != nil {
		http.Error(w, store.ErrReviewSent.Error(), http.StatusConflict)
		return
	}
	if len(review.Notes) == 0 {
		http.Error(w, "a reading with no notes in it has nothing to send", http.StatusBadRequest)
		return
	}

	put := chat.ReviewPut{
		ID: review.ID, Session: review.Session, Cwd: review.Cwd, Base: review.Base,
		At:    time.Now().UTC().Format(time.RFC3339),
		Notes: make([]chat.ReviewNote, 0, len(review.Notes)),
	}
	for _, n := range review.Notes {
		put.Notes = append(put.Notes, chat.ReviewNote{
			ID: n.ID, Path: n.Path, Line: n.Line, Quote: n.Quote, Text: n.Text,
			At: n.At.UTC().Format(time.RFC3339),
		})
	}

	path, err := s.shelf.Put(r.Context(), put)
	if err != nil {
		if errors.Is(err, chat.ErrNoShelf) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"id": review.ID, "path": path, "notes": len(review.Notes)})
}

// apiReviewSent records that a reading has gone to its session and where the
// file landed. It is a step of its own rather than part of writing the file:
// the signal travels to the session the way any message does, through the
// queue of a busy composer, and a reading is only settled once that has
// happened.
func (s *Server) apiReviewSent(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewID(w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		http.Error(w, "the reply did not parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(body.Path)
	if path == "" {
		http.Error(w, "a reading that has gone says where its file landed", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "readings are not kept: the database is not configured", http.StatusServiceUnavailable)
		return
	}

	err := s.db.MarkReviewSent(r.Context(), id, path)
	if errors.Is(err, store.ErrReviewSent) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	review, err := s.db.ReviewOf(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, review)
}

// apiReviewDrop removes a draft nobody sent.
func (s *Server) apiReviewDrop(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewID(w, r)
	if !ok {
		return
	}
	if s.db == nil {
		http.Error(w, "readings are not kept: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	err := s.db.DropReview(r.Context(), id)
	if errors.Is(err, store.ErrReviewSent) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"dropped": id})
}

// reviewID is the name of a reading as it travels in a path: letters, digits
// and dashes. A name is made here and never taken from a path on disk — this
// is the one part of the viewer that writes, and what it writes is named by
// the panel, not by whoever asks.
func reviewID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 120 {
		http.Error(w, "that is not the name of a reading", http.StatusBadRequest)
		return "", false
	}
	for _, c := range id {
		ok := c == '-' || c == '_' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			http.Error(w, "the name of a reading carries letters, digits and dashes and nothing else",
				http.StatusBadRequest)
			return "", false
		}
	}
	return id, true
}

// cleanNotes holds the notes to what a screen can draw and a file can carry.
// The quote travels as it came, an empty line included: it is how a note is
// found again once the branch moves, and trimming it would make a note on an
// empty line indistinguishable from a note that lost its place.
func cleanNotes(raw []store.ReviewNote) ([]store.ReviewNote, error) {
	out := make([]store.ReviewNote, 0, len(raw))
	if len(raw) > maxNotes {
		return nil, errors.New("a reading holds at most " + strconv.Itoa(maxNotes) + " notes")
	}
	seen := map[string]bool{}
	for _, n := range raw {
		n.ID = strings.TrimSpace(n.ID)
		n.Path = strings.TrimSpace(n.Path)
		n.Text = strings.TrimSpace(n.Text)
		n.Quote = strings.TrimRight(n.Quote, "\r\n")
		if n.ID == "" || seen[n.ID] {
			return nil, errors.New("every note carries a name of its own")
		}
		seen[n.ID] = true
		if n.Path == "" || n.Line <= 0 {
			return nil, errors.New("a note stands on a line of a file, and this one names none")
		}
		if n.Text == "" {
			return nil, errors.New("a note with nothing written in it is not a note")
		}
		if len(n.Text) > maxNoteText {
			n.Text = n.Text[:maxNoteText]
		}
		if len(n.Quote) > maxNoteQuote {
			n.Quote = n.Quote[:maxNoteQuote]
		}
		if n.At.IsZero() {
			n.At = time.Now().UTC()
		}
		out = append(out, n)
	}
	return out, nil
}
