package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

type parentQuestion struct {
	Key     string
	Text    string
	Answers []string
	Ask     *chat.Ask
}

func (s *Server) parentQuestionPolicy(ctx context.Context, child notify.Session) (bool, time.Duration) {
	on, wait := true, 5*time.Minute
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return false, wait
	}
	var contour, project map[string]any
	if found, ok := projectAt(list, child.CWD, s.worktrees(), s.db.ProjectRoots()); ok {
		contour, _ = launchOf(found.profile.Launch)
		project, _ = launchOf(found.project.Launch)
	} else {
		for _, profile := range list {
			if profile.ConfigDir == child.ConfigDir {
				contour, _ = launchOf(profile.Launch)
				break
			}
		}
	}
	for _, value := range schema.Effective(nil, contour, project) {
		switch value.Key {
		case "parentQuestions":
			on, _ = value.Value.(bool)
		case "parentQuestionWait":
			switch n := value.Value.(type) {
			case int:
				wait = time.Duration(n) * time.Minute
			case float64:
				wait = time.Duration(n) * time.Minute
			}
		}
	}
	return on, wait
}

// Only the push waits. The question stays in the collector and the verdict
// store, so the session lists continue to show it immediately.
func (s *Server) deferParentQuestion(ctx context.Context, child notify.Session, question parentQuestion, now time.Time) bool {
	if s.db == nil || s.exec == nil {
		return false
	}
	on, wait := s.parentQuestionPolicy(ctx, child)
	if !on {
		return false
	}
	opening, err := s.exec.Opening(ctx, child.Name, child.ID)
	if err != nil || opening == nil || !opening.Letters {
		return false
	}
	state, fresh, err := s.db.ClaimParentQuestion(ctx, child.ID, question.Key, opening.Conversation, now.Add(wait))
	if err != nil {
		log.Printf("the parent question claim: %v", err)
		return false
	}
	if fresh {
		body := parentQuestionLetter(child, question, wait)
		if err := s.sendParentQuestion(ctx, child, opening, body); err != nil {
			log.Printf("the question of %s did not reach its parent: %v", child.Name, err)
			return false
		}
		if err := s.db.ParentQuestionDelivered(ctx, child.ID, question.Key); err != nil {
			log.Printf("the parent question delivery was not kept: %v", err)
			return false
		}
		state.Delivered = true
	}
	return state.Delivered && now.Before(state.Due)
}

func parentQuestionLetter(child notify.Session, question parentQuestion, wait time.Duration) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Child %s (%s) asks a question.\n", child.Name, child.Profile)
	if question.Ask != nil {
		fmt.Fprintf(&body, "Ask id: %s\n", question.Ask.ToolUseID)
		for i, q := range question.Ask.Questions {
			fmt.Fprintf(&body, "\n%d. %s: %s\n", i+1, q.Header, q.Text)
			for _, option := range q.Options {
				fmt.Fprintf(&body, "- %q: %s\n", option.Label, option.Description)
			}
		}
		fmt.Fprintf(&body, "\nUse session_answer with name %q, this ask id, and one answer per question: "+
			"picks are exact option labels, or text for words of your own. A letter cannot release AskUserQuestion.\n", child.Name)
	} else {
		body.WriteString(question.Text)
		for _, option := range question.Answers {
			fmt.Fprintf(&body, "\n- %s", option)
		}
		fmt.Fprintf(&body, "\n\nReply to %q with send_to_session; your letter settles this prose question.\n", child.Name)
	}
	// A parent that relays the question makes the person answer it twice:
	// the child's own question already stands in the panel.
	fmt.Fprintf(&body, "Answer only what your own work already settles. If it needs the person's decision, leave it to them: "+
		"do not ask them yourself, retell the question or report it. They already see the child's question in the panel, "+
		"and it is pushed to them after %g minutes. Permissions and requests for the person's hands still go straight to them.", wait.Minutes())
	return body.String()
}

func (s *Server) sendParentQuestion(ctx context.Context, child notify.Session, opening *action.Opening, body string) error {
	id, err := s.db.LogAttempt(ctx, store.Action{Kind: string(action.SessionLetter), Target: opening.Name,
		DeviceName: "panel questions", Params: map[string]any{"child": child.ID, "conversation": opening.Conversation}})
	if err != nil {
		return err
	}
	req := action.Request{ID: requestID(id), Kind: action.SessionLetter, Target: opening.Name,
		Conversation: opening.Conversation, ParentQuestion: child.ID, Text: body, Device: "panel questions"}
	resp, err := s.exec.Do(ctx, req)
	if err == nil && !resp.OK {
		err = errors.New(resp.Error)
	}
	outcome := store.ActionOutcome{Result: store.ActionOK, Detail: resp.Detail, Duration: time.Duration(resp.DurationMs) * time.Millisecond}
	if err != nil {
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	}
	if saved := s.db.LogResult(ctx, id, outcome); saved != nil {
		log.Printf("the parent question journal result: %v", saved)
	}
	return err
}

