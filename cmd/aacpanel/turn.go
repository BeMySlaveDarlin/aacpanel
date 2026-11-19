package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/notify"
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
	answer := func(action string, block bool) { writeJSON(w, map[string]any{"action": action, "block": block}) }
	if s.db == nil || s.host == nil {
		answer("", false)
		return
	}
	live, ok := s.host.LiveSessionOf(body.Conversation)
	if !ok || live.Outside || live.Name != body.Session || live.Agent == schema.AgentCodex {
		answer("", false)
		return
	}
	if !question.Candidate(body.Text) || !s.pushPrefs(r.Context()).ClassifyQuestions() {
		answer("", false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Second)
	defer cancel()
	claimed, err := s.db.ClaimTurnQuestion(ctx, body.Conversation, body.Message)
	if err != nil {
		answer("", false)
		return
	}
	if claimed {
		verdict := "error"
		form := question.Verdict{Questions: []question.Question{}}
		if s.exec != nil {
			if got, err := s.exec.Question(ctx, live.Name, live.SessionID, body.Text); err == nil {
				form, verdict = got, "no"
				if len(got.Questions) > 0 {
					verdict = "yes"
				}
			}
		}
		save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		if err := s.db.SetQuestionVerdict(save, body.Conversation, live.Name, body.Message, verdict, form); err != nil {
			log.Printf("the turn verdict was not kept: %v", err)
			answer("", false)
			return
		}
	}
	var v store.QuestionVerdict
	// The fallback may own the claim already. Its pending call must finish
	// before this Stop decides whether the manual action needs an act.
	for {
		verdicts, err := s.db.QuestionVerdicts(ctx, []string{body.Conversation})
		if err != nil {
			answer("", false)
			return
		}
		v = verdicts[body.Conversation][body.Message]
		if v.Verdict != "pending" {
			break
		}
		select {
		case <-ctx.Done():
			answer("", false)
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	if v.Verdict != "yes" && v.Verdict != "no" {
		answer("", false)
		return
	}
	if len(v.Questions) > 0 && !v.Answered && !v.Closed && s.push != nil {
		child := notify.Session{ID: live.SessionID, Name: live.Name, CWD: live.CWD}
		if !s.deferParentQuestion(ctx, child, parentQuestion{Key: "text:" + body.Message, Text: v.What, Questions: v.Questions}, time.Now()) {
			message := notify.TextQuestion(child, body.Message, v.What)
			if s.pushPrefs(ctx).Allows(message) {
				if fresh, err := s.db.PushQuestion(ctx, body.Conversation, body.Message); err == nil && fresh {
					s.push.Send(message)
				}
			}
		}
	}
	block := false
	if v.Action != "" {
		acts, err := s.db.OpenActs(ctx, []string{body.Conversation})
		if err != nil {
			answer(v.Action, false)
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
	answer(v.Action, block)
}
