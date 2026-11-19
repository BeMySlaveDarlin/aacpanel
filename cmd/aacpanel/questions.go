package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	"aacpanel/internal/question"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

func questionSession(row map[string]json.RawMessage) bool {
	return textField(row, "agent") != schema.AgentCodex && textField(row, "status") != "busy"
}

func questionVisible(classify bool, verdict string) bool {
	return !classify || verdict == "yes" || verdict == "error"
}

func (s *Server) waitingCards(ctx context.Context, ids []string) (map[string][]chat.Item, error) {
	cards := map[string][]chat.Item{}
	if s.chat == nil || !s.chat.Available() {
		return nil, errors.New("the waiting cards are unavailable")
	}
	for start := 0; start < len(ids); start += 32 {
		got, err := s.chat.Feed(ctx, chat.Req{Waiting: ids[start:min(start+32, len(ids))]})
		if err != nil {
			log.Printf("the waiting cards: %v", err)
			return nil, err
		}
		if got.Waiting == nil {
			return nil, errors.New("the collector returned no waiting snapshot")
		}
		if s.db != nil {
			for id, messages := range got.AnsweredQuestions {
				if len(messages) > 0 {
					if err := s.db.AnswerTextQuestions(ctx, id, messages, "person"); err != nil {
						return nil, err
					}
				}
			}
		}
		for id, list := range got.Waiting {
			cards[id] = list
		}
	}
	return cards, nil
}

// questionCards filters only candidates. Classification never runs on a
// snapshot request; an unavailable verdict store leaves candidates visible.
func (s *Server) questionCards(ctx context.Context, rows []map[string]json.RawMessage, cards map[string][]chat.Item) map[string][]chat.Item {
	ids := []string{}
	for _, row := range rows {
		ids = append(ids, textField(row, "sessionId"))
	}
	classify := s.pushPrefs(ctx).ClassifyQuestions()
	verdicts := map[string]map[string]store.QuestionVerdict{}
	if s.db == nil {
		classify = false
	} else {
		got, err := s.db.QuestionVerdicts(ctx, ids)
		if err != nil {
			log.Printf("the question verdicts: %v", err)
			classify = false
		} else {
			verdicts = got
		}
	}
	out := map[string][]chat.Item{}
	for _, row := range rows {
		id := textField(row, "sessionId")
		for _, card := range cards[id] {
			if card.Role == "question" && verdicts[id][card.BriefID].Answered {
				continue
			}
			if card.Role == "question" && (!questionSession(row) || !questionVisible(classify, verdicts[id][card.BriefID].Verdict)) {
				continue
			}
			if card.Role == "question" && verdicts[id][card.BriefID].What != "" {
				card.Title = verdicts[id][card.BriefID].What
			}
			out[id] = append(out[id], card)
		}
	}
	return out
}

