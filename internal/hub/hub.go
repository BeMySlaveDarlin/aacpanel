package hub

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"aacpanel/internal/docker"
	"aacpanel/internal/store"
)

const (
	statsInterval    = 10 * time.Second
	statsConcurrency = 8
	statsTimeout     = 20 * time.Second
	sizesEvery       = 5 * time.Minute
)

// Hub holds one stream of docker events for all subscribers and sends them the ready tree JSON.
type Hub struct {
	docker *docker.Client
	writer *store.Writer

	mu      sync.Mutex
	subs    map[chan string]struct{}
	latest  string
	metrics docker.Metrics

	wake    chan struct{}
	sizesAt time.Time
}

func New(d *docker.Client, w *store.Writer) *Hub {
	return &Hub{
		docker:  d,
		writer:  w,
		subs:    map[chan string]struct{}{},
		metrics: docker.Metrics{Stats: map[string]docker.Stats{}, Disk: map[string]docker.Disk{}},
		wake:    make(chan struct{}, 1),
	}
}

func (h *Hub) Subscribe() chan string {
	ch := make(chan string, 4)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	latest := h.latest
	h.mu.Unlock()

	if latest != "" {
		ch <- latest
	}
	h.poke()
	return ch
}

func (h *Hub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

func (h *Hub) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *Hub) broadcast(payload string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = payload
	for ch := range h.subs {
		select {
		case ch <- payload:
		default:
		}
	}
}

// SnapshotMetrics returns a copy of the latest load snapshot.
func (h *Hub) SnapshotMetrics() *docker.Metrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := docker.Metrics{
		Stats: make(map[string]docker.Stats, len(h.metrics.Stats)),
		Disk:  make(map[string]docker.Disk, len(h.metrics.Disk)),
	}
	for k, v := range h.metrics.Stats {
		out.Stats[k] = v
	}
	for k, v := range h.metrics.Disk {
		out.Disk[k] = v
	}
	return &out
}

func (h *Hub) hasSubscribers() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs) > 0
}

func (h *Hub) refresh(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tree, err := h.docker.Tree(reqCtx, h.SnapshotMetrics())
	if err != nil {
		log.Printf("hub: refreshing the tree: %v", err)
		return
	}
	payload, err := json.Marshal(tree)
	if err != nil {
		log.Printf("hub: serializing the tree: %v", err)
		return
	}
	h.broadcast(string(payload))
}

// Run keeps the subscription to docker events and rebuilds the tree on changes.
func (h *Hub) Run(ctx context.Context) {
	go h.watchEvents(ctx)
	go h.collectStats(ctx)

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.wake:
			if debounce == nil {
				debounce = time.After(300 * time.Millisecond)
			}
		case <-debounce:
			debounce = nil
			h.refresh(ctx)
		case <-tick.C:
			h.refresh(ctx)
		}
	}
}

func (h *Hub) watchEvents(ctx context.Context) {
	for ctx.Err() == nil {
		err := h.docker.Events(ctx, h.poke)
		if ctx.Err() != nil {
			return
		}
		log.Printf("hub: the event stream broke (%v), reconnecting", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (h *Hub) collectStats(ctx context.Context) {
	tick := time.NewTicker(statsInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if !h.hasSubscribers() && h.writer == nil {
			continue
		}
		if h.collectOnce(ctx) {
			h.refresh(ctx)
		}
	}
}

func (h *Hub) collectOnce(ctx context.Context) bool {
	listCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	tree, err := h.docker.Tree(listCtx, nil)
	cancel()
	if err != nil {
		log.Printf("stats: container list: %v", err)
		return false
	}

	var ids []string
	for _, s := range tree.Stacks {
		for _, c := range s.Containers {
			if c.State == "running" {
				ids = append(ids, c.ID)
			}
		}
	}
	var sizes map[string]docker.Disk
	if time.Since(h.sizesAt) >= sizesEvery {
		sizeCtx, cancelSize := context.WithTimeout(ctx, 30*time.Second)
		sizes, err = h.docker.Sizes(sizeCtx)
		cancelSize()
		if err != nil {
			log.Printf("stats: container sizes: %v", err)
			sizes = nil
		} else {
			h.sizesAt = time.Now()
		}
	}

	if len(ids) == 0 {
		h.mu.Lock()
		h.metrics.Stats = map[string]docker.Stats{}
		if sizes != nil {
			h.metrics.Disk = sizes
		}
		h.mu.Unlock()
		return sizes != nil
	}

	statsCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()

	fresh := make(map[string]docker.Stats, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, statsConcurrency)

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-statsCtx.Done():
				return
			}
			s, err := h.docker.Stats(statsCtx, id)
			if err != nil {
				return
			}
			mu.Lock()
			fresh[id] = *s
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	if len(fresh) == 0 && sizes == nil {
		return false
	}
	h.mu.Lock()
	if len(fresh) > 0 {
		h.metrics.Stats = fresh
	}
	if sizes != nil {
		h.metrics.Disk = sizes
	}
	h.mu.Unlock()

	h.record(tree, fresh, sizes)
	return true
}

func (h *Hub) record(tree *docker.Tree, fresh map[string]docker.Stats, sizes map[string]docker.Disk) {
	if h.writer == nil {
		return
	}

	ts := time.Now()
	samples := make([]store.ContainerSample, 0, tree.Total)
	for _, st := range tree.Stacks {
		for _, c := range st.Containers {
			s := store.ContainerSample{
				Container: c.Name,
				State:     c.State,
				Health:    c.Health,
				Stack:     st.Name,
			}
			if v, ok := fresh[c.ID]; ok {
				cpu := v.CPU
				mem := int64(v.Mem)
				s.CPU, s.Mem = &cpu, &mem
				if v.MemLimit > 0 {
					pct := float64(v.Mem) / float64(v.MemLimit) * 100
					s.MemPct = &pct
				}
			}
			if d, ok := sizes[c.ID]; ok {
				rw, root := d.Rw, d.RootFs
				s.DiskRw, s.DiskRootFs = &rw, &root
			}
			samples = append(samples, s)
		}
	}
	h.writer.Containers(ts, samples)
}
