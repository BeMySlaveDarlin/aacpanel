package notify

import (
	"context"
	"log"
	"time"
)

const viewFresh = 40 * time.Second

// Views reports who is looking at a session right now.
type Views interface {
	Watching(ctx context.Context, session string, fresh time.Duration) ([]int64, error)
}

// UseViews plugs in the view marks.
func (s *Sender) UseViews(v Views) { s.views = v }

func (s *Sender) watching(ctx context.Context, m Message) map[int64]bool {
	if m.Session == "" || s.views == nil {
		return nil
	}
	askCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ids, err := s.views.Watching(askCtx, m.Session, viewFresh)
	if err != nil {
		log.Printf("notify: who is looking at %q is unknown (%v), sending to everyone", m.Session, err)
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
