package heavy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"aacpanel/internal/hostcfg"
)

// Host is what the queue reads the machine through and starts runs with.
type Host interface {
	// Measure reads the machine, the agents' slice and the runs going in it.
	Measure(ctx context.Context) (Measure, error)
	// Start moves the process pid into a new scope in the agents' slice and
	// returns once it is there.
	Start(ctx context.Context, pid int, s Scope) error
}

// Scope is the unit one run lives in and what it is held to. A ceiling of
// nought is none, and the run is held only by the slice there.
type Scope struct {
	Unit string
	Name string
	Mem  uint64
	Swap uint64
	CPUs int
}

// swapOf is the swap a run asking for ask may take, an eighth of it: a
// little lets the cold pages of a long run out, and a lot only turns the kill
// at its MemoryMax into minutes of thrashing.
func swapOf(ask uint64) uint64 { return ask / 8 }

const (
	// callWait bounds the read of a request and the write of a line.
	callWait = 5 * time.Second
	// maxRequest bounds a request: a name, a size and a directory.
	maxRequest = 16 << 10
	// nameMax is how long the name of a run may be, in characters.
	nameMax = 80
)

// Queue is the queue of heavy runs.
type Queue struct {
	host   Host
	limits Limits

	mu      sync.Mutex
	waiting []*waiter
	known   map[string]admitted
	head    string
	seq     int
	failed  string
	wake    chan struct{}
}

type waiter struct {
	pid   int
	name  string
	dir   string
	ask   uint64
	since time.Time
	said  time.Time
	// lines are what the queue tells the run while it waits, dropped when
	// the client reads slower than the queue says; verdict is the one reply
	// that ends the wait, never dropped and never blocking the queue.
	lines   chan Reply
	verdict chan Reply
}

// admitted is what the queue keeps of a run it let go: its scope keeps only
// the ceiling.
type admitted struct {
	name  string
	dir   string
	since time.Time
}

// NewQueue is a queue that admits by limits and reads the host through host.
func NewQueue(host Host, limits Limits) *Queue {
	return &Queue{host: host, limits: limits, known: map[string]admitted{}, wake: make(chan struct{}, 1)}
}

// LimitsOf are the limits of the description in bytes; the error names what
// the description gives that the queue cannot use.
func LimitsOf(h hostcfg.Heavy, m hostcfg.Memory) (Limits, error) {
	l := Limits{Slots: h.Slots, CPUs: h.CPUs}
	var errs []error
	for _, s := range []struct {
		name, value string
		to          *uint64
	}{
		{hostcfg.HeavyReserveEnv, h.Reserve, &l.Reserve},
		{hostcfg.HeavyMemEnv, h.Mem, &l.Ask},
		{hostcfg.AgentsMemoryHighEnv, m.Agents.High, &l.Budget},
	} {
		if s.value == "" {
			continue
		}
		n, ok := hostcfg.SizeBytes(s.value)
		if !ok {
			errs = append(errs, fmt.Errorf("%s=%s is not a size the queue can count", s.name, s.value))
			continue
		}
		*s.to = n
	}
	total, err := hostcfg.MachineMemory()
	if err != nil {
		errs = append(errs, fmt.Errorf("the queue does not refuse a run bigger than the machine: %w", err))
	}
	l.Total = total
	return l, errors.Join(errs...)
}

// Listen opens the socket for the owner alone. A socket an executor left
// behind is taken over; one that answers is another executor's.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("the directory of the queue's socket: %w", err)
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("another executor answers on %s", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("the queue's socket %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// Serve answers on ln and keeps the queue until ctx ends.
func (q *Queue) Serve(ctx context.Context, ln net.Listener) {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	go q.keep(ctx)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("heavy runs: the socket stopped: %v", err)
			}
			return
		}
		go q.answer(ctx, conn)
	}
}

