package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/stream"
)

// Message is what a person sends a thread: words, pictures by their paths on
// the host, or both, and the name the panel gave it to take it back by.
type Message struct {
	ID     string   `json:"id,omitempty"`
	Text   string   `json:"text,omitempty"`
	Images []string `json:"images,omitempty"`
}

// input is the message as turn/start takes it: the words, then each picture
// as a file of the host codex reads itself.
func (m Message) input() []any {
	out := []any{}
	if m.Text != "" {
		out = append(out, textInput(m.Text)...)
	}
	for _, path := range m.Images {
		out = append(out, map[string]any{"type": "localImage", "path": path})
	}
	return out
}

// Queued is a message that waits in the panel's queue of a thread.
type Queued struct {
	Message
	Since time.Time `json:"since"`
}

// outbox is what the panel holds for a thread until a turn takes it: the
// messages that wait for the turn that runs to end, and a model and an effort
// that go with the next turn/start where the daemon would not change them
// sooner. It is kept on the disk by the contour of the home, so an executor
// started again delivers what waited; it goes with the thread when the panel
// closes it or the daemon unloads it. The queue is the person's own words on
// their way to the thread, and the file is the owner's alone.
type outbox struct {
	Queue  []Queued `json:"queue"`
	Model  string   `json:"model,omitempty"`
	Effort string   `json:"effort,omitempty"`
}

func (b *outbox) empty() bool {
	return b == nil || (len(b.Queue) == 0 && b.Model == "" && b.Effort == "")
}

func outboxDir(contour string) string {
	return filepath.Join(stream.Dir(), "codex-outbox", contour)
}

func outboxPath(contour, id string) string { return filepath.Join(outboxDir(contour), id+".json") }

// loadOutboxes reads what an executor that stopped left for the threads of a
// contour.
func loadOutboxes(contour string) map[string]*outbox {
	out := map[string]*outbox{}
	entries, err := os.ReadDir(outboxDir(contour))
	if err != nil {
		return out
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || strings.HasPrefix(id, ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(outboxDir(contour), e.Name()))
		if err != nil {
			continue
		}
		b := &outbox{}
		if json.Unmarshal(raw, b) == nil && !b.empty() {
			out[id] = b
		}
	}
	return out
}

// box is the outbox of a thread, made when it has none. Called with mu held.
func (l *Link) box(id string) *outbox {
	b := l.boxes[id]
	if b == nil {
		b = &outbox{Queue: []Queued{}}
		l.boxes[id] = b
	}
	return b
}

// queued is how many messages wait for a thread. Called with mu held.
func (l *Link) queued(id string) int {
	if b := l.boxes[id]; b != nil {
		return len(b.Queue)
	}
	return 0
}

// keepOutbox writes the outbox of a thread to the disk, and removes it there
// once nothing waits. Called with mu held.
func (l *Link) keepOutbox(id string) {
	path := outboxPath(l.contour, id)
	b := l.boxes[id]
	if b.empty() {
		delete(l.boxes, id)
		_ = os.Remove(path)
		return
	}
	body, err := json.Marshal(b)
	if err == nil {
		err = write(path, body)
	}
	if err != nil {
		log.Printf("codex %s: what waits for thread %s was not kept on the disk, and a restart of the executor "+
			"loses it: %v", l.home, id, err)
	}
}

// later keeps a model and an effort to go with the next turn the panel starts
// in a thread; empty ones clear what was kept.
func (l *Link) later(id, model, effort string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if model == "" && effort == "" && l.boxes[id] == nil {
		return
	}
	b := l.box(id)
	b.Model, b.Effort = model, effort
	l.keepOutbox(id)
}

