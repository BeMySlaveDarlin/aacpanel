package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/question"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

func (s *Server) apiTurn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Conversation string `json:"conversation"`
		Session      string `json:"session"`
		Message      string `json:"message"`
		Text         string `json:"text"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || !uuidRE.MatchString(body.Conversation) || !uuidRE.MatchString(body.Message) || strings.TrimSpace(body.Text) == "" {
		http.Error(w, "a turn needs its conversation, message id and text", http.StatusBadRequest)
		return
	}
	answer := func(what, action string, block bool) {
		writeJSON(w, map[string]any{"question": what, "action": action, "block": block})
	}
	if s.db == nil || s.host == nil {
		answer("", "", false)
		return
	}
	live, ok := s.host.LiveSessionOf(body.Conversation)
	if !ok || live.Outside || live.Name != body.Session || live.Agent == schema.AgentCodex {
		answer("", "", false)
		return
	}
	if !question.Candidate(body.Text) || !s.pushPrefs(r.Context()).ClassifyQuestions() {
		answer("", "", false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Second)
	defer cancel()
	claimed, err := s.db.ClaimTurnQuestion(ctx, body.Conversation, body.Message)
	if err != nil {
		answer("", "", false)
		return
	}
	if claimed {
		verdict := "error"
		form := question.Verdict{Questions: []question.Question{}}
		if s.exec != nil {
			if got, err := s.exec.Question(ctx, live.Name, live.SessionID, body.Text, nil); err == nil {
				form, verdict = got, "no"
				if len(got.Questions) > 0 {
					verdict = "yes"
				}
			}
		}
		save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		if err := s.db.SetQuestionVerdict(save, body.Conversation, body.Message, verdict, form); err != nil {
			log.Printf("the turn verdict was not kept: %v", err)
			answer("", "", false)
			return
		}
	}
	var v store.QuestionVerdict
	// Concurrent Stops share the same verdict and must wait for its owner.
	for {
		verdicts, err := s.db.QuestionVerdicts(ctx, []string{body.Conversation})
		if err != nil {
			answer("", "", false)
			return
		}
		v = verdicts[body.Conversation][body.Message]
		if v.Verdict != "pending" {
			break
		}
		select {
		case <-ctx.Done():
			answer("", "", false)
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	if v.Verdict != "yes" && v.Verdict != "no" {
		answer("", "", false)
		return
	}
	if len(v.Questions) > 0 {
		// The hook names the question it refuses; a verdict without a summary
		// names it by its first question rather than letting the turn end.
		what := v.What
		if strings.TrimSpace(what) == "" {
			what = v.Questions[0].Question
		}
		answer(what, v.Action, true)
		return
	}
	block := false
	if v.Action != "" {
		acts, err := s.db.OpenActs(ctx, []string{body.Conversation})
		if err != nil {
			answer("", v.Action, false)
			return
		}
		block = true
		for _, row := range acts {
			if row.Kind == "do" {
				block = false
				break
			}
		}
	}
	answer("", v.Action, block)
}