func (q *Queue) answer(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(callWait))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Reply{Error: "the request was not parsed: " + err.Error()})
		return
	}
	switch req.Op {
	case OpView:
		v, err := q.View(ctx)
		if err != nil {
			_ = json.NewEncoder(conn).Encode(Reply{Error: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(Reply{View: &v})
	case OpRun:
		q.run(ctx, conn, req)
	default:
		_ = json.NewEncoder(conn).Encode(Reply{Error: fmt.Sprintf("there is no such operation: %q", req.Op)})
	}
}

// run keeps one run in the queue for as long as its client holds the
// connection, and tells it how it stands. A client that goes away — killed,
// or its command timed out — leaves the queue with it.
func (q *Queue) run(ctx context.Context, conn net.Conn, req Request) {
	say := func(r Reply) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(callWait))
		return json.NewEncoder(conn).Encode(r) == nil
	}
	pid, err := peerPID(conn)
	if err != nil {
		say(Reply{State: StateUnheld, Line: "the queue cannot tell which process asks (" + err.Error() +
			"); running without it and its ceilings"})
		return
	}
	ask := q.limits.Ask
	if req.Mem != "" {
		n, ok := hostcfg.SizeBytes(req.Mem)
		if !ok {
			say(Reply{State: StateRefused, Line: fmt.Sprintf("-mem %s is not a size systemd takes: a whole number "+
				"of bytes, or of K, M, G or T, like 8G or 768M", req.Mem)})
			return
		}
		ask = n
	}
	if why := Never(q.limits, ask); why != "" {
		say(Reply{State: StateRefused, Line: "refused: " + why + "; it would wait for ever"})
		return
	}
	now := time.Now()
	w := &waiter{pid: pid, name: cleanName(req.Name), dir: req.Dir, ask: ask, since: now, said: now,
		lines: make(chan Reply, 4), verdict: make(chan Reply, 1)}
	place, ahead := q.join(w)
	if !say(Reply{State: StateWaiting, Line: fmt.Sprintf("in the queue at place %d, %s, asking for %s",
		place, aheadOf(ahead), sizeOrNone(ask))}) {
		q.leave(w)
		return
	}
	q.poke()

	_ = conn.SetDeadline(time.Time{})
	gone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		close(gone)
	}()
	for {
		select {
		case r := <-w.lines:
			if !say(r) {
				q.leave(w)
				return
			}
		case r := <-w.verdict:
			say(r)
			return
		case <-gone:
			q.leave(w)
			return
		case <-ctx.Done():
			return
		}
	}
}

func aheadOf(n int) string {
	switch n {
	case 0:
		return "nobody ahead"
	case 1:
		return "1 run ahead"
	}
	return strconv.Itoa(n) + " runs ahead"
}

func sizeOrNone(n uint64) string {
	if n == 0 {
		return "no ceiling"
	}
	return Size(n)
}

// join puts w at the end of the queue and says where it stands.
func (q *Queue) join(w *waiter) (place, ahead int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.waiting = append(q.waiting, w)
	return len(q.waiting), len(q.waiting) - 1
}

// leave takes w out of the queue, if it is still there, and looks again: the
// run behind it may be the head now.
func (q *Queue) leave(w *waiter) {
	q.mu.Lock()
	for i, o := range q.waiting {
		if o == w {
			q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
			break
		}
	}
	q.mu.Unlock()
	q.poke()
}

func (q *Queue) poke() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// keep looks at the host every lookEvery and whenever a run joins or leaves,
// lets go the runs that fit, and tells the others how they stand.
func (q *Queue) keep(ctx context.Context) {
	tick := time.NewTicker(lookEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-q.wake:
		}
		q.look(ctx, time.Now())
	}
}

// look is one pass of the queue.
func (q *Queue) look(ctx context.Context, now time.Time) {
	m, err := q.host.Measure(ctx)
	q.mu.Lock()
	if err != nil {
		q.head = "a read of the host, which failed: " + err.Error()
		if q.failed != err.Error() {
			log.Printf("heavy runs: the host was not read, nothing is admitted: %v", err)
		}
		q.failed = err.Error()
		q.tell(now)
		q.mu.Unlock()
		return
	}
	q.failed = ""
	q.forget(m.Runs)
	asks := make([]uint64, len(q.waiting))
	for i, w := range q.waiting {
		asks[i] = w.ask
	}
	goes, waits := Admit(q.limits, m, asks)
	var leaving []*waiter
	for _, i := range goes {
		leaving = append(leaving, q.waiting[i])
	}
	for _, w := range leaving {
		for i, o := range q.waiting {
			if o == w {
				q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
				break
			}
		}
	}
	q.head = waits
	q.tell(now)
	q.mu.Unlock()

	for _, w := range leaving {
		q.let(ctx, w, m.Runs, now)
	}
}