// nudge asks the link to read the daemon now rather than at the next round.
func (l *Link) nudge() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// Send gives a thread a message. A free thread starts a turn with it. A busy
// one — or one with messages of the panel waiting before it — gets it in the
// panel's queue, and the turn that runs takes nothing of it: the message goes
// as a turn of its own once the thread is free, in the order it came. It
// returns the place of the message in the queue, zero when a turn started
// with it.
func (l *Link) Send(ctx context.Context, threadID string, m Message) (int, error) {
	c, err := l.client()
	if err != nil {
		return 0, err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	free, err := l.free(ctx, c, threadID)
	if err != nil {
		return 0, err
	}
	l.mu.Lock()
	waiting := l.queued(threadID) > 0
	l.mu.Unlock()
	if free && !waiting {
		return 0, l.start(ctx, c, threadID, m)
	}
	l.mu.Lock()
	b := l.box(threadID)
	b.Queue = append(b.Queue, Queued{Message: m, Since: time.Now()})
	n := len(b.Queue)
	l.keepOutbox(threadID)
	l.mu.Unlock()
	l.save(threadID)
	l.nudge()
	return n, nil
}

// Answer gives a thread the answer to a question it asked without waiting. A
// turn that runs takes it at once, as codex's own terminal hands it — codex
// asked so as to go on, and an answer behind that turn would come after the
// work it was for — and a free thread starts a turn with it. It reports
// whether the turn that runs took it. A turn that ends between the look and
// the answer leaves the thread free, and the answer starts a turn of its own.
func (l *Link) Answer(ctx context.Context, threadID string, m Message) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	turn, err := runningTurn(ctx, c, threadID)
	if err != nil {
		return false, err
	}
	if turn != "" {
		params := map[string]any{"threadId": threadID, "expectedTurnId": turn, "input": m.input()}
		if m.ID != "" {
			params["clientUserMessageId"] = m.ID
		}
		err = within(ctx, c, "turn/steer", params, nil)
		if err == nil {
			return true, nil
		}
		if !refused(err) {
			return false, err
		}
		refusal := err
		if turn, err = runningTurn(ctx, c, threadID); err != nil {
			return false, err
		}
		if turn != "" {
			return false, fmt.Errorf("the turn that runs did not take the answer: %w", refusal)
		}
	}
	return false, l.start(ctx, c, threadID, m)
}

// Unqueue takes a message back from the panel's queue of a thread before it
// goes. It reports false when the message is not there: it has gone as a turn.
func (l *Link) Unqueue(threadID, messageID string) bool {
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	found := false
	if b := l.boxes[threadID]; b != nil {
		n := len(b.Queue)
		b.Queue = slices.DeleteFunc(b.Queue, func(q Queued) bool { return q.ID == messageID })
		found = len(b.Queue) < n
		l.keepOutbox(threadID)
	}
	l.mu.Unlock()
	if found {
		l.save(threadID)
	}
	return found
}

// free says a thread runs no turn: neither one the daemon reports nor one the
// panel has just started — the daemon answers turn/start before the thread
// turns active, and a second turn/start then would go into the first turn.
func (l *Link) free(ctx context.Context, c *conn, id string) (bool, error) {
	info, err := read(ctx, c, id)
	if err != nil {
		return false, err
	}
	if info.Status.Type == "active" {
		return false, nil
	}
	turn, err := runningTurn(ctx, c, id)
	return turn == "", err
}

// start starts a turn with a message, and with the model and the effort that
// wait for the next turn. turn/start makes the caller a client of the thread.
// Called with sub held.
func (l *Link) start(ctx context.Context, c *conn, id string, m Message) error {
	params := map[string]any{"threadId": id, "input": m.input()}
	if m.ID != "" {
		params["clientUserMessageId"] = m.ID
	}
	l.mu.Lock()
	if b := l.boxes[id]; b != nil {
		if b.Model != "" {
			params["model"] = b.Model
		}
		if b.Effort != "" {
			params["effort"] = b.Effort
		}
	}
	l.mu.Unlock()
	if err := within(ctx, c, "turn/start", params, nil); err != nil {
		return err
	}
	l.later(id, "", "")
	l.set(id, func(t *thread) { t.ours, t.subscribed = true, true })
	return nil
}

// drain starts a turn with the first message of a thread's queue once the
// thread is free. A message the daemon refuses is dropped — it would be
// refused at every round and hold the rest behind it — and the executor's log
// names the thread; one that did not reach the daemon stays first.
func (l *Link) drain(ctx context.Context, c *conn, id string) {
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	if l.queued(id) == 0 {
		l.mu.Unlock()
		return
	}
	next := l.boxes[id].Queue[0]
	l.mu.Unlock()
	if free, err := l.free(ctx, c, id); err != nil || !free {
		return
	}
	err := l.start(ctx, c, id, next.Message)
	if err != nil && !refused(err) {
		return
	}
	if err != nil {
		log.Printf("codex %s: thread %s refused a message of the panel's queue, and it is dropped: %v", l.home, id, err)
	}
	l.mu.Lock()
	if b := l.boxes[id]; b != nil && len(b.Queue) > 0 {
		b.Queue = slices.Delete(b.Queue, 0, 1)
		l.keepOutbox(id)
	}
	l.mu.Unlock()
	l.save(id)
}
