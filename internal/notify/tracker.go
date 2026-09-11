package notify

import (
	"context"
	"log"
)

const bootKey = "boot"

// Journal is where the memory of announced events lives.
type Journal interface {
	Raised(ctx context.Context) ([]Event, error)
	Raise(ctx context.Context, e Event) error
	Drop(ctx context.Context, key string) error
}

// Tracker holds the set of announced events and turns a view of the host into notifications.
type Tracker struct {
	journal Journal
	active  map[string]Event
	loaded  bool
	spoke   bool
}

func NewTracker(j Journal) *Tracker {
	return &Tracker{journal: j, active: map[string]Event{}}
}

// Step matches a host report against what was already announced and returns what to send.
func (t *Tracker) Step(ctx context.Context, r Report) []Message {
	t.restore(ctx)
	if t.seed(ctx, r) {
		return nil
	}

	hold := set(r.Hold)
	drop := set(r.Drop)
	seen := set(r.Seen)

	var out []Message
	for key, e := range t.active {
		switch {
		case hold[key]:
			continue
		case drop[key]:
		case !seen[e.Domain]:
			continue
		case e.GoneTitle != "":
			out = append(out, e.gone())
		}
		delete(t.active, key)
		t.forget(ctx, key)
	}

	for _, e := range r.Raise {
		if _, told := t.active[e.Key]; told {
			continue
		}
		t.active[e.Key] = e
		t.remember(ctx, e)
		out = append(out, e.raised())
	}

	if len(out) > 0 {
		t.spoke = true
	}
	return out
}

func (t *Tracker) seed(ctx context.Context, r Report) bool {
	if !t.loaded || len(t.active) > 0 {
		return false
	}
	t.active[bootKey] = Event{Key: bootKey, Domain: "boot",
		Title: "notifications work", Body: "the mark of the first start"}
	t.remember(ctx, t.active[bootKey])

	for _, e := range r.Raise {
		e.GoneTitle, e.GoneBody, e.GoneSeverity = "", "", ""
		t.active[e.Key] = e
		t.remember(ctx, e)
	}
	log.Printf("notify: the first start, reasons taken in silently: %d", len(r.Raise))
	return true
}

// Active returns how many events are currently on the books.
func (t *Tracker) Active() int {
	if _, boot := t.active[bootKey]; boot {
		return len(t.active) - 1
	}
	return len(t.active)
}

func (t *Tracker) restore(ctx context.Context) {
	if t.loaded || t.journal == nil {
		t.loaded = true
		return
	}
	if t.spoke {
		log.Print("notify: the past arrived after we had already spoken — going on from memory")
		t.loaded = true
		return
	}
	list, err := t.journal.Raised(ctx)
	if err != nil {
		return
	}
	for _, e := range list {
		if _, have := t.active[e.Key]; !have {
			t.active[e.Key] = e
		}
	}
	t.loaded = true
}

func (t *Tracker) remember(ctx context.Context, e Event) {
	if t.journal == nil {
		return
	}
	if err := t.journal.Raise(ctx, e); err != nil {
		log.Printf("notify: reason %q was not written down (%v) — after a restart it will be announced again", e.Key, err)
	}
}

func (t *Tracker) forget(ctx context.Context, key string) {
	if t.journal == nil {
		return
	}
	if err := t.journal.Drop(ctx, key); err != nil {
		log.Printf("notify: reason %q was not forgotten (%v) — after a restart it will pass in silence", key, err)
	}
}

func set(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}
