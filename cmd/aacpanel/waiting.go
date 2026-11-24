package main

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/chat"
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
	// Agent marks a chain standing on the agent's turn: open, but not the
	// person's move.
	Agent   bool   `json:"agent,omitempty"`
	Running bool   `json:"running,omitempty"`
	Pos     *int64 `json:"pos,omitempty"`
	Nth     int    `json:"nth,omitempty"`
}

func waitingFor(acts []act.Record, cards []chat.Item, briefs []chat.BriefCard, sent map[string]bool) []waitingItem {
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
	for _, row := range acts {
		if !row.Open() || row.Kind == "claim" || row.Kind == "ask" {
			continue
		}
		kind := map[string]string{"do": "request", "secret": "secret", "call": "call"}[row.Kind]
		item := waitingItem{Kind: kind, ID: row.ID, Title: row.Title, Blocking: row.Blocking, Agent: row.AgentsMove(), Running: row.CommandRunning(), At: row.Created.Format(time.RFC3339Nano)}
		card := anchor(kind, row.ID)
		if row.Kind == "call" {
			card = nil
			for i := range cards {
				candidate := &cards[i]
				at, err := time.Parse(time.RFC3339Nano, candidate.At)
				if candidate.Role == "call" && err == nil && at.Equal(row.Created) {
					card = candidate
					break
				}
			}
			if card != nil && card.Text != "" {
				item.Title = card.Text
			}
		}
		add(item, card)
	}
	for _, brief := range briefs {
		if brief.Count() > 0 && !sent[brief.ID] {
			add(waitingItem{Kind: "brief", ID: brief.ID, Title: brief.Title, At: brief.At}, anchor("brief", brief.ID))
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
	names := []string{}
	for _, row := range rows {
		if name := textField(row, "session"); name != "" {
			names = append(names, name)
		}
		if id := textField(row, "sessionId"); id != "" {
			ids = append(ids, id)
		}
	}
	waiting, _ := s.waitingCards(ctx, ids)
	acts := map[string][]act.Record{}
	var claims []act.Record
	if s.db != nil {
		got, err := s.db.OpenActs(ctx, ids)
		if err != nil {
			log.Printf("the waiting acts: %v", err)
		}
		for _, row := range got {
			acts[row.Conversation] = append(acts[row.Conversation], row)
		}
		claims, err = s.db.OpenClaims(ctx, names)
		if err != nil {
			log.Printf("the session claims: %v", err)
		}
	}
	cards := waiting
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
	for _, row := range rows {
		id := textField(row, "sessionId")
		var mine []chat.BriefCard
		for _, b := range briefs {
			if b.SessionID == id {
				mine = append(mine, b)
			}
		}
		items := waitingFor(acts[id], cards[id], mine, sent)
		row["waiting"], _ = json.Marshal(items)
		row["claims"], _ = json.Marshal(claimsFor(claims, textField(row, "session")))
	}
	snapshot["sessions"], _ = json.Marshal(rows)
	out, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return out
}
