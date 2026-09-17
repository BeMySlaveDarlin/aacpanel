package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"aacpanel/internal/chat"
	"aacpanel/internal/store"
)

// A brief is named by the session that wrote it, and the name travels in a
// path. Anything outside this shape never reaches the collector.
var briefIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func briefFail(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrUnavailable) {
		http.Error(w, "briefs are unavailable: the session collector on the host does not answer. Is aacpanel-agent running?",
			http.StatusServiceUnavailable)
		return
	}
	if errors.Is(err, chat.ErrNoBriefs) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusNotFound)
}

func (s *Server) apiBriefs(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "briefs are unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	session := r.URL.Query().Get("session")
	if session != "" && !uuidRE.MatchString(session) {
		http.Error(w, "the conversation id does not look like an id", http.StatusBadRequest)
		return
	}
	cards, err := s.chat.Briefs(r.Context(), session)
	if err != nil {
		briefFail(w, err)
		return
	}
	if cards == nil {
		cards = []chat.BriefCard{}
	}
	// How far each one has got, in one query rather than one per card. With no
	// database the shelf still lists: the documents are on the host, and only
	// the progress column goes missing.
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.ID)
	}
	done, sent := map[string]int{}, map[string]bool{}
	if s.db != nil {
		if got, err := s.db.BriefProgress(r.Context(), ids); err == nil {
			done = got
		} else {
			log.Printf("the progress of the briefs was not read: %v", err)
		}
		if got, err := s.db.BriefsSent(r.Context(), ids); err == nil {
			sent = got
		}
	}
	rows := make([]map[string]any, 0, len(cards))
	for _, card := range cards {
		rows = append(rows, map[string]any{
			"id": card.ID, "sessionId": card.SessionID, "cwd": card.CWD,
			"title": card.Title, "eyebrow": card.Eyebrow, "at": card.At,
			"questions": card.Questions, "answered": done[card.ID], "sent": sent[card.ID],
		})
	}
	writeJSON(w, map[string]any{"briefs": rows})
}

// The ceilings the draft is held to. They match the ones the collector keeps on
// the document itself: a draft is what a person typed into that document, and a
// field it has no question for is not a draft but a way into the database.
//
// The count is the count of questions a document may carry, because every one
// of them may be answered. A ceiling below it refuses the whole draft from the
// answer that crosses it on, and the person goes on answering a document that
// has stopped saving.
const (
	briefMaxAnswers = 100
	briefMaxPicks   = 8
	briefMaxNote    = 800
)

