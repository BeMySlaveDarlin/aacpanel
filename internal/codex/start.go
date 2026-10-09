package codex

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/stream"
)

// readyPoll is how often a wait for the link asks whether it is connected.
var readyPoll = 50 * time.Millisecond

// Begin is what a new thread starts with. A choice left empty is not sent,
// and the config.toml of the home decides it.
type Begin struct {
	CWD      string
	Model    string
	Approval string
	Sandbox  string
	// Effort is the thread's from its start, and goes with the first turn.
	Effort string
	// Message is the first message; without one no turn starts.
	Message string
	// Hold keeps the panel a client of the thread until it closes it.
	// Nothing else holds a thread the panel alone talks to, and the daemon
	// unloads a thread a while after its last client leaves.
	Hold bool
	// Name is given to the thread at its start. Naming writes the thread to
	// the disk — the daemon writes a thread at its first turn or its naming —
	// and codex in a terminal resumes only a thread written there.
	Name string
}

// Up says whether the daemon of the home has its control socket: whether
// there is a daemon to connect to at all.
func (l *Link) Up() bool {
	_, err := filepath.EvalSymlinks(l.socket)
	return err == nil
}

// Home is the codex home the link is to.
func (l *Link) Home() string { return l.home }

// Ready waits until the link holds a connection to its daemon, dialling at
// once rather than at the next round.
func (l *Link) Ready(ctx context.Context) error {
	for {
		if _, err := l.client(); err == nil {
			return nil
		}
		select {
		case l.wake <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the codex daemon of %s did not take the panel's connection — the executor's log says why", l.home)
		case <-time.After(readyPoll):
		}
	}
}

// Start starts a thread in the daemon and, with a first message, its first
// turn. It returns the id of the thread; a first message that did not go is
// an error beside a thread that did start.
func (l *Link) Start(ctx context.Context, b Begin) (string, error) {
	c, err := l.client()
	if err != nil {
		return "", err
	}
	params := map[string]any{"cwd": b.CWD}
	for key, value := range map[string]string{"model": b.Model, "approvalPolicy": b.Approval, "sandbox": b.Sandbox} {
		if value != "" {
			params[key] = value
		}
	}
	if b.Effort != "" {
		params["config"] = map[string]any{"model_reasoning_effort": b.Effort}
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	var out struct {
		Thread threadInfo `json:"thread"`
	}
	if err := within(ctx, c, "thread/start", params, &out); err != nil {
		return "", err
	}
	id := out.Thread.ID
	if id == "" {
		return "", fmt.Errorf("the codex daemon of %s started a thread and did not name it", l.home)
	}
	l.seen(out.Thread, time.Now())
	// thread/start makes the caller a client of the thread.
	l.set(id, func(t *thread) { t.subscribed = true })
	if b.Hold {
		l.hold(id)
	}
	l.save(id)
	if b.Name != "" {
		if err := within(ctx, c, "thread/name/set", map[string]any{"threadId": id, "name": b.Name}, nil); err != nil {
			return id, fmt.Errorf("the thread was not named, and codex in a terminal cannot resume it: %w", err)
		}
	}
	if b.Message == "" {
		return id, nil
	}
	turn := map[string]any{"threadId": id, "input": textInput(b.Message)}
	if b.Effort != "" {
		turn["effort"] = b.Effort
	}
	if err := within(ctx, c, "turn/start", turn, nil); err != nil {
		return id, fmt.Errorf("the first message did not go: %w", err)
	}
	l.set(id, func(t *thread) { t.ours = true })
	return id, nil
}

// Close lets a thread go: the turn it runs is interrupted, and the panel
// stops holding it and leaves it. The daemon unloads the thread once no
// client is left; its rollout stays, and codex resume brings it back. It
// reports whether a turn was interrupted.
func (l *Link) Close(ctx context.Context, threadID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	turn, err := interrupt(ctx, c, threadID)
	if err != nil {
		return false, err
	}
	l.unhold(threadID)
	if err := within(ctx, c, "thread/unsubscribe", map[string]any{"threadId": threadID}, nil); err != nil {
		return turn != "", err
	}
	l.mu.Lock()
	l.closed[threadID] = turn
	l.remove(threadID)
	l.mu.Unlock()
	return turn != "", nil
}

// interrupt stops the turn a thread runs and returns its id, empty when none
// runs.
func interrupt(ctx context.Context, c *conn, threadID string) (string, error) {
	info, err := read(ctx, c, threadID)
	if err != nil {
		return "", err
	}
	if info.Status.Type != "active" {
		return "", nil
	}
	turn, err := runningTurn(ctx, c, threadID)
	if err != nil || turn == "" {
		return "", err
	}
	if err := within(ctx, c, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turn}, nil); err != nil {
		return "", err
	}
	return turn, nil
}

// letGo says a thread the panel closed is no session of its own any more: it
// stays out of the panel until the daemon unloads it, unless another client
// starts a turn in it — the turn interrupted at the close is not that.
func (l *Link) letGo(ctx context.Context, c *conn, id string, active bool) bool {
	l.mu.Lock()
	stopped, closed := l.closed[id]
	l.mu.Unlock()
	if !closed {
		return false
	}
	if !active {
		return true
	}
	if turn, err := runningTurn(ctx, c, id); err != nil || turn == "" || turn == stopped {
		return true
	}
	l.mu.Lock()
	delete(l.closed, id)
	l.mu.Unlock()
	return false
}

// unloaded forgets what the link kept of threads the daemon no longer has
// loaded: one the panel closed is gone, and one it held was let go past it —
// the daemon was started again, or no executor ran for a while.
func (l *Link) unloaded(ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.closed {
		if !slices.Contains(ids, id) {
			delete(l.closed, id)
		}
	}
	for id := range l.held {
		if !slices.Contains(ids, id) {
			delete(l.held, id)
			_ = os.Remove(heldPath(l.contour, id))
		}
	}
}

// heldDir keeps a mark per thread the panel holds, by the contour of its
// home: the threads outlive an executor, and the one started after it has to
// know which of them are the panel's to take back.
func heldDir(contour string) string {
	return filepath.Join(stream.Dir(), "codex-held", contour)
}

func heldPath(contour, id string) string { return filepath.Join(heldDir(contour), id) }

// heldMarks are the threads of a contour the panel holds, as marked.
func heldMarks(contour string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(heldDir(contour))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out[e.Name()] = true
		}
	}
	return out
}

// hold marks a thread the panel holds. A mark not written leaves the thread
// held only until the connection drops: the link takes back the marked ones.
func (l *Link) hold(id string) {
	l.mu.Lock()
	l.held[id] = true
	l.mu.Unlock()
	err := os.MkdirAll(heldDir(l.contour), 0o700)
	if err == nil {
		err = os.WriteFile(heldPath(l.contour, id), nil, 0o600)
	}
	if err != nil {
		log.Printf("codex %s: thread %s is held, and its mark was not written: %v", l.home, id, err)
	}
}

func (l *Link) unhold(id string) {
	l.mu.Lock()
	delete(l.held, id)
	l.mu.Unlock()
	_ = os.Remove(heldPath(l.contour, id))
}

func textInput(text string) []any {
	return []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}
}