func (s *Server) deferAskQuestions(ctx context.Context, cur notify.World, report *notify.Report) {
	if s.chat == nil || !s.chat.Available() {
		return
	}
	raised := report.Raise[:0]
	for _, event := range report.Raise {
		deferred := false
		if strings.HasPrefix(event.Key, "ask:") {
			for _, child := range cur.Sessions {
				if child.Ask == nil || !strings.HasPrefix(event.Key, "ask:"+child.ID+":") {
					continue
				}
				feed, err := s.chat.Feed(ctx, chat.Req{Session: child.ID, State: true, Limit: 1})
				questionID := child.Ask.ID
				standing := err != nil || feed.State == nil || feed.State.Ask != nil
				if err == nil && feed.State != nil && feed.State.Ask != nil &&
					feed.State.Ask.Server == "" && !feed.State.Ask.Async {
					ask := feed.State.Ask
					if questionID == "" || questionID == ask.ToolUseID {
						questionID = ask.ToolUseID
						deferred = s.deferParentQuestion(ctx, child, parentQuestion{Key: "ask:" + ask.ToolUseID, Ask: ask}, cur.Now)
					} else {
						standing = false
					}
				}
				if !deferred && questionID != "" && s.db != nil {
					allowed, err := s.db.ParentQuestionMayPush(ctx, child.ID, "ask:"+questionID, standing)
					if err == nil && !allowed {
						deferred = true
					}
				}
				break
			}
		}
		if !deferred {
			raised = append(raised, event)
		} else {
			report.Hold = append(report.Hold, event.Key)
		}
	}
	report.Raise = raised
}

// Capture the candidate ids before delivery: a later question from the same
// child must not disappear because an earlier letter finished sending late.
func (s *Server) parentReplyQuestions(ctx context.Context, req *action.Request) []string {
	if s.db == nil || s.exec == nil || req.Kind != action.SessionLetter || req.From == "" {
		return nil
	}
	child, ok := s.host.LiveSession(req.Target)
	if !ok || child.SessionID == "" {
		return nil
	}
	opening, err := s.exec.Opening(ctx, req.Target, child.SessionID)
	if err != nil || opening == nil || opening.Conversation != req.From {
		return nil
	}
	cards, err := s.waitingCards(ctx, []string{child.SessionID})
	if err != nil {
		return nil
	}
	var ids []string
	for _, card := range cards[child.SessionID] {
		if card.Role == "question" && card.BriefID != "" {
			ids = append(ids, card.BriefID)
		}
	}
	if len(ids) > 0 {
		req.Caller, req.Conversation = req.From, child.SessionID
	}
	return ids
}

func textQuestionSession(row map[string]json.RawMessage) notify.Session {
	return notify.Session{ID: textField(row, "sessionId"), Name: textField(row, "session"),
		CWD: textField(row, "cwd"), Profile: textField(row, "profile"), ConfigDir: textField(row, "configDir")}
}

// heldAtParent is the mark of a child whose question waits at its parent:
// the parent's name and when the question goes to the person.
type heldAtParent struct {
	Parent string `json:"parent"`
	Due    string `json:"due"`
}

// sessionsAtParent marks the rows whose standing question is held at their
// parent. A held question the row no longer asks marks nothing: it was
// answered or withdrawn, whatever the claim says. It runs after the waiting
// list is put on the rows, since the questions in prose are named there.
func (s *Server) sessionsAtParent(ctx context.Context, payload []byte, now time.Time) []byte {
	if s.db == nil {
		return payload
	}
	var snapshot map[string]json.RawMessage
	var rows []map[string]json.RawMessage
	if json.Unmarshal(payload, &snapshot) != nil || json.Unmarshal(snapshot["sessions"], &rows) != nil {
		return payload
	}
	ids := []string{}
	for _, row := range rows {
		if id := textField(row, "sessionId"); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return payload
	}
	held, err := s.db.HeldParentQuestions(ctx, ids, now)
	if err != nil {
		log.Printf("the questions held at parents: %v", err)
		return payload
	}
	if len(held) == 0 {
		return payload
	}
	for _, row := range rows {
		id := textField(row, "sessionId")
		asked := standingQuestions(row)
		for _, q := range held[id] {
			if !asked[q.Question] {
				continue
			}
			row["parentQuestion"], _ = json.Marshal(heldAtParent{Parent: s.parentName(row, q.Parent), Due: q.Due.UTC().Format(time.RFC3339)})
			break
		}
	}
	snapshot["sessions"], _ = json.Marshal(rows)
	out, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return out
}

// standingQuestions are the keys a parent question is claimed under for what
// the row asks now: its tool question and its questions in prose.
func standingQuestions(row map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	var ask struct {
		ID string `json:"toolUseId"`
	}
	if json.Unmarshal(row["ask"], &ask) == nil && ask.ID != "" {
		out["ask:"+ask.ID] = true
	}
	var waiting []waitingItem
	if json.Unmarshal(row["waiting"], &waiting) == nil {
		for _, item := range waiting {
			if item.Kind == "question" && item.ID != "" {
				out["text:"+item.ID] = true
			}
		}
	}
	return out
}

// parentName is the name the person knows the parent by: the live session of
// the conversation the question was mailed to, or the parent the row names.
func (s *Server) parentName(row map[string]json.RawMessage, conversation string) string {
	if s.host != nil {
		if live, ok := s.host.LiveSessionOf(conversation); ok && live.Name != "" {
			return live.Name
		}
	}
	var parent struct {
		Session string `json:"session"`
	}
	if json.Unmarshal(row["parent"], &parent) == nil {
		return parent.Session
	}
	return ""
}