func (s *Server) briefDoc(w http.ResponseWriter, r *http.Request) (*chat.Brief, bool) {
	if !s.chat.Available() {
		http.Error(w, "briefs are unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return nil, false
	}
	id := r.PathValue("id")
	if !briefIDRE.MatchString(id) {
		http.Error(w, "the name of a brief is lowercase letters, digits and dashes", http.StatusBadRequest)
		return nil, false
	}
	doc, err := s.chat.BriefOf(r.Context(), id)
	if err != nil {
		briefFail(w, err)
		return nil, false
	}
	return doc, true
}

func (s *Server) apiBrief(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.briefDoc(w, r)
	if !ok {
		return
	}
	// A brief is readable with no database behind it: the document lives on the
	// host, and only the answers do not. Refusing the whole screen because the
	// draft is unavailable would hide what is still there.
	draft := store.BriefDraft{Answers: map[string]store.BriefAnswer{}}
	if s.db != nil {
		got, err := s.db.BriefDraftOf(r.Context(), doc.ID)
		if err != nil {
			log.Printf("the draft of brief %s was not read: %v", doc.ID, err)
		} else {
			draft = got
		}
	}
	writeJSON(w, map[string]any{
		"brief": doc,
		"draft": draft,
		"reply": briefReply(doc, draft),
	})
}

func (s *Server) apiBriefDraft(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.briefDoc(w, r)
	if !ok {
		return
	}
	var body struct {
		Answers map[string]store.BriefAnswer `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&body); err != nil {
		http.Error(w, "the draft did not parse: "+err.Error(), http.StatusBadRequest)
		return
	}
	answers, err := cleanBriefAnswers(body.Answers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "the draft cannot be saved: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	// A brief whose answers have gone into a session is settled. Saving over it
	// would leave the panel showing one set of answers while the session holds
	// another, and the screen that locks the fields is not where that is
	// decided: a request does not have to come from that screen.
	if was, err := s.db.BriefDraftOf(r.Context(), doc.ID); err == nil && was.SentAt != nil {
		http.Error(w, "the answers of this brief have already gone into the session: it is read from here on",
			http.StatusConflict)
		return
	}
	if err := s.db.SaveBriefDraft(r.Context(), doc.ID, answers); err != nil {
		http.Error(w, "the draft was not saved: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	draft, err := s.db.BriefDraftOf(r.Context(), doc.ID)
	if err != nil {
		draft = store.BriefDraft{Answers: answers}
	}
	writeJSON(w, map[string]any{"draft": draft, "reply": briefReply(doc, draft)})
}

func (s *Server) apiBriefSent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !briefIDRE.MatchString(id) {
		http.Error(w, "the name of a brief is lowercase letters, digits and dashes", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "the mark cannot be saved: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	if was, err := s.db.BriefDraftOf(r.Context(), id); err == nil && was.SentAt != nil {
		http.Error(w, "this brief was already sent", http.StatusConflict)
		return
	}
	if err := s.db.MarkBriefSent(r.Context(), id); err != nil {
		http.Error(w, "the mark was not saved: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func cleanBriefAnswers(raw map[string]store.BriefAnswer) (map[string]store.BriefAnswer, error) {
	if len(raw) > briefMaxAnswers {
		return nil, fmt.Errorf("a draft holds at most %d answers", briefMaxAnswers)
	}
	out := make(map[string]store.BriefAnswer, len(raw))
	for id, answer := range raw {
		if !briefIDRE.MatchString(id) {
			return nil, fmt.Errorf("%q does not name a question", id)
		}
		if len(answer.Picks) > briefMaxPicks {
			return nil, fmt.Errorf("question %s carries more picks than it has options", id)
		}
		for _, pick := range answer.Picks {
			if len([]rune(pick)) > 4 {
				return nil, fmt.Errorf("question %s: %q does not name an option", id, pick)
			}
		}
		if len([]rune(answer.Note)) > briefMaxNote {
			return nil, fmt.Errorf("the note under question %s is longer than %d characters", id, briefMaxNote)
		}
		out[id] = answer
	}
	return out, nil
}

// briefReply builds the text that goes into the session.
//
// It is built here and not on the screen so that what the person reads before
// sending and what the session receives are one text. The order is the order of
// the questions: an answer that arrives sorted by anything else costs the
// reader the document they wrote.
func briefReply(doc *chat.Brief, draft store.BriefDraft) string {
	if doc == nil {
		return ""
	}
	done := 0
	for _, q := range doc.Questions {
		if briefAnswered(draft.Answers[q.ID]) {
			done++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Brief %q · %s\n", doc.Title, doc.ID)
	fmt.Fprintf(&b, "Answered %d of %d\n", done, len(doc.Questions))

	for _, q := range doc.Questions {
		answer := draft.Answers[q.ID]
		if q.Kind == "none" && !briefAnswered(answer) {
			continue
		}
		fmt.Fprintf(&b, "\n%s %s\n", q.N, q.Title)
		switch {
		case answer.Skip:
			b.WriteString("   skipped\n")
		case len(answer.Picks) > 0:
			for _, pick := range answer.Picks {
				fmt.Fprintf(&b, "   %s · %s\n", pick, briefOptionLabel(q, pick))
			}
		case answer.Note == "":
			b.WriteString("   no answer\n")
		}
		if note := strings.TrimSpace(answer.Note); note != "" {
			fmt.Fprintf(&b, "   note: %s\n", note)
		}
	}
	return b.String()
}

func briefAnswered(a store.BriefAnswer) bool {
	return a.Skip || len(a.Picks) > 0 || strings.TrimSpace(a.Note) != ""
}

func briefOptionLabel(q chat.BriefQuestion, key string) string {
	for _, opt := range q.Options {
		if opt.Key == key {
			return opt.Label
		}
	}
	// A pick with no option behind it means the document was reissued without
	// that option. Saying the bare key beats dropping the line: the session
	// wrote the document and can look up what the key meant.
	return "(an option the document no longer offers)"
}

// apiBriefDrop takes a brief off the shelf of the host and the answers with it.
//
// The person reading the panel removes what they are looking at; a session asks
// through the publisher and names the directory it works in, which is where the
// rule about its own documents is kept.
func (s *Server) apiBriefDrop(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "briefs are unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if !briefIDRE.MatchString(id) {
		http.Error(w, "the name of a brief is lowercase letters, digits and dashes", http.StatusBadRequest)
		return
	}
	if err := s.chat.DropBriefOf(r.Context(), id, ""); err != nil {
		briefFail(w, err)
		return
	}
	// The document is gone; the answers to it go too, and a database that is
	// down does not make the removal a failure — the brief is off the shelf.
	if s.db != nil {
		if err := s.db.DropBriefDraft(r.Context(), id); err != nil {
			log.Printf("the brief %s is removed, its draft is not: %v", id, err)
		}
	}
	writeJSON(w, map[string]any{"ok": true, "dropped": id})
}
