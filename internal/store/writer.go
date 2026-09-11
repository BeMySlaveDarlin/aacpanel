package store

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	queueLimit   = 180
	writeTimeout = 15 * time.Second
	retryDelay   = 5 * time.Second
	logEvery     = time.Minute
)

// ContainerSample is a row of metrics_container_raw.
type ContainerSample struct {
	Container  string
	State      string
	Health     string
	Stack      string
	CPU        *float64
	Mem        *int64
	MemPct     *float64
	DiskRw     *int64
	DiskRootFs *int64
}

type batch interface {
	write(ctx context.Context, tx pgx.Tx, hostID int) error
	rows() int
}

// Writer puts the metrics into a queue and writes them in batches.
type Writer struct {
	store    *Store
	hostName string

	mu    sync.Mutex
	queue []batch
	lost  int

	wake        chan struct{}
	startedAt   time.Time
	lastGrip    time.Time
	lastAgentAt int64
	faults      map[string]SnapshotFault
}

func NewWriter(s *Store, hostName string) *Writer {
	return &Writer{
		store:     s,
		hostName:  hostName,
		startedAt: time.Now(),
		wake:      make(chan struct{}, 1),
	}
}

// Containers queues a snapshot of the containers' load.
func (w *Writer) Containers(ts time.Time, samples []ContainerSample) {
	if w == nil || len(samples) == 0 {
		return
	}
	w.enqueue(&containerBatch{ts: ts, samples: samples})
}

func (w *Writer) enqueue(b batch) {
	w.mu.Lock()
	if len(w.queue) >= queueLimit {
		w.queue = w.queue[1:]
		w.lost++
	}
	w.queue = append(w.queue, b)
	w.mu.Unlock()

	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run drains the queue into the database.
func (w *Writer) Run(ctx context.Context) {
	if w == nil {
		return
	}
	tick := time.NewTicker(retryDelay)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-tick.C:
		}

		for w.flushOne(ctx) {
			if ctx.Err() != nil {
				return
			}
		}
	}
}

func (w *Writer) flushOne(ctx context.Context) bool {
	w.mu.Lock()
	if len(w.queue) == 0 {
		w.mu.Unlock()
		return false
	}
	b := w.queue[0]
	lost := w.lost
	w.mu.Unlock()

	if err := w.write(ctx, b); err != nil {
		if ctx.Err() == nil {
			w.gripe(err, lost)
		}
		return false
	}

	w.mu.Lock()
	if len(w.queue) > 0 && w.queue[0] == b {
		w.queue = w.queue[1:]
	}
	if w.lost > 0 {
		log.Printf("store: metric batches dropped because the buffer overflowed: %d", w.lost)
		w.lost = 0
	}
	w.mu.Unlock()
	return true
}

func (w *Writer) write(ctx context.Context, b batch) error {
	pool, err := w.store.Pool()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	hostID, err := w.store.HostID(ctx, w.hostName)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if err := b.write(ctx, tx, hostID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *Writer) gripe(err error, lost int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.lastGrip) < logEvery {
		return
	}
	w.lastGrip = time.Now()

	queued := len(w.queue)
	if errors.Is(err, ErrClosed) {
		return
	}
	if errors.Is(err, ErrUnavailable) {
		if time.Since(w.startedAt) < logEvery {
			return
		}
		log.Printf("store: metrics are not being written, the database is unavailable; batches buffered: %d, dropped: %d", queued, lost)
		return
	}
	log.Printf("store: writing metrics: %v; batches buffered: %d, dropped: %d", err, queued, lost)
}

// Queued is how many batches are waiting to be written.
func (w *Writer) Queued() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.queue)
}

type containerBatch struct {
	ts      time.Time
	samples []ContainerSample
}

func (b *containerBatch) rows() int { return len(b.samples) }

func (b *containerBatch) write(ctx context.Context, tx pgx.Tx, hostID int) error {
	batch := &pgx.Batch{}
	for _, s := range b.samples {
		batch.Queue(`
			INSERT INTO metrics_container_raw
				(ts, host_id, container, cpu_pct, mem_bytes, mem_pct, disk_rw, disk_rootfs, state, health, stack)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, nullif($10, ''), nullif($11, ''))
			ON CONFLICT DO NOTHING`,
			b.ts, hostID, s.Container, s.CPU, s.Mem, s.MemPct, s.DiskRw, s.DiskRootFs, s.State, s.Health, s.Stack)
	}
	return tx.SendBatch(ctx, batch).Close()
}
