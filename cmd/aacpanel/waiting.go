package main

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	personrequest "aacpanel/internal/request"
)

// waitingItem is one outstanding thing, with the exact feed row it opens.
// A task can reach the service before its tool result reaches the transcript;
// until then it still counts, but has no position to promise a jump to.
type waitingItem struct {
	Kind     string `json:"kind"`
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	At       string `json:"at,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
	Pos      *int64 `json:"pos,omitempty"`
	Nth      int    `json:"nth,omitempty"`
}

func waitingFor(requests []personrequest.Record, called string, cards []chat.Item, briefs []chat.BriefCard, sent map[string]bool, secrets []action.SecretFile) []waitingItem {
	out := []waitingItem{}
	anchor := func(kind, id string) *chat.Item {
		for i := range cards {
			c := &cards[i]
			if c.Role == kind && (id == "" || c.BriefID == id || c.Name == id) {
				return c
			}
		}
		return nil
	}
	add := func(item waitingItem, card *chat.Item) {
		if card != nil {
			pos := card.Pos
			item.Pos, item.Nth = &pos, card.Nth
		}
		out = append(out, item)
	}
	for _, row := range requests {
		if row.Open() {
			add(waitingItem{Kind: "request", ID: row.ID, Title: row.Title, Blocking: row.Blocking,
				At: row.Created.Format(time.RFC3339Nano)}, anchor("request", row.ID))
		}
	}
	if called != "" {
		item := waitingItem{Kind: "call", Title: "Called you", At: called}
		card := anchor("call", "")
		if card != nil && card.Text != "" {
			item.Title = card.Text
		}
		add(item, card)
	}
	for _, brief := range briefs {
		if brief.Questions > 0 && !sent[brief.ID] {
			add(waitingItem{Kind: "brief", ID: brief.ID, Title: brief.Title, At: brief.At}, anchor("brief", brief.ID))
		}
	}
	for _, card := range cards {
		if card.Role == "question" && card.BriefID != "" {
			add(waitingItem{Kind: "question", ID: card.BriefID, Title: card.Title, At: card.At}, &card)
			continue
		}
		// Tool questions and permissions have their own visible turn stops.
		if card.Role != "secret" {
			continue
		}
		asked, _ := time.Parse(time.RFC3339Nano, card.At)
		saved := false
		for _, file := range secrets {
			at, err := time.Parse(time.RFC3339Nano, file.At)
			if file.Name == card.Name && err == nil && at.After(asked) {
				saved = true
				break
			}
		}
		if !saved {
			add(waitingItem{Kind: "secret", ID: card.Name, Title: card.Title, At: card.At}, &card)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Blocking != out[j].Blocking {
			return out[i].Blocking
		}
		return out[i].At < out[j].At
	})
	return out
}

func (s *Server) sessionsWaiting(ctx context.Context, payload []byte) []byte {
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
	requests := map[string][]personrequest.Record{}
	if s.db != nil {
		got, err := s.db.Requests(ctx, ids)
		if err != nil {
			log.Printf("the waiting requests: %v", err)
		}
		for _, row := range got {
			requests[row.Conversation] = append(requests[row.Conversation], row)
		}
	}
	waiting, _ := s.waitingCards(ctx, ids)
	cards := s.questionCards(ctx, rows, waiting)
	var briefs []chat.BriefCard
	if s.chat != nil && s.chat.Available() {
		var err error
		briefs, err = s.chat.Briefs(ctx, "")
		if err != nil {
			log.Printf("the waiting briefs: %v", err)
		}
	}
	births := map[string]string{}
	for _, b := range briefs {
		births[b.ID] = b.Born()
	}
	sent := map[string]bool{}
	if s.db != nil && len(births) > 0 {
		got, err := s.db.BriefsSent(ctx, births)
		if err == nil {
			sent = got
		} else {
			log.Printf("the sent briefs: %v", err)
		}
	}
	var secrets []action.SecretFile
	hasSecret := false
	for _, list := range cards {
		for _, c := range list {
			if c.Role == "secret" {
				hasSecret = true
			}
		}
	}
	if hasSecret && s.exec != nil {
		got, err := s.exec.Secrets(ctx)
		if err == nil {
			secrets = got.Secrets
		} else {
			log.Printf("the saved secrets: %v", err)
		}
	}
	for _, row := range rows {
		id := textField(row, "sessionId")
		var mine []chat.BriefCard
		for _, b := range briefs {
			if b.SessionID == id {
				mine = append(mine, b)
			}
		}
		items := waitingFor(requests[id], textField(row, "called"), cards[id], mine, sent, secrets)
		row["waiting"], _ = json.Marshal(items)
	}
	snapshot["sessions"], _ = json.Marshal(rows)
	out, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return out
}