// let starts the scope of a run the queue admitted, and tells its client to
// go on. A scope the manager would not start leaves the run to go on
// without it: the queue is there to order the runs, not to stop one.
func (q *Queue) let(ctx context.Context, w *waiter, runs []Running, now time.Time) {
	unit := q.nextUnit(runs)
	s := Scope{Unit: unit, Name: w.name, Mem: w.ask, Swap: swapOf(w.ask), CPUs: q.limits.CPUs}
	if err := q.host.Start(ctx, w.pid, s); err != nil {
		log.Printf("heavy runs: %q of process %d runs without its scope: %v", w.name, w.pid, err)
		w.verdict <- Reply{State: StateUnheld, Line: fmt.Sprintf("the queue let the run go after %s, but its scope "+
			"did not start (%v); running without the queue's ceilings", waited(now.Sub(w.since)), err)}
		q.poke()
		return
	}
	q.mu.Lock()
	q.known[unit] = admitted{name: w.name, dir: w.dir, since: now}
	q.mu.Unlock()
	w.verdict <- Reply{State: StateAdmitted, Line: fmt.Sprintf("admitted after %s into %s: %s", waited(now.Sub(w.since)),
		unit, held(s))}
}

func waited(d time.Duration) string {
	return d.Round(time.Second).String()
}

func held(s Scope) string {
	var parts []string
	if s.Mem > 0 {
		parts = append(parts, "MemoryMax="+Size(s.Mem), "MemorySwapMax="+Size(s.Swap))
	}
	if s.CPUs > 0 {
		parts = append(parts, fmt.Sprintf("CPUQuota=%d%%", s.CPUs*100))
	}
	if len(parts) == 0 {
		return "no ceilings of its own"
	}
	return strings.Join(parts, " ")
}

// nextUnit is a name of a scope no run going has: an executor started while
// the runs another one admitted go numbers its own from one again.
func (q *Queue) nextUnit(runs []Running) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	taken := map[string]bool{}
	for _, r := range runs {
		taken[r.Unit] = true
	}
	for {
		q.seq++
		unit := UnitPrefix + strconv.Itoa(q.seq) + ".scope"
		if !taken[unit] {
			return unit
		}
	}
}

// forget drops what the queue kept of runs whose scopes have ended.
func (q *Queue) forget(runs []Running) {
	going := map[string]bool{}
	for _, r := range runs {
		going[r.Unit] = true
	}
	for unit := range q.known {
		if !going[unit] {
			delete(q.known, unit)
		}
	}
}

// tell gives every waiting run that has heard nothing for sayEvery where it
// stands and what the head waits for.
func (q *Queue) tell(now time.Time) {
	for i, w := range q.waiting {
		if now.Sub(w.said) < sayEvery {
			continue
		}
		line := fmt.Sprintf("next in the queue after %s, waiting for %s", waited(now.Sub(w.since)), q.head)
		if i > 0 {
			line = fmt.Sprintf("%s after %s; the head waits for %s", aheadOf(i), waited(now.Sub(w.since)), q.head)
		}
		select {
		case w.lines <- Reply{State: StateWaiting, Line: line}:
			w.said = now
		default:
		}
	}
}

// View is the queue as it stands now.
func (q *Queue) View(ctx context.Context) (View, error) {
	m, err := q.host.Measure(ctx)
	if err != nil {
		return View{}, fmt.Errorf("the host was not read: %w", err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	v := View{Slots: q.limits.Slots, Running: []RunningView{}, Waiting: []WaitingView{}}
	for _, r := range m.Runs {
		rv := RunningView{Unit: r.Unit, Ask: r.Ask, Taken: r.Taken}
		if k, ok := q.known[r.Unit]; ok {
			rv.Name, rv.Dir, rv.Since = k.name, k.dir, k.since.Unix()
		}
		v.Running = append(v.Running, rv)
	}
	for i, w := range q.waiting {
		v.Waiting = append(v.Waiting, WaitingView{Place: i + 1, Name: w.name, Dir: w.dir, Ask: w.ask,
			Since: w.since.Unix()})
	}
	if len(q.waiting) > 0 {
		v.Head = q.head
	}
	return v, nil
}

// peerPID is the process at the other end of a unix connection, as the
// kernel tells it.
func peerPID(conn net.Conn) (int, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	if cred.Pid <= 0 {
		return 0, errors.New("the process lives where the executor does not see it")
	}
	return int(cred.Pid), nil
}

// cleanName is a name fit for a line and a screen: one line, no control
// characters, cut at nameMax.
func cleanName(name string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
	if r := []rune(name); len(r) > nameMax {
		name = string(r[:nameMax-1]) + "…"
	}
	return name
}
