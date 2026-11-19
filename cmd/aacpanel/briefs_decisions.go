package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func emptyBriefDraft(doc *chat.Brief) store.BriefDraft {
	draft := store.BriefDraft{Answers: map[string]store.BriefAnswer{}}
	if doc.DecisionForm() {
		draft.DecisionAnswers = &store.BriefDecisions{Decisions: map[string]store.BriefAnswer{}}
	}
	return draft
}

func cleanBriefDecisions(doc *chat.Brief, raw store.BriefDecisions) (store.BriefDecisions, error) {
	if len(raw.Decisions) > briefMaxAnswers {
		return raw, fmt.Errorf("a draft holds at most %d decisions", briefMaxAnswers)
	}
	if len([]rune(raw.Note)) > 4000 {
		return raw, errors.New("the task note is longer than 4000 characters")
	}
	known := make(map[string]chat.BriefDecision, len(doc.Decisions))
	for i, d := range doc.Decisions {
		if d.ID == "" {
			d.ID = strconv.Itoa(i + 1)
		}
		known[d.ID] = d
	}
	out := store.BriefDecisions{Decisions: map[string]store.BriefAnswer{}, Note: strings.TrimSpace(raw.Note)}
	for id, a := range raw.Decisions {
		d, ok := known[id]
		if !ok {
			return raw, fmt.Errorf("%q does not name a decision", id)
		}
		if a.Skip {
			return raw, fmt.Errorf("decision %s cannot be skipped; leave it unanswered", id)
		}
		if len([]rune(a.Note)) > 2000 {
			return raw, fmt.Errorf("the note under decision %s is longer than 2000 characters", id)
		}
		if len(a.Picks) > len(d.Options) || (!d.Multi && len(a.Picks) > 1) {
			return raw, fmt.Errorf("decision %s carries more picks than it allows", id)
		}
		keys := map[string]bool{}
		for _, o := range d.Options {
			keys[o.Key] = true
		}
		seen := map[string]bool{}
		for _, pick := range a.Picks {
			if !keys[pick] || seen[pick] {
				return raw, fmt.Errorf("decision %s: %q does not name a distinct option", id, pick)
			}
			seen[pick] = true
		}
		a.Note = strings.TrimSpace(a.Note)
		out.Decisions[id] = a
	}
	return out, nil
}

func briefDecisionReply(doc *chat.Brief, draft store.BriefDraft) string {
	answers := store.BriefDecisions{}
	if draft.DecisionAnswers != nil {
		answers = *draft.DecisionAnswers
	}
	done := 0
	for i, d := range doc.Decisions {
		id := d.ID
		if id == "" {
			id = strconv.Itoa(i + 1)
		}
		if len(answers.Decisions[id].Picks) > 0 {
			done++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The person decided brief %q (%s): %d of %d decided.\n", doc.Title, doc.ID, done, len(doc.Decisions))
	for i, d := range doc.Decisions {
		id := d.ID
		if id == "" {
			id = strconv.Itoa(i + 1)
		}
		a := answers.Decisions[id]
		fmt.Fprintf(&b, "%d. %s\n", i+1, d.Title)
		if len(a.Picks) == 0 {
			b.WriteString("   no answer\n")
		} else {
			picks := map[string]bool{}
			for _, key := range a.Picks {
				picks[key] = true
			}
			var labels, recommended []string
			same := true
			for _, o := range d.Options {
				if picks[o.Key] {
					labels = append(labels, o.Label)
				}
				if o.Recommended {
					recommended = append(recommended, o.Label)
				}
				if picks[o.Key] != o.Recommended {
					same = false
				}
			}
			verdict := "recommended"
			if !same {
				verdict = "changed; recommended: " + strings.Join(recommended, ", ")
			}
			fmt.Fprintf(&b, "   picked: %s (%s)\n", strings.Join(labels, ", "), verdict)
		}
		if note := strings.TrimSpace(a.Note); note != "" {
			fmt.Fprintf(&b, "   note: %s\n", note)
		}
	}
	if note := strings.TrimSpace(answers.Note); note != "" {
		fmt.Fprintf(&b, "Note: %s\n", note)
	}
	return b.String()
}

func briefMessageID(doc *chat.Brief) string {
	sum := sha256.Sum256([]byte("brief:" + doc.ID + ":" + doc.Born()))
	return act.MessageID(hex.EncodeToString(sum[:16]))
}

func (s *Server) briefTarget(doc *chat.Brief) string {
	if s.host == nil {
		return ""
	}
	raw, err := s.host.Raw()
	if err != nil {
		return ""
	}
	var snapshot struct {
		Sessions []host.LiveSession `json:"sessions"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return ""
	}
	for _, live := range snapshot.Sessions {
		if !live.Outside && live.Name != "" && live.SessionID == doc.SessionID {
			return live.Name
		}
	}
	for _, live := range snapshot.Sessions {
		if !live.Outside && live.Name != "" && doc.CWD != "" && live.CWD == doc.CWD {
			return live.Name
		}
	}
	return ""
}

func (s *Server) apiBriefSend(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.briefDoc(w, r)
	if !ok {
		return
	}
	if !doc.DecisionForm() {
		http.Error(w, "this brief uses the question form", http.StatusBadRequest)
		return
	}
	if s.db == nil {
		http.Error(w, "the draft cannot be sent: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	// A closing tab must not release the row while the executor accepts mail.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout+5*time.Second)
	defer cancel()
	lease, err := s.db.LockBrief(ctx, doc.ID, doc.Born())
	if err != nil {
		code := http.StatusServiceUnavailable
		if errors.Is(err, store.ErrBriefSent) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer lease.Close(ctx)
	answers := store.BriefDecisions{}
	if lease.Draft.DecisionAnswers != nil {
		answers = *lease.Draft.DecisionAnswers
	}
	answers, err = cleanBriefDecisions(doc, answers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lease.Draft.DecisionAnswers = &answers
	if lease.Draft.Decided() == 0 {
		http.Error(w, "decide at least one decision before sending", http.StatusBadRequest)
		return
	}
	target := s.briefTarget(doc)
	if target == "" {
		http.Error(w, "the author is not running and no live session works in this brief's directory", http.StatusConflict)
		return
	}
	text := briefReply(doc, lease.Draft)
	raw, err := json.Marshal(map[string]any{"kind": action.SessionSend, "target": target,
		"params": map[string]any{"text": text, "messageId": briefMessageID(doc)}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	inner := r.Clone(ctx)
	inner.Body = io.NopCloser(bytes.NewReader(raw))
	delivery := &actionReply{header: make(http.Header)}
	s.runAction(delivery, inner, false)
	if delivery.status != http.StatusOK {
		copyClaimReply(w, delivery)
		return
	}
	if err := lease.Finish(ctx); err != nil {
		http.Error(w, "the decisions were delivered but their mark was not saved: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "draft": lease.Draft, "reply": text})
}
