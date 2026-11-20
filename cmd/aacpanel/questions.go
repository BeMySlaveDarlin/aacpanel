package main

import (
	"context"
	"errors"
	"log"

	"aacpanel/internal/chat"
)

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
		for id, list := range got.Waiting {
			cards[id] = list
		}
	}
	return cards, nil
}