// runTextQuestions bounds both the number of model processes and their life.
// Claims outlive the service; a restart never retries a message that spent a
// call, and a claim left unfinished becomes a visible error in the store.
func (s *Server) runTextQuestions(ctx context.Context) {
	slots := make(chan struct{}, 2)
	var jobs sync.WaitGroup
	defer jobs.Wait()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		s.scanTextQuestions(ctx, func(conversation, session string, card chat.Item) {
			select {
			case slots <- struct{}{}:
			default:
				return
			}
			jobs.Add(1)
			go func() {
				defer jobs.Done()
				defer func() { <-slots }()
				s.classifyTextQuestion(ctx, conversation, session, card)
			}()
		})
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) scanTextQuestions(ctx context.Context, schedule func(string, string, chat.Item)) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	payload, err := s.host.JSON()
	if err != nil {
		return
	}
	var snap struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if json.Unmarshal(payload, &snap) != nil || snap.Sessions == nil {
		return
	}
	ids := []string{}
	for _, row := range snap.Sessions {
		if id := textField(row, "sessionId"); id != "" {
			ids = append(ids, id)
		}
	}
	cards := map[string][]chat.Item{}
	if len(ids) > 0 {
		cards, err = s.waitingCards(ctx, ids)
		if err != nil {
			return
		}
	}
	active := []string{}
	for id, list := range cards {
		for _, card := range list {
			if card.Role == "question" {
				active = append(active, id+":"+card.BriefID)
			}
		}
	}
	if err := s.db.CloseQuestions(ctx, active); err != nil {
		log.Printf("the question closures: %v", err)
		return
	}
	verdicts, err := s.db.QuestionVerdicts(ctx, ids)
	if err != nil {
		log.Printf("the question verdicts: %v", err)
		return
	}
	for id, messages := range verdicts {
		for message, v := range messages {
			if !v.Hooked || v.Answered || v.Closed || len(v.Questions) == 0 {
				continue
			}
			found := false
			for _, card := range cards[id] {
				if card.Role == "question" && card.BriefID == message {
					found = true
					break
				}
			}
			if !found {
				cards[id] = append(cards[id], chat.Item{Role: "question", BriefID: message, Text: v.What, Title: v.What})
			}
		}
	}
	prefs := s.pushPrefs(ctx)
	for _, row := range snap.Sessions {
		if !questionSession(row) {
			continue
		}
		id, name := textField(row, "sessionId"), textField(row, "session")
		for _, card := range cards[id] {
			if card.Role != "question" || card.BriefID == "" || (!question.Candidate(card.Text) && !verdicts[id][card.BriefID].Hooked) {
				continue
			}
			verdict := verdicts[id][card.BriefID]
			if verdict.Answered {
				continue
			}
			if prefs.ClassifyQuestions() && verdict.Verdict == "" {
				schedule(id, name, card)
				continue
			}
			if !questionVisible(prefs.ClassifyQuestions(), verdict.Verdict) || s.push == nil {
				continue
			}
			if verdict.Verdict == "yes" {
				question := verdict.What
				if question == "" {
					question = card.Text
				}
				if s.deferParentQuestion(ctx, textQuestionSession(row),
					parentQuestion{Key: "text:" + card.BriefID, Text: question, Questions: verdict.Questions}, time.Now()) {
					continue
				}
			}
			what := verdict.What
			if what == "" {
				what = card.Title
			}
			message := notify.TextQuestion(notify.Session{ID: id, Name: name}, card.BriefID, what)
			if !prefs.Allows(message) {
				continue
			}
			fresh, err := s.db.PushQuestion(ctx, id, card.BriefID)
			if err != nil {
				log.Printf("the question push claim: %v", err)
				continue
			}
			if fresh {
				s.push.Send(message)
			}
		}
	}
}

func (s *Server) classifyTextQuestion(ctx context.Context, conversation, session string, card chat.Item) {
	ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	// A queued job re-reads the live switch before spending the account.
	if !s.pushPrefs(ctx).ClassifyQuestions() {
		return
	}
	claimed, err := s.db.ClaimQuestion(ctx, conversation, card.BriefID)
	if err != nil {
		log.Printf("the question claim: %v", err)
		return
	}
	if !claimed {
		return
	}
	verdict := "error"
	form := question.Verdict{Questions: []question.Question{}}
	// A shortened message may have lost the very question we found. It
	// cannot earn a negative verdict from the classifier.
	if s.exec != nil && !card.Cut {
		got, err := s.questionVerdict(ctx, session, conversation, card.Text)
		if err == nil {
			verdict = "no"
			form = got
			if len(got.Questions) > 0 {
				verdict = "yes"
			}
		}
	}
	// A timeout is still a verdict. Give its write its own short deadline so
	// cancellation cannot leave an uncertain question hidden until recovery.
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	if err := s.db.SetQuestionVerdict(save, conversation, session, card.BriefID, verdict, form); err != nil {
		log.Printf("the question verdict was not kept: %v", err)
	}
}

func (s *Server) questionVerdict(ctx context.Context, session, conversation, text string) (question.Verdict, error) {
	alreadyAsked, err := s.db.OpenAskQuestions(ctx, conversation)
	if err != nil {
		return question.Verdict{}, err
	}
	return s.exec.Question(ctx, session, conversation, text, alreadyAsked)
}
